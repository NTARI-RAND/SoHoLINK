package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// ComputeMetering calculates resource consumption and earnings for a completed
// job and writes a record to job_metering. It is idempotent — calling it twice
// for the same job is safe. Returns nil if the job is not found or not yet
// completed (started_at / completed_at not set).
func ComputeMetering(ctx context.Context, db *DB, jobID string) error {
	var (
		startedAt, completedAt time.Time
		cpuEnabled             bool
		ramPct, storageGB      int
		cpuCores               int
		ramMB                  int64
		priceMultiplier        float64
	)

	err := db.Pool.QueryRow(ctx, `
		SELECT j.started_at, j.completed_at,
		       COALESCE(rp.cpu_enabled, true),
		       COALESCE(rp.ram_pct, 100),
		       COALESCE(rp.storage_gb, 0),
		       COALESCE(rp.price_multiplier, 1.0),
		       hw.cpu_cores, hw.ram_mb
		FROM jobs j
		JOIN nodes n ON n.id = j.node_id
		LEFT JOIN resource_profiles rp ON rp.node_id = n.id AND rp.is_default = TRUE
		CROSS JOIN LATERAL (
		    SELECT (n.hardware_profile->>'CPUCores')::int    AS cpu_cores,
		           (n.hardware_profile->>'RAMMB')::bigint    AS ram_mb
		) hw
		WHERE j.id = $1
		  AND j.started_at IS NOT NULL
		  AND j.completed_at IS NOT NULL`,
		jobID,
	).Scan(&startedAt, &completedAt,
		&cpuEnabled, &ramPct, &storageGB,
		&priceMultiplier, &cpuCores, &ramMB)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	durationHours := completedAt.Sub(startedAt).Hours()

	var cpuCoreHours float64
	if cpuEnabled {
		cpuCoreHours = float64(cpuCores) * durationHours
	}
	ramGBHours := float64(ramMB) / 1024.0 * float64(ramPct) / 100.0 * durationHours
	storageGBMonths := float64(storageGB) / 730.0 * durationHours

	// Fetch current platform rates.
	rateRows, err := db.Pool.Query(ctx, `
		SELECT resource_type, base_rate, contributor_share
		FROM resource_pricing
		WHERE (effective_until IS NULL OR effective_until > NOW())
		ORDER BY resource_type, effective_from DESC`)
	if err != nil {
		return err
	}
	defer rateRows.Close()

	rates := make(map[string]float64)
	var contributorShare float64 = 0.65 // fallback if no row found
	for rateRows.Next() {
		var rt string
		var rate, share float64
		if err := rateRows.Scan(&rt, &rate, &share); err != nil {
			return err
		}
		if _, seen := rates[rt]; !seen {
			rates[rt] = rate
			if rt == "cpu_core_hr" {
				contributorShare = share
			}
		}
	}
	if err := rateRows.Err(); err != nil {
		return err
	}

	totalCost := (cpuCoreHours*rates["cpu_core_hr"] +
		ramGBHours*rates["ram_gb_hr"] +
		storageGBMonths*rates["storage_gb_mo"]) * priceMultiplier

	consumerPaidCents := int64(math.Round(totalCost * 100))
	contributorEarnedCents := int64(math.Round(float64(consumerPaidCents) * contributorShare))
	platformFeeCents := consumerPaidCents - contributorEarnedCents

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO job_metering
		    (job_id, cpu_core_hours, ram_gb_hours, storage_gb_months,
		     consumer_paid_cents, contributor_earned_cents, platform_fee_cents)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (job_id) DO NOTHING`,
		jobID, cpuCoreHours, ramGBHours, storageGBMonths,
		consumerPaidCents, contributorEarnedCents, platformFeeCents,
	)
	return err
}

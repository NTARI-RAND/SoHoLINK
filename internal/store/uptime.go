package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/metrics"
)

// RunUptimeScorer runs in a goroutine and updates nodes.uptime_pct every
// interval based on heartbeat events over the past 7 days.
// Expected heartbeats = 7 * 24 * 60 * 2 (one per 30s).
func RunUptimeScorer(ctx context.Context, db *DB, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := scoreUptime(ctx, db); err != nil {
				// Non-fatal — log and continue on next tick.
				slog.Warn("uptime scorer error", "error", err)
			}
		}
	}
}

func scoreUptime(ctx context.Context, db *DB) error {
	start := time.Now()
	defer func() { metrics.UptimeScorerDuration.Observe(time.Since(start).Seconds()) }()

	_, err := db.Pool.Exec(ctx, `
        UPDATE nodes n
        SET uptime_pct = LEAST(
            (SELECT COUNT(*) FROM node_heartbeat_events e
             WHERE e.node_id = n.id
               AND e.recorded_at > NOW() - INTERVAL '7 days')::numeric / 20160 * 100,
            100)`,
	)
	return err
}

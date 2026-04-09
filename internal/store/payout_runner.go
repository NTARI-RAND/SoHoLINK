package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
)

// RunPayoutReleaser runs in a goroutine and releases payouts for eligible
// jobs every interval. It calls EligiblePayouts, then for each candidate
// calls pc.TriggerPayout. Errors per-candidate are logged and skipped —
// a failed payout does not stop processing other candidates.
func RunPayoutReleaser(ctx context.Context, db *DB, pc *payment.Client, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			candidates, err := EligiblePayouts(ctx, db)
			if err != nil {
				slog.Warn("payout releaser: EligiblePayouts error", "error", err)
				continue
			}

			for _, c := range candidates {
				_, err := pc.TriggerPayout(ctx, c.ProviderStripeAccountID, c.ContributorEarnedCents)
				if err != nil {
					slog.Warn("payout releaser: TriggerPayout failed",
						"job_id", c.JobID,
						"stripe_account", c.ProviderStripeAccountID,
						"error", err,
					)
					continue
				}

				_, dbErr := db.Pool.Exec(ctx,
					`UPDATE job_metering SET payout_released_at = NOW() WHERE job_id = $1`,
					c.JobID,
				)
				if dbErr != nil {
					slog.Warn("payout releaser: failed to mark payout_released_at",
						"job_id", c.JobID,
						"error", dbErr,
					)
				}
			}
		}
	}
}

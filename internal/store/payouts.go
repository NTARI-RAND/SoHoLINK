package store

import "context"

// PayoutCandidate holds the identifiers needed to release a payout to a provider.
type PayoutCandidate struct {
	JobID                   string
	ProviderStripeAccountID string
	ContributorEarnedCents  int64
}

// EligiblePayouts returns jobs that are ready for payout release:
// completed more than 24 hours ago, no open or under_review dispute,
// and the provider has a stripe_account_id set.
func EligiblePayouts(ctx context.Context, db *DB) ([]PayoutCandidate, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT j.id, p.stripe_account_id, jm.contributor_earned_cents
		FROM jobs j
		JOIN nodes n ON n.id = j.node_id
		JOIN providers p ON p.id = n.provider_id
		JOIN job_metering jm ON jm.job_id = j.id
		LEFT JOIN disputes d ON d.job_id = j.id
		    AND d.status IN ('open', 'under_review')
		WHERE j.status = 'completed'
		  AND j.completed_at < NOW() - INTERVAL '24 hours'
		  AND j.amount_cents > 0
		  AND p.stripe_account_id IS NOT NULL
		  AND d.id IS NULL
		  AND jm.contributor_earned_cents > 0
		  AND jm.payout_released_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []PayoutCandidate
	for rows.Next() {
		var c PayoutCandidate
		if err := rows.Scan(&c.JobID, &c.ProviderStripeAccountID, &c.ContributorEarnedCents); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

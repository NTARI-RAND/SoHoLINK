package payment

import (
	"context"
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
)

// TriggerPayout initiates a manual payout of amountCents from the connected
// account's Stripe balance to its linked bank account. This is called by NTARI
// after the 24-hour dispute window has elapsed.
//
// The Stripe-Account header is set so the payout acts on the connected
// account's balance rather than the platform balance.
//
// idempotencyKey MUST be stable per logical payout (the caller passes the
// job id). If the releaser's post-transfer bookkeeping write fails and the
// job is re-selected on the next tick, replaying the SAME key makes Stripe
// return the original payout instead of creating a second one — this is the
// backstop that closes the double-pay window (audit finding M1). An empty key
// disables the header and is treated as a caller error.
func (c *Client) TriggerPayout(ctx context.Context, connectedAccountID string, amountCents int64, idempotencyKey string) (string, error) {
	if amountCents <= 0 {
		return "", fmt.Errorf("trigger payout: amountCents must be positive")
	}
	if idempotencyKey == "" {
		return "", fmt.Errorf("trigger payout: idempotencyKey must be set")
	}

	params := &stripe.PayoutCreateParams{
		Amount:   stripe.Int64(amountCents),
		Currency: stripe.String("usd"),
	}
	params.SetStripeAccount(connectedAccountID)
	// Scope the key to payouts so it can never collide with another Stripe
	// operation that happens to key on the same job id.
	params.SetIdempotencyKey("payout:" + idempotencyKey)

	po, err := c.sc.V1Payouts.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("trigger payout: %w", err)
	}
	return po.ID, nil
}

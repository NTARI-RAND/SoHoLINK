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
func (c *Client) TriggerPayout(ctx context.Context, connectedAccountID string, amountCents int64) (string, error) {
	if amountCents <= 0 {
		return "", fmt.Errorf("trigger payout: amountCents must be positive")
	}

	params := &stripe.PayoutCreateParams{
		Amount:   stripe.Int64(amountCents),
		Currency: stripe.String("usd"),
	}
	params.SetStripeAccount(connectedAccountID)

	po, err := c.sc.V1Payouts.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("trigger payout: %w", err)
	}
	return po.ID, nil
}

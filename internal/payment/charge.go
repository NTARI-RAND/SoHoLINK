package payment

import (
	"context"
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
)

// DestinationChargeResult carries the identifiers returned after creating a
// destination charge against a connected account.
type DestinationChargeResult struct {
	PaymentIntentID string
	ClientSecret    string
}

// CreateDestinationCharge creates a PaymentIntent that charges amountCents
// to the platform, deducts feeCents as the application fee, and transfers the
// remainder to connectedAccountID. All amounts are in cents (int64).
//
// The returned ClientSecret must be passed to Stripe.js on the consumer side
// to complete payment confirmation.
func (c *Client) CreateDestinationCharge(
	ctx context.Context,
	amountCents int64,
	feeCents int64,
	connectedAccountID string,
) (DestinationChargeResult, error) {
	if amountCents <= 0 {
		return DestinationChargeResult{}, fmt.Errorf("create destination charge: amountCents must be positive")
	}
	if feeCents < 0 {
		return DestinationChargeResult{}, fmt.Errorf("create destination charge: feeCents must be non-negative")
	}
	if feeCents >= amountCents {
		return DestinationChargeResult{}, fmt.Errorf("create destination charge: feeCents (%d) must be less than amountCents (%d)", feeCents, amountCents)
	}

	params := &stripe.PaymentIntentCreateParams{
		Amount:               stripe.Int64(amountCents),
		Currency:             stripe.String("usd"),
		ApplicationFeeAmount: stripe.Int64(feeCents),
		TransferData: &stripe.PaymentIntentCreateTransferDataParams{
			Destination: stripe.String(connectedAccountID),
		},
	}

	pi, err := c.sc.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return DestinationChargeResult{}, fmt.Errorf("create destination charge: %w", err)
	}

	return DestinationChargeResult{
		PaymentIntentID: pi.ID,
		ClientSecret:    pi.ClientSecret,
	}, nil
}

// CreateRefund issues a refund against the given PaymentIntent for amountCents.
// Pass the full charge amount to refund in full, or a partial amount for
// split outcomes. amountCents must be positive.
func (c *Client) CreateRefund(ctx context.Context, paymentIntentID string, amountCents int64) error {
	if amountCents <= 0 {
		return fmt.Errorf("create refund: amountCents must be positive")
	}
	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(paymentIntentID),
		Amount:        stripe.Int64(amountCents),
	}
	_, err := c.sc.V1Refunds.Create(ctx, params)
	if err != nil {
		return fmt.Errorf("create refund: %w", err)
	}
	return nil
}

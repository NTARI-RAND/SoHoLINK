package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	stripe "github.com/stripe/stripe-go/v82"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
)

// handleStripeWebhook verifies the Stripe-Signature header and dispatches
// V1 webhook events. Returns 400 on signature failure so Stripe retries;
// returns 200 on success or for unrecognised event types.
func (ps *PortalServer) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if err := payment.HandleV1Event(w, r, ps.webhookSecret, ps.onStripeEvent); err != nil {
		http.Error(w, "webhook error", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (ps *PortalServer) onStripeEvent(event stripe.Event) error {
	switch event.Type {
	case "account.updated":
		return ps.onAccountUpdated(event)
	}
	return nil
}

// onAccountUpdated syncs a connected account's onboarding status to the
// providers table. Stripe sends this event whenever capabilities change,
// including when a provider completes the Stripe Express onboarding flow.
func (ps *PortalServer) onAccountUpdated(event stripe.Event) error {
	var acct stripe.Account
	if err := json.Unmarshal(event.Data.Raw, &acct); err != nil {
		return fmt.Errorf("unmarshal account: %w", err)
	}

	transfersActive := acct.Capabilities != nil &&
		acct.Capabilities.Transfers == stripe.AccountCapabilityStatusActive

	_, err := ps.db.Pool.Exec(
		context.Background(),
		`UPDATE providers SET stripe_onboarding_complete = $1, updated_at = NOW()
		 WHERE stripe_account_id = $2`,
		transfersActive, acct.ID,
	)
	return err
}

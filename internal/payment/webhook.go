package payment

import (
	"context"
	"fmt"
	"io"
	"net/http"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

// V1EventHandler is called when a V1 webhook event is received. Implementations
// should switch on event.Type and handle relevant events.
type V1EventHandler func(event stripe.Event) error

// ThinEventHandler is called when a V2 thin event is received. The event ID
// can be used to fetch the full event object via V2CoreEvents.Retrieve if
// additional data is needed.
type ThinEventHandler func(ctx context.Context, thinEvent *stripe.ThinEvent) error

// HandleV1Event reads the request body, verifies the Stripe-Signature header
// using webhookSecret, constructs the V1 Event, and dispatches it to handler.
// Returns a non-nil error if signature validation fails or handler returns an error.
func HandleV1Event(w http.ResponseWriter, r *http.Request, webhookSecret string, handler V1EventHandler) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read webhook body: %w", err)
	}

	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), webhookSecret)
	if err != nil {
		return fmt.Errorf("construct v1 event: %w", err)
	}

	if err := handler(event); err != nil {
		return fmt.Errorf("handle v1 event %s: %w", event.Type, err)
	}
	return nil
}

// HandleThinEvent reads the request body, verifies the Stripe-Signature header
// using webhookSecret, parses the thin event, and dispatches it to handler.
// Use this for V2 event destinations (account.updated, capability.updated thin events).
func (c *Client) HandleThinEvent(w http.ResponseWriter, r *http.Request, webhookSecret string, handler ThinEventHandler) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read webhook body: %w", err)
	}

	thinEvent, err := c.sc.ParseThinEvent(body, r.Header.Get("Stripe-Signature"), webhookSecret)
	if err != nil {
		return fmt.Errorf("parse thin event: %w", err)
	}

	if err := handler(r.Context(), thinEvent); err != nil {
		return fmt.Errorf("handle thin event %s: %w", thinEvent.Type, err)
	}
	return nil
}

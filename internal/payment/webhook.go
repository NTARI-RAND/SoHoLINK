package payment

import (
	"context"
	"fmt"
	"io"
	"net/http"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

// DefaultWebhookBodyBytes follows Stripe's Go webhook example (64 KiB).
const DefaultWebhookBodyBytes int64 = 64 << 10

// WebhookOptions sets a local request-byte limit. Zero retains the default;
// negative limits are rejected. The last nonzero option takes precedence.
type WebhookOptions struct {
	MaxBodyBytes int64
}

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
func HandleV1Event(w http.ResponseWriter, r *http.Request, webhookSecret string, handler V1EventHandler, options ...WebhookOptions) error {
	body, err := readWebhookBody(w, r, options)
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
func (c *Client) HandleThinEvent(w http.ResponseWriter, r *http.Request, webhookSecret string, handler ThinEventHandler, options ...WebhookOptions) error {
	body, err := readWebhookBody(w, r, options)
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

// readWebhookBody preserves the signed bytes and detects overflow, including
// requests with unknown or misleading Content-Length. The HTTP server owns
// closing the incoming request body.
func readWebhookBody(w http.ResponseWriter, r *http.Request, options []WebhookOptions) ([]byte, error) {
	limit := DefaultWebhookBodyBytes
	for _, option := range options {
		if option.MaxBodyBytes != 0 {
			limit = option.MaxBodyBytes
		}
	}
	if limit < 0 {
		return nil, fmt.Errorf("webhook body limit must be positive")
	}
	return io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
}

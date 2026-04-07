package payment

import stripe "github.com/stripe/stripe-go/v82"

// Client wraps the Stripe SDK client. Construct one per process via New.
type Client struct {
	sc *stripe.Client
}

// New constructs a Client using the provided Stripe secret key.
// The caller is responsible for reading the key from the environment.
func New(secretKey string) *Client {
	return &Client{sc: stripe.NewClient(secretKey)}
}

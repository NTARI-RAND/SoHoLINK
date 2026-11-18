package identity

import "context"

type operatorContextKey struct{}

// WithOperatorID records the ID of an operator whose transmission has been
// verified. It is written ONLY by the API's OperatorAuth middleware, after the
// operator transmission verifies against the operator's active keys — the
// operator-path counterpart of the SPIFFE ID that RequireSPIFFE records. It
// lives here, beside the SPIFFE key, so the protocol adapter can bind a node
// message to its owning operator without importing the api package.
func WithOperatorID(ctx context.Context, operatorID string) context.Context {
	return context.WithValue(ctx, operatorContextKey{}, operatorID)
}

// OperatorIDFromContext retrieves the verified operator ID stored by
// WithOperatorID. Returns false if the request did not pass operator
// authentication.
func OperatorIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(operatorContextKey{}).(string)
	return id, ok && id != ""
}

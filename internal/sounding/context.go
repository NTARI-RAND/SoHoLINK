// Package sounding is SoHoLINK's demand-sounding telemetry: a hot-path-safe,
// fire-and-forget observation layer that records the SHAPE of job demand and
// contributor CAPACITY into the migration-025 TimescaleDB hypertables, which
// the :8090 governance dashboard (step 3) reads.
//
// HOT-PATH SAFETY IS PARAMOUNT. Everything here observes placement; nothing
// here may block, slow, alter, or fail an actual placement. The Sink is an
// async buffered channel drained by a single goroutine that batches writes and
// DROPS on backpressure rather than blocking a caller. Every write path
// FAILS OPEN: a telemetry error is logged and swallowed, never propagated back
// to SubmitJob/FindMatch. If we cannot record without risk, we record less.
//
// The package deliberately depends only on internal/store (for the pool). It
// does NOT import orchestrator or api, so the hot-path packages depend on it
// (not the reverse) with no import cycle.
package sounding

import "context"

// OperatorUnknown is the operator_id recorded when a code path has no
// authenticated operator in its context. The demand-sounding tables are
// NOT NULL on operator_id (migration 025), so a sentinel is required rather
// than a NULL. It is the honest value for the transitional era: the
// frontend-as-operator seam (CLAUDE.md "Layer 2") is a design target and is
// not yet wired onto the internal submit or SPIFFE heartbeat paths, so those
// records carry OperatorUnknown until it lands. The instant an
// operator-authenticated request reaches an instrumentation point, the real
// operator_id flows automatically via the context helpers below — see
// api.OperatorAuth, which stamps the same key.
const OperatorUnknown = "unknown"

// operatorIDKey is the private context key under which the authenticated
// operator_id is carried to instrumentation points. It is intentionally
// defined HERE (a leaf package) rather than in internal/api so that
// internal/orchestrator can read it without importing api (which would be a
// cycle: api already imports orchestrator).
type operatorIDKey struct{}

// ContextWithOperatorID returns a child context carrying operatorID for later
// retrieval by instrumentation. api.OperatorAuth calls this so that every
// operator-authenticated request propagates its identity to SubmitJob.
func ContextWithOperatorID(ctx context.Context, operatorID string) context.Context {
	return context.WithValue(ctx, operatorIDKey{}, operatorID)
}

// OperatorIDFromContext returns the operator_id carried by ctx and whether one
// was present. A present-but-empty value reports ok=false.
func OperatorIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(operatorIDKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// OperatorIDOrUnknown returns the context's operator_id, or OperatorUnknown
// when none is present. Instrumentation uses this so a record always has a
// non-NULL operator_id.
func OperatorIDOrUnknown(ctx context.Context) string {
	if id, ok := OperatorIDFromContext(ctx); ok {
		return id
	}
	return OperatorUnknown
}

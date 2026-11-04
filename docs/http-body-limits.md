# Webhook and submission-response byte limits

The transitional member portal bounds Stripe webhook requests before signature
verification and bounds responses from its private orchestrator submission API.
These are local resource budgets, not payment amount limits or changes to the
`sohocloud-protocol` wire contract.

| Surface | Default budget | Overflow behavior |
| --- | --- | --- |
| Stripe V1 and thin webhook request | 64 KiB | Return a wrapped `*http.MaxBytesError`; dispatch no event. The portal returns HTTP 413. |
| Successful `SubmitJob` response | 1 MiB | Return an error with status and byte limit; return no partial job result. |
| Non-2xx `SubmitJob` response | 4 KiB | Return an error with status and byte limit, omitting the oversized body. |

The webhook default follows [Stripe's Go webhook example](https://pkg.go.dev/github.com/stripe/stripe-go/v82/webhook).
The submission result currently contains four string fields: job ID, node ID,
job token and provider Stripe account ID. A 1 MiB response budget leaves ample
room for that receipt; diagnostics use a separate, smaller budget. Operators
should check their actual event/response sizes before rollout and deliberately
configure a larger budget if required.

Reads detect overflow from actual bytes, including chunked requests/responses
and absent Content-Length. They read at most the budget plus one byte to
separate an exact-size body from an oversized one. A body exactly at the budget
is accepted if otherwise valid. Webhook signature verification receives the
original bytes, including whitespace; it never receives truncated or re-encoded
JSON. [Stripe requires the unmodified request body](https://docs.stripe.com/webhooks/signature).

All orchestrator response bodies are closed after success, rejection, read
failure or JSON failure. The HTTP server owns closing incoming webhook request
bodies. A webhook read failure dispatches no event. A submission-response failure
can occur after the orchestrator has already created a job: this change adds no
automatic retry or idempotency guarantee.

## Configuration in callers

Existing calls keep working with defaults. Go callers can override budgets:

```go
err := payment.HandleV1Event(w, r, secret, handler,
    payment.WebhookOptions{MaxBodyBytes: 128 << 10})

// The thin-event handler accepts the same optional WebhookOptions.
err = stripeClient.HandleThinEvent(w, r, secret, thinHandler,
    payment.WebhookOptions{MaxBodyBytes: 128 << 10})

orch := orchclient.New(baseURL, orchclient.BodyLimits{
    SuccessBytes: 2 << 20,
    ErrorBytes:   8 << 10,
})
```

Zero fields retain defaults. When multiple option structs are supplied, the
last nonzero value for each field wins. Negative budgets fail before reading a
webhook or sending a submission. Orchestrator budgets must also be less than
`math.MaxInt64`, leaving room for the overflow-detection byte. Configure clients
before concurrent use. The portal binary currently uses defaults; no new
environment variables or live Stripe settings are introduced.

Signature failures retain HTTP 400; valid unhandled event types retain HTTP 200.
Oversized payloads return HTTP 413 without exposing their contents. Custom thin
webhook endpoints should likewise map `*http.MaxBytesError` using `errors.As`.
Within-budget upstream error bodies retain their existing diagnostic behavior;
oversized bodies are omitted entirely rather than published as truncated JSON.

## Verification

```sh
go test -race ./internal/payment ./internal/orchclient ./internal/portal
go test -race ./...
go vet ./...
go build ./...
```

Regression tests exercise real Stripe signature verification, below/exact/over
budgets, declared and chunked HTTP bodies, altered signed bytes, read failures,
separate configurable success/error budgets, bounded diagnostics, maximum bytes
consumed, and response closure. Fixtures use local HTTP servers and synthetic
Stripe events, without contacting a payment service.

The limits add no dependency, change no fee/settlement rule or conformance
binding, and preserve the portal's transitional frontend ownership described in
`CONFORMANCE.md`. Availability-preview PR #28 touches the same client; any
integration should preserve these bounds for submission and separately assess
the new preview response shape.

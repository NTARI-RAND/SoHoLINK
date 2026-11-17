package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

const testWebhookSecret = "whsec_local_fixture"

func signedRequest(payload string) *http.Request {
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: []byte(payload), Secret: testWebhookSecret})
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("Stripe-Signature", signed.Header)
	return req
}

func v1Payload() string {
	return "{\n  \"id\":\"evt_fixture\", \"object\":\"event\", \"type\":\"test.event\",\n \"api_version\":\"" + stripe.APIVersion + "\", \"data\":{\"object\":{\"id\":\"obj_fixture\"}}\n}\n"
}

func TestWebhookBodyLimit(t *testing.T) {
	for _, thin := range []bool{false, true} {
		name := "v1"
		if thin {
			name = "thin"
		}
		t.Run(name, func(t *testing.T) {
			for _, size := range []int{65535, 65536, 65537} {
				for _, noLength := range []bool{false, true} {
					t.Run(strings.Join([]string{sizeLabel(size), map[bool]string{false: "declared", true: "no-length"}[noLength]}, "/"), func(t *testing.T) {
						payload := v1Payload()
						payload += strings.Repeat(" ", size-len(payload))
						req := signedRequest(payload)
						if noLength {
							req.ContentLength = -1
							req.TransferEncoding = []string{"chunked"}
						}
						calls := 0
						var err error
						if thin {
							err = New("sk_test_local").HandleThinEvent(httptest.NewRecorder(), req, testWebhookSecret, func(_ context.Context, event *stripe.ThinEvent) error {
								calls++
								if event.ID != "evt_fixture" {
									t.Errorf("event ID = %q", event.ID)
								}
								return nil
							})
						} else {
							err = HandleV1Event(httptest.NewRecorder(), req, testWebhookSecret, func(event stripe.Event) error {
								calls++
								if event.ID != "evt_fixture" {
									t.Errorf("event ID = %q", event.ID)
								}
								return nil
							})
						}
						if size > 65536 {
							var tooLarge *http.MaxBytesError
							if !errors.As(err, &tooLarge) {
								t.Errorf("want body-limit error, got %v", err)
							}
							if calls != 0 {
								t.Errorf("oversized event dispatched %d times", calls)
							}
						} else if err != nil || calls != 1 {
							t.Errorf("signed event: calls=%d err=%v", calls, err)
						}
					})
				}
			}
		})
	}
}

func sizeLabel(size int) string {
	switch size {
	case 65535:
		return "below"
	case 65536:
		return "exact"
	default:
		return "over"
	}
}

// Removing signature validation must not allow altered bytes to reach dispatch.
func TestWebhookRejectsChangedRawBytes(t *testing.T) {
	for _, thin := range []bool{false, true} {
		req := signedRequest(v1Payload())
		req.Body = io.NopCloser(strings.NewReader(strings.Replace(v1Payload(), "evt_fixture", "evt_changed", 1)))
		called := false
		var err error
		if thin {
			err = New("sk_test_local").HandleThinEvent(httptest.NewRecorder(), req, testWebhookSecret, func(context.Context, *stripe.ThinEvent) error { called = true; return nil })
		} else {
			err = HandleV1Event(httptest.NewRecorder(), req, testWebhookSecret, func(stripe.Event) error { called = true; return nil })
		}
		if err == nil || called {
			t.Errorf("altered payload: called=%v err=%v", called, err)
		}
	}
}

func TestWebhookConfiguredLimit(t *testing.T) {
	payload := v1Payload()
	for _, thin := range []bool{false, true} {
		for _, limit := range []int64{int64(len(payload) - 1), int64(len(payload)), -1} {
			req := signedRequest(payload)
			called := false
			var err error
			options := WebhookOptions{MaxBodyBytes: limit}
			if thin {
				err = New("sk_test_local").HandleThinEvent(httptest.NewRecorder(), req, testWebhookSecret, func(context.Context, *stripe.ThinEvent) error { called = true; return nil }, options)
			} else {
				err = HandleV1Event(httptest.NewRecorder(), req, testWebhookSecret, func(stripe.Event) error { called = true; return nil }, options)
			}
			if limit == int64(len(payload)) {
				if err != nil || !called {
					t.Errorf("exact configured limit: called=%v err=%v", called, err)
				}
			} else if err == nil || called {
				t.Errorf("limit=%d: called=%v err=%v", limit, called, err)
			}
		}
	}
}

func TestWebhookHTTPBodyLimit(t *testing.T) {
	for _, thin := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "thin"}[thin], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				if thin {
					err = New("sk_test_local").HandleThinEvent(w, r, testWebhookSecret, func(context.Context, *stripe.ThinEvent) error { return nil })
				} else {
					err = HandleV1Event(w, r, testWebhookSecret, func(stripe.Event) error { return nil })
				}
				var tooLarge *http.MaxBytesError
				switch {
				case errors.As(err, &tooLarge):
					http.Error(w, "too large", 413)
				case err != nil:
					http.Error(w, "invalid event", 400)
				default:
					w.WriteHeader(204)
				}
			}))
			defer srv.Close()
			for _, size := range []int{65536, 65537} {
				for _, chunked := range []bool{false, true} {
					payload := v1Payload() + strings.Repeat(" ", size-len(v1Payload()))
					req := signedRequest(payload)
					req.URL, _ = url.Parse(srv.URL)
					req.RequestURI = ""
					if chunked {
						req.ContentLength = -1
						req.TransferEncoding = []string{"chunked"}
					}
					resp, err := srv.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					_ = resp.Body.Close()
					want := 204
					if size > 65536 {
						want = 413
					}
					if resp.StatusCode != want {
						t.Errorf("size=%d chunked=%v: status=%d want=%d", size, chunked, resp.StatusCode, want)
					}
				}
			}
		})
	}
}

type failedWebhookBody struct{}

func (failedWebhookBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedWebhookBody) Close() error             { return nil }

func TestWebhookReadFailureDoesNotDispatch(t *testing.T) {
	for _, thin := range []bool{false, true} {
		req := signedRequest(v1Payload())
		req.Body = failedWebhookBody{}
		called := false
		var err error
		if thin {
			err = New("sk_test_local").HandleThinEvent(httptest.NewRecorder(), req, testWebhookSecret, func(context.Context, *stripe.ThinEvent) error { called = true; return nil })
		} else {
			err = HandleV1Event(httptest.NewRecorder(), req, testWebhookSecret, func(stripe.Event) error { called = true; return nil })
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) || called {
			t.Errorf("read failure: called=%v err=%v", called, err)
		}
	}
}

type countedWebhookBody struct {
	io.Reader
	read int
}

func (b *countedWebhookBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (*countedWebhookBody) Close() error { return nil }

// Checking size after an unbounded read must fail this resource-use regression.
func TestWebhookStopsReadingAtLimit(t *testing.T) {
	for _, thin := range []bool{false, true} {
		body := &countedWebhookBody{Reader: strings.NewReader(strings.Repeat("x", 131072))}
		req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
		req.Body = body
		req.ContentLength = -1
		called := false
		var err error
		if thin {
			err = New("sk_test_local").HandleThinEvent(httptest.NewRecorder(), req, testWebhookSecret, func(context.Context, *stripe.ThinEvent) error { called = true; return nil })
		} else {
			err = HandleV1Event(httptest.NewRecorder(), req, testWebhookSecret, func(stripe.Event) error { called = true; return nil })
		}
		var tooLarge *http.MaxBytesError
		if !errors.As(err, &tooLarge) || called {
			t.Errorf("overflow: called=%v err=%v", called, err)
		}
		if body.read > 65537 {
			t.Errorf("read=%d want at most 65537", body.read)
		}
	}
}

package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripeWebhookOversizeReturns413(t *testing.T) {
	ps := &PortalServer{webhookSecret: "whsec_local_fixture"}
	for _, declared := range []bool{true, false} {
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(strings.Repeat("x", 65537)))
		if !declared {
			req.ContentLength = -1
		}
		w := httptest.NewRecorder()
		ps.handleStripeWebhook(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("declared=%v: status=%d want 413", declared, w.Code)
		}
		if strings.Contains(w.Body.String(), strings.Repeat("x", 100)) {
			t.Error("response leaks request body")
		}
	}
}

func TestStripeWebhookBadSignatureReturns400(t *testing.T) {
	ps := &PortalServer{webhookSecret: "whsec_local_fixture"}
	payload := `{"id":"evt_private_fixture","type":"account.updated","data":{"object":{"id":"acct_private"}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("Stripe-Signature", "invalid-signature")
	w := httptest.NewRecorder()
	ps.handleStripeWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad signature: status=%d want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "private") {
		t.Error("signature failure response leaks request body")
	}
}

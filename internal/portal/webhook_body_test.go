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

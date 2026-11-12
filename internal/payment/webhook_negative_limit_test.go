package payment

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookRejectsNegativeBudget(t *testing.T) {
	body := &countedWebhookBody{Reader: strings.NewReader("unread-body")}
	req := signedRequest(v1Payload())
	req.Body = body
	_, err := readWebhookBody(httptest.NewRecorder(), req, []WebhookOptions{{MaxBodyBytes: -1}})
	if err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("negative limit must be positive validation: got %v", err)
	}
	if body.read != 0 {
		t.Errorf("invalid option read %d body bytes, want none", body.read)
	}
}

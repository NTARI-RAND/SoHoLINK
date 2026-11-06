package portal

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripeWebhookRejectionDebugLog(t *testing.T) {
	const privateMarker = "PRIVATE_WEBHOOK_PAYLOAD"
	for _, tc := range []struct {
		name, payload string
		level         slog.Level
		status        int
	}{
		{"signature/debug", privateMarker, slog.LevelDebug, http.StatusBadRequest},
		{"oversize/debug", privateMarker + strings.Repeat("x", 65537), slog.LevelDebug, http.StatusRequestEntityTooLarge},
		{"signature/info", privateMarker, slog.LevelInfo, http.StatusBadRequest},
		{"oversize/info", privateMarker + strings.Repeat("x", 65537), slog.LevelInfo, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: tc.level})))
			defer slog.SetDefault(previous)
			ps := &PortalServer{webhookSecret: "whsec_local_fixture"}
			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(tc.payload))
			req.Header.Set("Stripe-Signature", "private-signature")
			ps.handleStripeWebhook(httptest.NewRecorder(), req)
			if tc.level == slog.LevelInfo {
				if output.Len() != 0 {
					t.Fatal("unauthenticated rejection logged above debug level")
				}
				return
			}
			assertWebhookRejectionLog(t, &output, tc.status, privateMarker)
		})
	}
}

func assertWebhookRejectionLog(t *testing.T, output *bytes.Buffer, status int, privateMarker string) {
	t.Helper()
	if output.Len() == 0 {
		t.Fatal("rejection has no debug log")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["status"] != float64(status) || record["error"] == nil || record["error"] == "" {
		t.Fatalf("rejection lacks status/error: %v", record)
	}
	if strings.Contains(output.String(), privateMarker) || strings.Contains(output.String(), "private-signature") {
		t.Fatal("debug log leaks webhook body or signature")
	}
}

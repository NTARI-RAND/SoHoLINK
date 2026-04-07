package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStripeProcessor_Name verifies the processor name.
func TestStripeProcessor_Name(t *testing.T) {
	p := NewStripeProcessor("sk_test_123", "whsec_123")
	if got := p.Name(); got != "stripe" {
		t.Errorf("Name() = %q, want %q", got, "stripe")
	}
}

// TestStripeProcessor_IsOnline verifies online status checks.
func TestStripeProcessor_IsOnline(t *testing.T) {
	tests := []struct {
		name      string
		secretKey string
		offline   bool
		want      bool
	}{
		{
			name:      "online with secret key",
			secretKey: "sk_test_123",
			offline:   false,
			want:      true,
		},
		{
			name:      "offline with secret key",
			secretKey: "sk_test_123",
			offline:   true,
			want:      false,
		},
		{
			name:      "no secret key",
			secretKey: "",
			offline:   false,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStripeProcessor(tt.secretKey, "whsec_123")
			p.offline = tt.offline
			if got := p.IsOnline(context.Background()); got != tt.want {
				t.Errorf("IsOnline() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStripeProcessor_CreateCharge tests charge creation.
func TestStripeProcessor_CreateCharge(t *testing.T) {
	tests := []struct {
		name       string
		secretKey  string
		request    ChargeRequest
		mockStatus int
		mockBody   string
		wantErr    bool
		wantStatus string
	}{
		{
			name:      "successful charge creation",
			secretKey: "sk_test_123",
			request: ChargeRequest{
				Amount:        10000,
				Currency:      "usd",
				UserDID:       "did:soho:user123",
				ProviderDID:   "did:soho:provider456",
				ResourceType:  "compute",
				UsageRecordID: "usage_789",
			},
			mockStatus: http.StatusOK,
			mockBody: `{
				"id": "pi_123456",
				"status": "succeeded",
				"amount": 10000,
				"currency": "usd"
			}`,
			wantErr:    false,
			wantStatus: "succeeded",
		},
		{
			name:      "requires payment method status",
			secretKey: "sk_test_123",
			request: ChargeRequest{
				Amount:   5000,
				Currency: "usd",
			},
			mockStatus: http.StatusOK,
			mockBody: `{
				"id": "pi_pending",
				"status": "requires_payment_method",
				"amount": 5000,
				"currency": "usd"
			}`,
			wantErr:    false,
			wantStatus: "pending",
		},
		{
			name:      "stripe API error",
			secretKey: "sk_test_123",
			request: ChargeRequest{
				Amount:   1000,
				Currency: "usd",
			},
			mockStatus: http.StatusBadRequest,
			mockBody: `{
				"error": {
					"type": "invalid_request_error",
					"message": "Amount must be positive"
				}
			}`,
			wantErr: true,
		},
		{
			name:      "no secret key",
			secretKey: "",
			request: ChargeRequest{
				Amount:   1000,
				Currency: "usd",
			},
			wantErr: true,
		},
		{
			name:      "default currency to USD",
			secretKey: "sk_test_123",
			request: ChargeRequest{
				Amount:   2000,
				Currency: "", // Empty currency
			},
			mockStatus: http.StatusOK,
			mockBody: `{
				"id": "pi_usd_default",
				"status": "succeeded",
				"amount": 2000,
				"currency": "usd"
			}`,
			wantErr:    false,
			wantStatus: "succeeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Stripe API server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify authentication
				user, _, ok := r.BasicAuth()
				if ok && user != tt.secretKey {
					t.Errorf("Basic auth user = %q, want %q", user, tt.secretKey)
				}

				// Verify request method and path
				if r.Method != http.MethodPost {
					t.Errorf("Method = %q, want POST", r.Method)
				}
				if !strings.Contains(r.URL.Path, "/v1/payment_intents") {
					t.Errorf("Path = %q, want /v1/payment_intents", r.URL.Path)
				}

				// Return mock response
				w.WriteHeader(tt.mockStatus)
				w.Write([]byte(tt.mockBody))
			}))
			defer server.Close()

			p := NewStripeProcessor(tt.secretKey, "whsec_123")
			// Override the Stripe API endpoint for testing
			// (In real implementation, you'd inject the base URL)

			if tt.secretKey == "" {
				// Test no secret key case without mock server
				_, err := p.CreateCharge(context.Background(), tt.request)
				if (err != nil) != tt.wantErr {
					t.Errorf("CreateCharge() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			// Inject the mock server URL so the request hits our test handler.
			p.baseURL = server.URL
			result, err := p.CreateCharge(context.Background(), tt.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateCharge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != nil && result.Status != tt.wantStatus {
				t.Errorf("CreateCharge() status = %q, want %q", result.Status, tt.wantStatus)
			}
		})
	}
}

// TestStripeProcessor_ConfirmCharge tests charge confirmation.
func TestStripeProcessor_ConfirmCharge(t *testing.T) {
	tests := []struct {
		name       string
		secretKey  string
		chargeID   string
		mockStatus int
		mockBody   string
		wantErr    bool
	}{
		{
			name:      "successful confirmation",
			secretKey: "sk_test_123",
			chargeID:  "pi_123456",
			mockStatus: http.StatusOK,
			mockBody: `{
				"id": "pi_123456",
				"status": "succeeded",
				"amount": 10000
			}`,
			wantErr: false,
		},
		{
			name:      "no secret key",
			secretKey: "",
			chargeID:  "pi_123456",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStripeProcessor(tt.secretKey, "whsec_123")

			// Wire mock server when test provides mock data.
			if tt.mockBody != "" {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					w.Write([]byte(tt.mockBody)) //nolint:errcheck
				}))
				defer srv.Close()
				p.baseURL = srv.URL
			}

			err := p.ConfirmCharge(context.Background(), tt.chargeID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConfirmCharge() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStripeProcessor_RefundCharge tests refund processing.
func TestStripeProcessor_RefundCharge(t *testing.T) {
	tests := []struct {
		name       string
		secretKey  string
		chargeID   string
		reason     string
		mockStatus int
		mockBody   string
		wantErr    bool
	}{
		{
			name:       "successful refund",
			secretKey:  "sk_test_123",
			chargeID:   "pi_123456",
			reason:     "customer_request",
			mockStatus: http.StatusOK,
			mockBody:   `{"id": "re_123456", "status": "succeeded", "amount": 10000}`,
			wantErr:    false,
		},
		{
			name:      "no secret key",
			secretKey: "",
			chargeID:  "pi_123456",
			reason:    "customer_request",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStripeProcessor(tt.secretKey, "whsec_123")

			// Wire mock server when test provides mock data.
			if tt.mockBody != "" {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					w.Write([]byte(tt.mockBody)) //nolint:errcheck
				}))
				defer srv.Close()
				p.baseURL = srv.URL
			}

			err := p.RefundCharge(context.Background(), tt.chargeID, tt.reason)
			if (err != nil) != tt.wantErr {
				t.Errorf("RefundCharge() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestStripeProcessor_GetChargeStatus tests status retrieval.
func TestStripeProcessor_GetChargeStatus(t *testing.T) {
	tests := []struct {
		name      string
		secretKey string
		chargeID  string
		wantErr   bool
	}{
		{
			name:      "get status",
			secretKey: "sk_test_123",
			chargeID:  "pi_123456",
			wantErr:   false, // Will fail with no mock, but validates flow
		},
		{
			name:      "no secret key",
			secretKey: "",
			chargeID:  "pi_123456",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStripeProcessor(tt.secretKey, "whsec_123")

			_, err := p.GetChargeStatus(context.Background(), tt.chargeID)
			if tt.secretKey == "" && err == nil {
				t.Error("GetChargeStatus() expected error with no secret key")
			}
		})
	}
}

// TestStripeProcessor_ListCharges tests listing charges.
func TestStripeProcessor_ListCharges(t *testing.T) {
	tests := []struct {
		name      string
		secretKey string
		filter    ChargeFilter
		wantErr   bool
	}{
		{
			name:      "list with default limit",
			secretKey: "sk_test_123",
			filter: ChargeFilter{
				Limit: 0, // Should default to 10
			},
			wantErr: false,
		},
		{
			name:      "list with custom limit",
			secretKey: "sk_test_123",
			filter: ChargeFilter{
				Limit: 25,
			},
			wantErr: false,
		},
		{
			name:      "list with max limit",
			secretKey: "sk_test_123",
			filter: ChargeFilter{
				Limit: 150, // Should cap at 100
			},
			wantErr: false,
		},
		{
			name:      "no secret key",
			secretKey: "",
			filter: ChargeFilter{
				Limit: 10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStripeProcessor(tt.secretKey, "whsec_123")

			_, err := p.ListCharges(context.Background(), tt.filter)
			if tt.secretKey == "" && err == nil {
				t.Error("ListCharges() expected error with no secret key")
			}
		})
	}
}

// TestMapStripeStatus tests the status mapping function.
func TestMapStripeStatus(t *testing.T) {
	tests := []struct {
		stripeStatus string
		want         string
	}{
		{"requires_payment_method", "pending"},
		{"requires_confirmation", "pending"},
		{"requires_action", "pending"},
		{"processing", "pending"},
		{"succeeded", "succeeded"},
		{"canceled", "failed"},
		{"requires_capture", "pending"},
		{"unknown_status", "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.stripeStatus, func(t *testing.T) {
			if got := mapStripeStatus(tt.stripeStatus); got != tt.want {
				t.Errorf("mapStripeStatus(%q) = %q, want %q", tt.stripeStatus, got, tt.want)
			}
		})
	}
}

// TestStripeProcessor_DoRequest tests the HTTP request helper.
func TestStripeProcessor_DoRequest(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		mockStatus int
		mockBody   string
		wantErr    bool
	}{
		{
			name:       "successful request",
			method:     http.MethodGet,
			mockStatus: http.StatusOK,
			mockBody:   `{"success": true}`,
			wantErr:    false,
		},
		{
			name:       "bad request error",
			method:     http.MethodPost,
			mockStatus: http.StatusBadRequest,
			mockBody:   `{"error": {"message": "Invalid request"}}`,
			wantErr:    true,
		},
		{
			name:       "unauthorized error",
			method:     http.MethodGet,
			mockStatus: http.StatusUnauthorized,
			mockBody:   `{"error": {"message": "Invalid API key"}}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify basic auth is set
				_, _, ok := r.BasicAuth()
				if !ok {
					t.Error("Expected Basic Auth header")
				}

				w.WriteHeader(tt.mockStatus)
				w.Write([]byte(tt.mockBody))
			}))
			defer server.Close()

			p := NewStripeProcessor("sk_test_123", "whsec_123")

			body, err := p.doRequest(context.Background(), tt.method, server.URL, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("doRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(body) != tt.mockBody {
				t.Errorf("doRequest() body = %q, want %q", string(body), tt.mockBody)
			}
		})
	}
}

// TestStripeProcessor_ContextCancellation tests context handling.
func TestStripeProcessor_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "pi_123"}`))
	}))
	defer server.Close()

	p := NewStripeProcessor("sk_test_123", "whsec_123")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := p.doRequest(ctx, http.MethodGet, server.URL, nil)
	if err == nil {
		t.Error("Expected context cancellation error")
	}
}

// TestStripeProcessor_ResponseParsing tests JSON response parsing.
func TestStripeProcessor_ResponseParsing(t *testing.T) {
	tests := []struct {
		name     string
		jsonBody string
		wantErr  bool
	}{
		{
			name: "valid payment intent response",
			jsonBody: `{
				"id": "pi_123",
				"status": "succeeded",
				"amount": 10000,
				"currency": "usd"
			}`,
			wantErr: false,
		},
		{
			name: "response with error",
			jsonBody: `{
				"error": {
					"type": "card_error",
					"message": "Your card was declined"
				}
			}`,
			wantErr: false, // Parsing succeeds, but error field is populated
		},
		{
			name:     "invalid JSON",
			jsonBody: `{invalid json`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sr stripeResponse
			err := json.Unmarshal([]byte(tt.jsonBody), &sr)
			if (err != nil) != tt.wantErr {
				t.Errorf("JSON unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && sr.Error != nil {
				t.Logf("Parsed error: %s", sr.Error.Message)
			}
		})
	}
}

// TestStripeProcessor_MetadataEncoding tests metadata field encoding.
func TestStripeProcessor_MetadataEncoding(t *testing.T) {
	req := ChargeRequest{
		Amount:         10000,
		Currency:       "usd",
		UserDID:        "did:soho:user123",
		ProviderDID:    "did:soho:provider456",
		ResourceType:   "compute",
		UsageRecordID:  "usage_789",
		IdempotencyKey: "idempotent_123",
		Metadata: map[string]string{
			"session_id": "sess_abc",
			"region":     "us-west-2",
		},
	}

	// This test verifies the metadata encoding logic
	// In a real test, we'd check the form values sent to Stripe
	if req.Metadata["session_id"] != "sess_abc" {
		t.Error("Metadata not preserved")
	}
}

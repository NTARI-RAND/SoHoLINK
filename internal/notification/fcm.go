// Package notification — FCM v1 notifier for Android background wakeup.
//
// Mirrors the APNs notifier structure exactly (apns.go). Uses the Firebase
// HTTP v1 API with a GCP service-account JWT for authentication.
// No external dependencies beyond the Go standard library.
package notification

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	fcmTokenEndpoint = "https://oauth2.googleapis.com/token"
	fcmScope         = "https://www.googleapis.com/auth/firebase.messaging"
	fcmJWTLifetime   = 55 * time.Minute // Google tokens last 60 min; refresh at 55
)

// ErrFCMDeviceTokenInvalid is returned when FCM responds with UNREGISTERED.
// Callers should remove the token from storage.
var ErrFCMDeviceTokenInvalid = errors.New("fcm: device token is no longer registered")

// FCMConfig holds the GCP service account credentials from the Firebase Console.
type FCMConfig struct {
	// ProjectID is the Firebase project ID (e.g. "soholink-prod").
	ProjectID string

	// ServiceAccountJSON is the raw JSON content of the GCP service account key
	// downloaded from the Firebase Console → Project Settings → Service Accounts.
	ServiceAccountJSON string
}

// serviceAccountKey holds the fields we need from the service account JSON.
type serviceAccountKey struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// FCMNotifier sends Firebase Cloud Messaging push notifications using the
// FCM HTTP v1 API. Safe for concurrent use.
type FCMNotifier struct {
	cfg        FCMConfig
	sa         serviceAccountKey
	privateKey *rsa.PrivateKey

	mu         sync.Mutex
	cachedToken string
	tokenExpiry time.Time

	client *http.Client
}

// NewFCMNotifier parses the service account JSON and returns a ready notifier.
func NewFCMNotifier(cfg FCMConfig) (*FCMNotifier, error) {
	var sa serviceAccountKey
	if err := json.Unmarshal([]byte(cfg.ServiceAccountJSON), &sa); err != nil {
		return nil, fmt.Errorf("fcm: parse service account JSON: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("fcm: service account JSON missing client_email or private_key")
	}

	privKey, err := parseRSAPrivateKey(sa.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("fcm: parse private key: %w", err)
	}

	return &FCMNotifier{
		cfg:        cfg,
		sa:         sa,
		privateKey: privKey,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// SendJobRequest sends a data-only FCM message that wakes the Android app
// without displaying a visible notification. The app's onBackgroundMessage
// handler receives this and triggers a WebSocket reconnect.
func (n *FCMNotifier) SendJobRequest(ctx context.Context, fcmToken, taskID, workloadID string) error {
	return n.send(ctx, fcmToken, map[string]string{
		"event_type":  "job_request",
		"task_id":     taskID,
		"workload_id": workloadID,
	}, nil)
}

// SendPaymentReceived sends a visible notification informing the user that
// a payment has arrived.
func (n *FCMNotifier) SendPaymentReceived(ctx context.Context, fcmToken string, amountSats int64) error {
	return n.send(ctx, fcmToken, map[string]string{
		"event_type":  "payment_received",
		"amount_sats": fmt.Sprintf("%d", amountSats),
	}, &fcmNotification{
		Title: "Payment received",
		Body:  fmt.Sprintf("%d sats credited to your node", amountSats),
	})
}

// ── Internal ─────────────────────────────────────────────────────────────────

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fcmMessage struct {
	Token        string              `json:"token"`
	Data         map[string]string   `json:"data,omitempty"`
	Notification *fcmNotification    `json:"notification,omitempty"`
}

type fcmRequest struct {
	Message fcmMessage `json:"message"`
}

func (n *FCMNotifier) send(ctx context.Context, fcmToken string, data map[string]string, notif *fcmNotification) error {
	token, err := n.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("fcm: obtain access token: %w", err)
	}

	msg := fcmMessage{Token: fcmToken, Data: data, Notification: notif}
	body, err := json.Marshal(fcmRequest{Message: msg})
	if err != nil {
		return fmt.Errorf("fcm: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", n.cfg.ProjectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	var errBody struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)

	if errBody.Error.Status == "UNREGISTERED" {
		return fmt.Errorf("%w: %s", ErrFCMDeviceTokenInvalid, fcmToken)
	}
	return fmt.Errorf("fcm: HTTP %d: %s", resp.StatusCode, errBody.Error.Message)
}

// getAccessToken returns a cached or freshly minted OAuth2 token.
func (n *FCMNotifier) getAccessToken(ctx context.Context) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.cachedToken != "" && time.Now().Before(n.tokenExpiry) {
		return n.cachedToken, nil
	}

	now := time.Now()
	exp := now.Add(fcmJWTLifetime + 5*time.Minute) // slightly over lifetime for the request to arrive

	// Build JWT header + payload.
	header := base64RawJSON(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload := base64RawJSON(map[string]interface{}{
		"iss":   n.sa.ClientEmail,
		"sub":   n.sa.ClientEmail,
		"aud":   fcmTokenEndpoint,
		"scope": fcmScope,
		"iat":   now.Unix(),
		"exp":   exp.Unix(),
	})

	sigInput := header + "." + payload
	digest := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, n.privateKey, 0, digest[:])
	if err != nil {
		return "", fmt.Errorf("fcm: sign JWT: %w", err)
	}

	assertion := sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	// Exchange JWT assertion for an access token.
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fcmTokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm: token request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("fcm: decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", errors.New("fcm: empty access token in response")
	}

	n.cachedToken = tokenResp.AccessToken
	n.tokenExpiry = now.Add(fcmJWTLifetime)
	return n.cachedToken, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func base64RawJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("fcm: private key is not RSA")
		}
		return rk, nil
	default:
		return nil, fmt.Errorf("fcm: unsupported PEM type %q", block.Type)
	}
}

// Suppress unused import warning for math/big (used implicitly via crypto/rsa).
var _ = big.NewInt

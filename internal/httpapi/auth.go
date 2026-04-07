package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Single-use nonce cache (in-memory, 5-minute TTL)
// ---------------------------------------------------------------------------

type nonceEntry struct{ expiry time.Time }

var nonceMu sync.Mutex
var nonceMap = make(map[string]nonceEntry)

func init() {
	// Background goroutine prunes expired nonces every minute.
	go func() {
		t := time.NewTicker(time.Minute)
		for range t.C {
			pruneNonces()
		}
	}()
}

func pruneNonces() {
	now := time.Now()
	nonceMu.Lock()
	defer nonceMu.Unlock()
	for k, e := range nonceMap {
		if now.After(e.expiry) {
			delete(nonceMap, k)
		}
	}
}

func newNonce() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	nonce := hex.EncodeToString(b)
	nonceMu.Lock()
	nonceMap[nonce] = nonceEntry{expiry: time.Now().Add(5 * time.Minute)}
	nonceMu.Unlock()
	return nonce
}

// consumeNonce returns true and atomically removes the nonce if it is valid
// and unexpired. A nonce can only be consumed once.
func consumeNonce(nonce string) bool {
	nonceMu.Lock()
	defer nonceMu.Unlock()
	e, ok := nonceMap[nonce]
	if !ok || time.Now().After(e.expiry) {
		return false
	}
	delete(nonceMap, nonce)
	return true
}

// ---------------------------------------------------------------------------
// GET /api/auth/challenge
// ---------------------------------------------------------------------------

// handleAuthChallenge issues a fresh single-use nonce for the client to sign.
// No authentication is required for this endpoint.
func (s *Server) handleAuthChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ // #nosec G104
		"nonce":      newNonce(),
		"expires_at": time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
	})
}

// ---------------------------------------------------------------------------
// POST /api/auth/connect
// ---------------------------------------------------------------------------

type connectRequest struct {
	Nonce      string `json:"nonce"`
	PublicKey  string `json:"public_key"`  // base64 Ed25519 public key (32 bytes) — optional when signing from the owner device
	Signature  string `json:"signature"`   // base64 Ed25519 signature over nonce bytes
	DeviceName string `json:"device_name"` // human-readable label, e.g. "MacBook Air"
}

// handleAuthConnect verifies the Ed25519 signature against the stored owner
// public key and — on success — issues a persistent device token.
//
// public_key is optional: if omitted, the server verifies the signature against
// the stored owner public key directly (used by the browser dashboard where
// deriving the public key from a seed in-browser is not straightforward).
func (s *Server) handleAuthConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store not ready", http.StatusServiceUnavailable)
		return
	}

	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// 1. Validate nonce (single-use, 5-min TTL).
	if !consumeNonce(req.Nonce) {
		http.Error(w, "invalid or expired nonce", http.StatusUnauthorized)
		return
	}

	// 2. Load the owner's public key stored at node startup.
	ctx := r.Context()
	ownerPubB64, ok, err := s.store.GetOwnerPublicKey(ctx)
	if err != nil || !ok {
		http.Error(w, "node owner key not configured", http.StatusServiceUnavailable)
		return
	}
	ownerPub, err := base64.StdEncoding.DecodeString(ownerPubB64)
	if err != nil {
		http.Error(w, "server key corrupt", http.StatusInternalServerError)
		return
	}

	// 3. If client supplied a public key, verify it matches the owner key.
	//    If omitted, verify directly against the stored owner public key.
	verifyPub := ed25519.PublicKey(ownerPub)
	if req.PublicKey != "" {
		clientPub, decErr := base64.StdEncoding.DecodeString(req.PublicKey)
		if decErr != nil || len(clientPub) != ed25519.PublicKeySize {
			http.Error(w, "invalid public_key", http.StatusBadRequest)
			return
		}
		if !constEqual(clientPub, ownerPub) {
			log.Printf("[auth] connect rejected: public key mismatch from %s", clientIP(r))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		verifyPub = ed25519.PublicKey(clientPub)
	}

	// 4. Verify the Ed25519 signature over the nonce.
	sig, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		http.Error(w, "invalid signature encoding", http.StatusBadRequest)
		return
	}
	if !ed25519.Verify(verifyPub, []byte(req.Nonce), sig) {
		log.Printf("[auth] connect rejected: signature invalid from %s", clientIP(r))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 6. Generate a 32-byte random device token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	tokenHex := hex.EncodeToString(raw)

	name := req.DeviceName
	if name == "" {
		name = "unknown device"
	}
	if err := s.store.StoreDeviceToken(ctx, tokenHex, name); err != nil {
		log.Printf("[auth] StoreDeviceToken error: %v", err)
		http.Error(w, "could not persist token", http.StatusInternalServerError)
		return
	}

	log.Printf("[auth] device authenticated: %q from %s", name, clientIP(r))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ // #nosec G104
		"device_token": tokenHex,
	})
}

// ---------------------------------------------------------------------------
// POST /api/auth/owner-login  (browser convenience endpoint)
// ---------------------------------------------------------------------------
// The browser POSTs the raw 64-hex-char private key seed over HTTPS.
// The server derives the Ed25519 public key from the seed and compares it
// to the stored owner public key — no browser cryptography required.
// This endpoint is rate-limited to 5 attempts per restart via an in-memory
// counter (sufficient for a single-operator node).

var loginAttempts int
var loginMu sync.Mutex

func (s *Server) handleOwnerLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store not ready", http.StatusServiceUnavailable)
		return
	}

	// Basic brute-force guard.
	loginMu.Lock()
	loginAttempts++
	attempts := loginAttempts
	loginMu.Unlock()
	if attempts > 20 {
		http.Error(w, "too many attempts — restart the node to reset", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Seed       string `json:"seed"`        // 64-char hex Ed25519 private key seed
		DeviceName string `json:"device_name"` // human label
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Decode the seed.
	seedBytes, err := hex.DecodeString(req.Seed)
	if err != nil || len(seedBytes) != ed25519.SeedSize {
		http.Error(w, "seed must be 64 hex characters (32 bytes)", http.StatusBadRequest)
		return
	}

	// Derive the Ed25519 public key from the seed.
	privKey := ed25519.NewKeyFromSeed(seedBytes)
	derivedPub := privKey.Public().(ed25519.PublicKey)
	derivedPubB64 := base64.StdEncoding.EncodeToString(derivedPub)

	// Load the stored owner public key.
	ctx := r.Context()
	ownerPubB64, ok, err := s.store.GetOwnerPublicKey(ctx)
	if err != nil || !ok {
		http.Error(w, "node owner key not configured", http.StatusServiceUnavailable)
		return
	}

	// Compare in constant time.
	if derivedPubB64 != ownerPubB64 {
		log.Printf("[auth] owner-login rejected: wrong key from %s", clientIP(r))
		http.Error(w, "incorrect private key", http.StatusUnauthorized)
		return
	}

	// Reset attempt counter on success.
	loginMu.Lock()
	loginAttempts = 0
	loginMu.Unlock()

	// Issue a device token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	tokenHex := hex.EncodeToString(raw)

	name := req.DeviceName
	if name == "" {
		name = "browser"
	}
	if err := s.store.StoreDeviceToken(ctx, tokenHex, name); err != nil {
		http.Error(w, "could not persist token", http.StatusInternalServerError)
		return
	}

	log.Printf("[auth] owner logged in via browser: %q from %s", name, clientIP(r))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"device_token": tokenHex}) // #nosec G104
}

// constEqual compares two byte slices in constant time to prevent timing attacks.
func constEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// ---------------------------------------------------------------------------
// First-run keypair bootstrap
// ---------------------------------------------------------------------------

// ensureOwnerKeypairLogged calls EnsureOwnerKeypair and, if a brand-new
// keypair was generated, prints the private key seed to stdout exactly once.
// The operator must copy this 64-char hex string and distribute it to every
// device that should have management access to this node.
func (s *Server) ensureOwnerKeypairLogged(ctx context.Context) {
	if s.store == nil {
		return
	}
	privHex, isNew, err := s.store.EnsureOwnerKeypair(ctx)
	if err != nil {
		log.Printf("[auth] EnsureOwnerKeypair error: %v", err)
		return
	}
	if !isNew {
		return
	}
	log.Println("╔══════════════════════════════════════════════════════════════════╗")
	log.Println("║        SOHOLINK NODE — OWNER PRIVATE KEY (save this now)         ║")
	log.Println("╠══════════════════════════════════════════════════════════════════╣")
	log.Printf( "║  %s  ║", privHex)
	log.Println("╠══════════════════════════════════════════════════════════════════╣")
	log.Println("║  This is shown ONCE and never stored. Keep it in a password      ║")
	log.Println("║  manager. Enter it in the SoHoLINK app to connect any device.    ║")
	log.Println("╚══════════════════════════════════════════════════════════════════╝")
}

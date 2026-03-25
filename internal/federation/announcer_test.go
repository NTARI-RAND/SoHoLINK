package federation

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

func TestNew_DefaultInterval(t *testing.T) {
	a := New(Config{
		CoordinatorURL: "http://localhost:9000",
		NodeDID:        "did:soho:node1",
	})
	if a.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", a.interval)
	}
}

func TestNew_CustomInterval(t *testing.T) {
	a := New(Config{
		CoordinatorURL:    "http://localhost:9000",
		NodeDID:           "did:soho:node1",
		HeartbeatInterval: 10 * time.Second,
	})
	if a.interval != 10*time.Second {
		t.Errorf("interval = %v, want 10s", a.interval)
	}
}

func TestNew_ZeroIntervalUsesDefault(t *testing.T) {
	a := New(Config{
		CoordinatorURL:    "http://localhost:9000",
		HeartbeatInterval: 0,
	})
	if a.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s default", a.interval)
	}
}

func TestNew_NegativeIntervalUsesDefault(t *testing.T) {
	a := New(Config{
		CoordinatorURL:    "http://localhost:9000",
		HeartbeatInterval: -5 * time.Second,
	})
	if a.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s default", a.interval)
	}
}

func TestNew_NilResourcesFnReturnsZeros(t *testing.T) {
	a := New(Config{
		CoordinatorURL: "http://localhost:9000",
		ResourcesFn:    nil,
	})
	res := a.resourcesFn()
	if res.TotalCPU != 0 || res.TotalMemMB != 0 || res.TotalDiskGB != 0 {
		t.Errorf("nil ResourcesFn should return zero NodeResources, got %+v", res)
	}
}

func TestNew_CustomResourcesFn(t *testing.T) {
	custom := func() NodeResources {
		return NodeResources{
			TotalCPU:    8.0,
			TotalMemMB:  32768,
			TotalDiskGB: 500,
			GPUModel:    "RTX 4090",
		}
	}
	a := New(Config{
		CoordinatorURL: "http://localhost:9000",
		ResourcesFn:    custom,
	})
	res := a.resourcesFn()
	if res.TotalCPU != 8.0 {
		t.Errorf("TotalCPU = %f, want 8.0", res.TotalCPU)
	}
	if res.GPUModel != "RTX 4090" {
		t.Errorf("GPUModel = %q, want RTX 4090", res.GPUModel)
	}
}

func TestNew_FieldMapping(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		check  func(*Announcer) bool
		errMsg string
	}{
		{
			name: "coordinator_url",
			cfg:  Config{CoordinatorURL: "https://coord.example.com"},
			check: func(a *Announcer) bool {
				return a.coordinatorURL == "https://coord.example.com"
			},
			errMsg: "coordinatorURL mismatch",
		},
		{
			name: "node_did",
			cfg:  Config{NodeDID: "did:soho:abc123"},
			check: func(a *Announcer) bool {
				return a.nodeDID == "did:soho:abc123"
			},
			errMsg: "nodeDID mismatch",
		},
		{
			name: "address",
			cfg:  Config{Address: "192.168.1.1:8080"},
			check: func(a *Announcer) bool {
				return a.address == "192.168.1.1:8080"
			},
			errMsg: "address mismatch",
		},
		{
			name: "region",
			cfg:  Config{Region: "us-east-1"},
			check: func(a *Announcer) bool {
				return a.region == "us-east-1"
			},
			errMsg: "region mismatch",
		},
		{
			name: "price",
			cfg:  Config{PricePerCPUHourSats: 500},
			check: func(a *Announcer) bool {
				return a.pricePerCPUHourSats == 500
			},
			errMsg: "pricePerCPUHourSats mismatch",
		},
		{
			name: "priv_seed_hex",
			cfg:  Config{PrivSeedHex: "aabbccdd"},
			check: func(a *Announcer) bool {
				return a.privSeedHex == "aabbccdd"
			},
			errMsg: "privSeedHex mismatch",
		},
		{
			name: "pub_key_b64",
			cfg:  Config{PubKeyB64: "dGVzdA=="},
			check: func(a *Announcer) bool {
				return a.pubKeyB64 == "dGVzdA=="
			},
			errMsg: "pubKeyB64 mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New(tt.cfg)
			if !tt.check(a) {
				t.Error(tt.errMsg)
			}
		})
	}
}

func TestNew_ClientNotNil(t *testing.T) {
	a := New(Config{CoordinatorURL: "http://localhost:9000"})
	if a.client == nil {
		t.Error("client should not be nil")
	}
}

func TestNew_ClientTimeout(t *testing.T) {
	a := New(Config{CoordinatorURL: "http://localhost:9000"})
	if a.client.Timeout != 10*time.Second {
		t.Errorf("client timeout = %v, want 10s", a.client.Timeout)
	}
}

// ---------------------------------------------------------------------------
// sign() tests
// ---------------------------------------------------------------------------

func TestSign_EmptyKeyReturnsEmpty(t *testing.T) {
	a := New(Config{PrivSeedHex: ""})
	sig, err := a.sign("test message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig != "" {
		t.Errorf("expected empty signature for empty key, got %q", sig)
	}
}

func TestSign_ValidKeyProducesSignature(t *testing.T) {
	// Generate a real Ed25519 key pair
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	seedHex := hex.EncodeToString(priv.Seed())
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	a := New(Config{
		CoordinatorURL: "http://localhost:9000",
		PrivSeedHex:    seedHex,
		PubKeyB64:      pubB64,
	})

	msg := "did:soho:node1:192.168.1.1:8080:2024-01-01T00:00:00Z:abc123"
	sig, err := a.sign(msg)
	if err != nil {
		t.Fatalf("sign error: %v", err)
	}
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}

	// Verify the signature
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(pub, []byte(msg), sigBytes) {
		t.Error("signature verification failed")
	}
}

func TestSign_InvalidHexReturnsError(t *testing.T) {
	a := New(Config{PrivSeedHex: "not-valid-hex"})
	_, err := a.sign("test")
	if err == nil {
		t.Error("expected error for invalid hex key")
	}
}

// ---------------------------------------------------------------------------
// generateNonce() tests
// ---------------------------------------------------------------------------

func TestGenerateNonce_Length(t *testing.T) {
	nonce, err := generateNonce()
	if err != nil {
		t.Fatalf("generateNonce error: %v", err)
	}
	// 16 bytes -> 32 hex chars
	if len(nonce) != 32 {
		t.Errorf("nonce length = %d, want 32", len(nonce))
	}
}

func TestGenerateNonce_ValidHex(t *testing.T) {
	nonce, err := generateNonce()
	if err != nil {
		t.Fatalf("generateNonce error: %v", err)
	}
	_, err = hex.DecodeString(nonce)
	if err != nil {
		t.Errorf("nonce is not valid hex: %v", err)
	}
}

func TestGenerateNonce_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		nonce, err := generateNonce()
		if err != nil {
			t.Fatalf("generateNonce error: %v", err)
		}
		if seen[nonce] {
			t.Fatalf("duplicate nonce on iteration %d: %s", i, nonce)
		}
		seen[nonce] = true
	}
}

// ---------------------------------------------------------------------------
// NodeResources struct tests
// ---------------------------------------------------------------------------

func TestNodeResources_ZeroValue(t *testing.T) {
	var r NodeResources
	if r.TotalCPU != 0 || r.AvailableCPU != 0 {
		t.Error("zero NodeResources should have 0 CPU")
	}
	if r.GPUModel != "" {
		t.Errorf("GPUModel = %q, want empty", r.GPUModel)
	}
}

func TestNodeResources_FieldAssignment(t *testing.T) {
	r := NodeResources{
		TotalCPU:     16.0,
		AvailableCPU: 8.5,
		TotalMemMB:   65536,
		AvailMemMB:   32000,
		TotalDiskGB:  2000,
		AvailDiskGB:  1500,
		GPUModel:     "A100",
	}
	if r.TotalCPU != 16.0 {
		t.Errorf("TotalCPU = %f, want 16.0", r.TotalCPU)
	}
	if r.AvailMemMB != 32000 {
		t.Errorf("AvailMemMB = %d, want 32000", r.AvailMemMB)
	}
}

// ---------------------------------------------------------------------------
// AnnounceRequest / HeartbeatRequest struct tests
// ---------------------------------------------------------------------------

func TestAnnounceRequest_Fields(t *testing.T) {
	req := AnnounceRequest{
		NodeDID:             "did:soho:n1",
		PublicKey:           "pubkey_b64",
		Address:             "10.0.0.1:8080",
		Region:              "eu-west-1",
		PricePerCPUHourSats: 100,
		Timestamp:           time.Now().UTC().Format(time.RFC3339),
		Nonce:               "abcdef1234567890abcdef1234567890",
		Signature:           "sig_b64",
	}
	if req.NodeDID != "did:soho:n1" {
		t.Errorf("NodeDID = %q, want did:soho:n1", req.NodeDID)
	}
	if req.Region != "eu-west-1" {
		t.Errorf("Region = %q, want eu-west-1", req.Region)
	}
}

func TestHeartbeatRequest_Fields(t *testing.T) {
	req := HeartbeatRequest{
		NodeDID:   "did:soho:n2",
		Timestamp: "2024-01-01T00:00:00Z",
		Nonce:     "1234567890abcdef1234567890abcdef",
		Signature: "sig",
	}
	if req.NodeDID != "did:soho:n2" {
		t.Errorf("NodeDID = %q, want did:soho:n2", req.NodeDID)
	}
}

package did

import (
	"crypto/ed25519"
	"testing"
)

func TestEncodeDIDKeyRoundtrip(t *testing.T) {
	pub, _, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair failed: %v", err)
	}

	did := EncodeDIDKey(pub)

	// Verify format
	if did[:8] != "did:key:" {
		t.Errorf("DID should start with 'did:key:', got: %s", did[:8])
	}
	if did[8] != 'z' {
		t.Errorf("DID should have 'z' multibase prefix, got: %c", did[8])
	}

	// Decode back
	decoded, err := DecodeDIDKey(did)
	if err != nil {
		t.Fatalf("DecodeDIDKey failed: %v", err)
	}

	if !pub.Equal(decoded) {
		t.Errorf("roundtrip failed: keys don't match")
	}
}

func TestDecodeDIDKeyInvalid(t *testing.T) {
	tests := []struct {
		name string
		did  string
	}{
		{"empty", ""},
		{"wrong prefix", "did:web:example.com"},
		{"no multibase", "did:key:abc"},
		{"too short", "did:key:z1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeDIDKey(tt.did)
			if err == nil {
				t.Errorf("expected error for DID: %s", tt.did)
			}
		})
	}
}

func TestEncodeDIDKeyDeterministic(t *testing.T) {
	pub, _, _ := GenerateKeypair()

	did1 := EncodeDIDKey(pub)
	did2 := EncodeDIDKey(pub)

	if did1 != did2 {
		t.Errorf("encoding should be deterministic: %s != %s", did1, did2)
	}
}

func TestKeyPairSaveLoad(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair failed: %v", err)
	}

	// Save to temp file
	tmpFile := t.TempDir() + "/test_key.pem"
	if err := SavePrivateKey(tmpFile, priv); err != nil {
		t.Fatalf("SavePrivateKey failed: %v", err)
	}

	// Load back
	loadedPriv, err := LoadPrivateKey(tmpFile)
	if err != nil {
		t.Fatalf("LoadPrivateKey failed: %v", err)
	}

	if !priv.Equal(loadedPriv) {
		t.Error("loaded private key doesn't match original")
	}

	// Verify public key matches
	loadedPub := loadedPriv.Public().(ed25519.PublicKey)
	if !pub.Equal(loadedPub) {
		t.Error("loaded public key doesn't match original")
	}
}

func TestSignatureWithDIDKey(t *testing.T) {
	pub, priv, _ := GenerateKeypair()

	// Sign a message
	message := []byte("test message")
	sig := ed25519.Sign(priv, message)

	// Encode to DID and decode back
	did := EncodeDIDKey(pub)
	decoded, err := DecodeDIDKey(did)
	if err != nil {
		t.Fatalf("DecodeDIDKey failed: %v", err)
	}

	// Verify signature with decoded key
	if !ed25519.Verify(decoded, message, sig) {
		t.Error("signature verification failed with decoded DID key")
	}
}

package did

import (
	"crypto/ed25519"
	"fmt"
	"strings"

	"github.com/mr-tron/base58"
)

// Multicodec prefix for Ed25519 public key
// See: https://github.com/multiformats/multicodec/blob/master/table.csv
var ed25519MulticodecPrefix = []byte{0xed, 0x01}

// EncodeDIDKey converts an Ed25519 public key to a DID:key string.
// Format: did:key:z<base58btc(multicodec_prefix + public_key_bytes)>
func EncodeDIDKey(pub ed25519.PublicKey) string {
	// Prepend multicodec prefix
	prefixed := make([]byte, len(ed25519MulticodecPrefix)+len(pub))
	copy(prefixed, ed25519MulticodecPrefix)
	copy(prefixed[len(ed25519MulticodecPrefix):], pub)

	// Base58-btc encode
	encoded := base58.Encode(prefixed)

	return "did:key:z" + encoded
}

// DecodeDIDKey parses a DID:key string and returns the Ed25519 public key.
func DecodeDIDKey(did string) (ed25519.PublicKey, error) {
	// Validate prefix
	if !strings.HasPrefix(did, "did:key:z") {
		return nil, fmt.Errorf("invalid DID:key format: must start with 'did:key:z'")
	}

	// Strip prefix to get base58-encoded portion
	encoded := did[len("did:key:z"):]

	// Base58-btc decode
	decoded, err := base58.Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base58: %w", err)
	}

	// Verify and strip multicodec prefix
	if len(decoded) < len(ed25519MulticodecPrefix) {
		return nil, fmt.Errorf("decoded key too short")
	}
	for i, b := range ed25519MulticodecPrefix {
		if decoded[i] != b {
			return nil, fmt.Errorf("invalid multicodec prefix: expected Ed25519 (0xed01)")
		}
	}

	pubBytes := decoded[len(ed25519MulticodecPrefix):]

	// Ed25519 public keys are 32 bytes
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length: got %d, expected %d", len(pubBytes), ed25519.PublicKeySize)
	}

	return ed25519.PublicKey(pubBytes), nil
}

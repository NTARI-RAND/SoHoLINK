package agent

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AllowlistPublicKey is the base64-encoded Ed25519 public key used to verify
// allowlist signatures. It is injected at build time via ldflags:
//
//	go build -ldflags "-X github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent.AllowlistPublicKey=<base64key>" ...
//
// An empty value causes Verify() to return ErrAllowlistNoKey. Production
// builds MUST set this.
var AllowlistPublicKey string

// WorkloadType represents a class of job the agent can execute.
type WorkloadType string

const (
	WorkloadCompute          WorkloadType = "compute"
	WorkloadStorage          WorkloadType = "storage"
	WorkloadPrintTraditional WorkloadType = "print_traditional"
	WorkloadPrint3D          WorkloadType = "print_3d"
)

// EgressTier controls outbound network access for a containerized workload.
type EgressTier string

const (
	EgressNone     EgressTier = "none"
	EgressOutbound EgressTier = "outbound"
)

// DeviceAccess names a controlled exception to default container hardening,
// granting access to a specific host device or socket. Each exception is
// honored only when present in the matched allowlist entry.
type DeviceAccess string

const (
	DeviceCUPSSocket DeviceAccess = "cups_socket"
	DeviceUSBPrinter DeviceAccess = "usb_printer"
)

// AllowlistEntry describes a single approved container image and its policy.
type AllowlistEntry struct {
	Name                string         `json:"name"`
	Digest              string         `json:"digest"`
	Type                WorkloadType   `json:"type"`
	Egress              EgressTier     `json:"egress"`
	AllowedDestinations []string       `json:"allowed_destinations,omitempty"`
	DeviceAccess        []DeviceAccess `json:"device_access,omitempty"`
}

// Allowlist is the signed document the control plane publishes.
type Allowlist struct {
	Version   int              `json:"version"`
	IssuedAt  time.Time        `json:"issued_at"`
	Entries   []AllowlistEntry `json:"entries"`
	Signature string           `json:"signature"`
}

// Errors returned by allowlist operations.
var (
	ErrImageNotAllowed    = errors.New("image not in allowlist")
	ErrAllowlistSignature = errors.New("allowlist signature verification failed")
	ErrAllowlistNoKey     = errors.New("allowlist public key not configured")
	ErrAllowlistFetch     = errors.New("allowlist fetch failed")
	ErrAllowlistMalformed = errors.New("allowlist malformed")
)

// canonicalSigningBytes returns the deterministic JSON representation used
// for signature generation and verification. Entries are sorted by digest
// to ensure canonicalization is stable regardless of source ordering.
func (a *Allowlist) canonicalSigningBytes() ([]byte, error) {
	entries := make([]AllowlistEntry, len(a.Entries))
	copy(entries, a.Entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Digest < entries[j].Digest
	})
	payload := struct {
		Version  int              `json:"version"`
		IssuedAt time.Time        `json:"issued_at"`
		Entries  []AllowlistEntry `json:"entries"`
	}{a.Version, a.IssuedAt, entries}
	return json.Marshal(payload)
}

// Sign computes a signature over the allowlist's canonical bytes using priv
// and stores the base64-encoded signature in a.Signature. Any existing
// signature is overwritten. Reuses canonicalSigningBytes so Sign and Verify
// cannot diverge.
func (a *Allowlist) Sign(priv ed25519.PrivateKey) error {
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("sign: invalid private key length: got %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	msg, err := a.canonicalSigningBytes()
	if err != nil {
		return fmt.Errorf("sign: canonicalize: %w", err)
	}
	sig := ed25519.Sign(priv, msg)
	a.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// Verify checks that the allowlist's signature matches the configured public
// key. Returns nil on success, ErrAllowlistNoKey if no key is configured,
// or ErrAllowlistSignature on any verification failure.
func (a *Allowlist) Verify() error {
	if AllowlistPublicKey == "" {
		return ErrAllowlistNoKey
	}
	pubBytes, err := base64.StdEncoding.DecodeString(AllowlistPublicKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: invalid public key", ErrAllowlistSignature)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("%w: invalid signature encoding", ErrAllowlistSignature)
	}
	msg, err := a.canonicalSigningBytes()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAllowlistSignature, err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), msg, sigBytes) {
		return ErrAllowlistSignature
	}
	return nil
}

// Lookup returns the allowlist entry matching the given image reference.
// The reference must be digest-pinned (e.g. "soholink/worker@sha256:...").
// Tag-only references are always rejected.
func (a *Allowlist) Lookup(image string) (*AllowlistEntry, error) {
	digest, ok := extractDigest(image)
	if !ok {
		return nil, fmt.Errorf("%w: image must be digest-pinned", ErrImageNotAllowed)
	}
	for i := range a.Entries {
		if a.Entries[i].Digest == digest {
			return &a.Entries[i], nil
		}
	}
	return nil, ErrImageNotAllowed
}

// extractDigest pulls the sha256:... portion from a digest-pinned image
// reference. Returns false if the reference is not digest-pinned with sha256.
func extractDigest(image string) (string, bool) {
	idx := strings.LastIndex(image, "@")
	if idx < 0 || idx == len(image)-1 {
		return "", false
	}
	d := image[idx+1:]
	if !strings.HasPrefix(d, "sha256:") || len(d) <= len("sha256:") {
		return "", false
	}
	return d, true
}

// AllowlistCachePath returns the on-disk cache location for the allowlist.
// It lives next to agent.conf so it shares the same protected directory.
// Exposed as a variable so tests can override it.
var AllowlistCachePath = func() string {
	return filepath.Join(filepath.Dir(DefaultConfigPath()), "allowlist.json")
}

// LoadAllowlistFromURL fetches the allowlist from the given URL, verifies
// its signature, and caches it locally. If the fetch fails, it falls back
// to the cached copy. The returned allowlist is always signature-verified;
// no unverified allowlist is ever returned to the caller.
func LoadAllowlistFromURL(url string, httpClient *http.Client) (*Allowlist, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	cachePath := AllowlistCachePath()

	al, fetchErr := fetchAllowlist(url, httpClient)
	if fetchErr == nil {
		if verr := al.Verify(); verr != nil {
			return nil, verr
		}
		// Persist to cache; cache write failure is non-fatal.
		if data, merr := json.MarshalIndent(al, "", "  "); merr == nil {
			_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
			_ = os.WriteFile(cachePath, data, 0o644)
		}
		return al, nil
	}

	// Fallback to cache.
	data, rerr := os.ReadFile(cachePath)
	if rerr != nil {
		return nil, fmt.Errorf("%w: remote=%v cache=%v", ErrAllowlistFetch, fetchErr, rerr)
	}
	cached := &Allowlist{}
	if err := json.Unmarshal(data, cached); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAllowlistMalformed, err)
	}
	if err := cached.Verify(); err != nil {
		return nil, err
	}
	return cached, nil
}

func fetchAllowlist(url string, client *http.Client) (*Allowlist, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrAllowlistFetch, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, err
	}
	al := &Allowlist{}
	if err := json.Unmarshal(body, al); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAllowlistMalformed, err)
	}
	return al, nil
}

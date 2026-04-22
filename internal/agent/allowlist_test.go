package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTestKey installs a fresh Ed25519 keypair as the package-level
// AllowlistPublicKey for the duration of the test, returning the matching
// private key for signing fixtures. The original value is restored on cleanup.
func withTestKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	original := AllowlistPublicKey
	AllowlistPublicKey = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { AllowlistPublicKey = original })
	return priv
}

// signAllowlist produces a valid signature for the given allowlist using
// the test private key. The signature is written to a.Signature.
func signAllowlist(t *testing.T, a *Allowlist, priv ed25519.PrivateKey) {
	t.Helper()
	msg, err := a.canonicalSigningBytes()
	if err != nil {
		t.Fatalf("canonical bytes: %v", err)
	}
	sig := ed25519.Sign(priv, msg)
	a.Signature = base64.StdEncoding.EncodeToString(sig)
}

// withTestCachePath redirects AllowlistCachePath to a file in a temp dir
// for the duration of the test. Returns the redirected path.
func withTestCachePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.json")
	original := AllowlistCachePath
	AllowlistCachePath = func() string { return path }
	t.Cleanup(func() { AllowlistCachePath = original })
	return path
}

func sampleAllowlist() *Allowlist {
	return &Allowlist{
		Version:  1,
		IssuedAt: time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
		Entries: []AllowlistEntry{
			{
				Name:   "soholink/compute-worker",
				Digest: "sha256:" + strings.Repeat("a", 64),
				Type:   WorkloadCompute,
				Egress: EgressNone,
			},
			{
				Name:                "soholink/storage-worker",
				Digest:              "sha256:" + strings.Repeat("b", 64),
				Type:                WorkloadStorage,
				Egress:              EgressOutbound,
				AllowedDestinations: []string{"sync.soholink.com:22000"},
			},
		},
	}
}

func TestVerify_NoKeyConfigured(t *testing.T) {
	original := AllowlistPublicKey
	AllowlistPublicKey = ""
	t.Cleanup(func() { AllowlistPublicKey = original })

	al := sampleAllowlist()
	al.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if err := al.Verify(); !errors.Is(err, ErrAllowlistNoKey) {
		t.Fatalf("expected ErrAllowlistNoKey, got %v", err)
	}
}

func TestVerify_ValidSignature(t *testing.T) {
	priv := withTestKey(t)
	al := sampleAllowlist()
	signAllowlist(t, al, priv)
	if err := al.Verify(); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestVerify_TamperedEntries(t *testing.T) {
	priv := withTestKey(t)
	al := sampleAllowlist()
	signAllowlist(t, al, priv)
	// Mutate after signing — verification must fail.
	al.Entries[0].Digest = "sha256:" + strings.Repeat("c", 64)
	if err := al.Verify(); !errors.Is(err, ErrAllowlistSignature) {
		t.Fatalf("expected ErrAllowlistSignature, got %v", err)
	}
}

func TestVerify_InvalidSignatureEncoding(t *testing.T) {
	withTestKey(t)
	al := sampleAllowlist()
	al.Signature = "not-base64!!!"
	if err := al.Verify(); !errors.Is(err, ErrAllowlistSignature) {
		t.Fatalf("expected ErrAllowlistSignature, got %v", err)
	}
}

func TestVerify_OrderIndependent(t *testing.T) {
	priv := withTestKey(t)
	al := sampleAllowlist()
	signAllowlist(t, al, priv)
	// Reverse the entry order — signature should still verify because
	// canonicalSigningBytes sorts by digest.
	al.Entries[0], al.Entries[1] = al.Entries[1], al.Entries[0]
	if err := al.Verify(); err != nil {
		t.Fatalf("expected order-independent verification, got %v", err)
	}
}

func TestLookup_DigestMatch(t *testing.T) {
	al := sampleAllowlist()
	digest := "sha256:" + strings.Repeat("a", 64)
	entry, err := al.Lookup("soholink/compute-worker@" + digest)
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if entry.Type != WorkloadCompute {
		t.Fatalf("expected compute, got %v", entry.Type)
	}
}

func TestLookup_RejectsTagOnly(t *testing.T) {
	al := sampleAllowlist()
	_, err := al.Lookup("soholink/compute-worker:latest")
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Fatalf("expected ErrImageNotAllowed, got %v", err)
	}
}

func TestLookup_RejectsUnknownDigest(t *testing.T) {
	al := sampleAllowlist()
	unknown := "sha256:" + strings.Repeat("f", 64)
	_, err := al.Lookup("soholink/compute-worker@" + unknown)
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Fatalf("expected ErrImageNotAllowed, got %v", err)
	}
}

func TestLookup_RejectsEmptyDigest(t *testing.T) {
	al := sampleAllowlist()
	_, err := al.Lookup("soholink/compute-worker@")
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Fatalf("expected ErrImageNotAllowed, got %v", err)
	}
}

func TestLookup_RejectsNonSHA256Digest(t *testing.T) {
	al := sampleAllowlist()
	_, err := al.Lookup("soholink/compute-worker@md5:abc")
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Fatalf("expected ErrImageNotAllowed, got %v", err)
	}
}

func TestLoadAllowlistFromURL_FetchesAndCaches(t *testing.T) {
	priv := withTestKey(t)
	cachePath := withTestCachePath(t)

	al := sampleAllowlist()
	signAllowlist(t, al, priv)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(al)
	}))
	t.Cleanup(srv.Close)

	loaded, err := LoadAllowlistFromURL(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded.Entries))
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache file at %s, got %v", cachePath, err)
	}
}

func TestLoadAllowlistFromURL_FallsBackToCache(t *testing.T) {
	priv := withTestKey(t)
	cachePath := withTestCachePath(t)

	// Pre-seed the cache with a valid signed allowlist.
	al := sampleAllowlist()
	signAllowlist(t, al, priv)
	data, err := json.Marshal(al)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// Server that always 500s — forces fallback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	loaded, err := LoadAllowlistFromURL(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("expected 2 entries from cache, got %d", len(loaded.Entries))
	}
}

func TestLoadAllowlistFromURL_FailsWhenRemoteAndCacheBothMissing(t *testing.T) {
	withTestKey(t)
	withTestCachePath(t) // cache path exists but file does not

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := LoadAllowlistFromURL(srv.URL, srv.Client())
	if !errors.Is(err, ErrAllowlistFetch) {
		t.Fatalf("expected ErrAllowlistFetch, got %v", err)
	}
}

func TestLoadAllowlistFromURL_RejectsTamperedCache(t *testing.T) {
	priv := withTestKey(t)
	cachePath := withTestCachePath(t)

	// Seed cache with a valid signed allowlist, then tamper.
	al := sampleAllowlist()
	signAllowlist(t, al, priv)
	al.Entries[0].Digest = "sha256:" + strings.Repeat("9", 64) // mutate after sign
	data, err := json.Marshal(al)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// Force fallback by 500-ing the server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err = LoadAllowlistFromURL(srv.URL, srv.Client())
	if !errors.Is(err, ErrAllowlistSignature) {
		t.Fatalf("expected ErrAllowlistSignature on tampered cache, got %v", err)
	}
}

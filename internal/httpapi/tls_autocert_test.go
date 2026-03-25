package httpapi

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTLSCert_ExistingCertsUnchanged(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	// Create dummy files
	os.WriteFile(certFile, []byte("cert"), 0644)
	os.WriteFile(keyFile, []byte("key"), 0600)

	gotCert, gotKey, generated, err := EnsureTLSCert(certFile, keyFile, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generated {
		t.Error("should not generate when both files exist")
	}
	if gotCert != certFile || gotKey != keyFile {
		t.Errorf("paths changed: got (%s, %s), want (%s, %s)", gotCert, gotKey, certFile, keyFile)
	}
}

func TestEnsureTLSCert_AutoGeneratesWhenEmpty(t *testing.T) {
	dir := t.TempDir()

	certPath, keyPath, generated, err := EnsureTLSCert("", "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !generated {
		t.Error("should generate when both paths are empty")
	}
	if certPath == "" || keyPath == "" {
		t.Error("generated paths should not be empty")
	}

	// Verify files exist
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert file not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("key file not created: %v", err)
	}
}

func TestEnsureTLSCert_GeneratedCertIsValid(t *testing.T) {
	dir := t.TempDir()

	certPath, keyPath, _, err := EnsureTLSCert("", "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the cert/key pair can be loaded by Go's TLS library
	_, err = tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("generated cert/key pair is invalid: %v", err)
	}
}

func TestEnsureTLSCert_ReusesExistingGenerated(t *testing.T) {
	dir := t.TempDir()

	// First call generates
	cert1, key1, gen1, err := EnsureTLSCert("", "", dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !gen1 {
		t.Error("first call should generate")
	}

	// Read the generated cert content
	content1, _ := os.ReadFile(cert1)

	// Second call reuses
	cert2, key2, gen2, err := EnsureTLSCert("", "", dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !gen2 {
		t.Error("second call should report generated=true (files exist)")
	}
	if cert1 != cert2 || key1 != key2 {
		t.Error("second call returned different paths")
	}

	content2, _ := os.ReadFile(cert2)
	if string(content1) != string(content2) {
		t.Error("cert content changed between calls — should reuse")
	}
}

func TestEnsureTLSCert_KeyFileExists(t *testing.T) {
	dir := t.TempDir()

	_, keyPath, _, err := EnsureTLSCert("", "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}

	// Key file should exist and be non-empty
	if info.Size() == 0 {
		t.Error("key file is empty")
	}
}

func TestGenerateSelfSignedCert_ReturnsValidPEM(t *testing.T) {
	certPEM, keyPEM, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(certPEM) == 0 {
		t.Error("cert PEM is empty")
	}
	if len(keyPEM) == 0 {
		t.Error("key PEM is empty")
	}

	// Verify PEM can be loaded
	_, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("PEM pair is invalid: %v", err)
	}
}

func TestGenerateSelfSignedCert_IncludesLocalhost(t *testing.T) {
	certPEM, keyPEM, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load pair: %v", err)
	}

	// Parse the leaf certificate to check SAN entries
	if cert.Leaf == nil {
		// X509KeyPair may not populate Leaf; parse manually
		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr != nil {
			t.Fatalf("parse leaf: %v", parseErr)
		}
		cert.Leaf = leaf
	}

	// Verify localhost is in DNS SANs
	foundLocalhost := false
	for _, dns := range cert.Leaf.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
			break
		}
	}
	if !foundLocalhost {
		t.Errorf("cert DNSNames %v does not include localhost", cert.Leaf.DNSNames)
	}

	// Verify 127.0.0.1 is in IP SANs
	found127 := false
	for _, ip := range cert.Leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			found127 = true
			break
		}
	}
	if !found127 {
		t.Errorf("cert IPAddresses %v does not include 127.0.0.1", cert.Leaf.IPAddresses)
	}
}

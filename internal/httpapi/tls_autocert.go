package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureTLSCert checks whether TLS cert and key files exist at the given paths.
// If both are empty, it generates a self-signed certificate for local development
// and returns the paths to the generated files.
//
// For production, operators should provide real certificates (Let's Encrypt, etc.).
// The self-signed cert is valid for 1 year and covers localhost + LAN IPs.
//
// Returns (certPath, keyPath, wasGenerated, error).
func EnsureTLSCert(certFile, keyFile, dataDir string) (string, string, bool, error) {
	// If both paths are provided and files exist, use them as-is.
	if certFile != "" && keyFile != "" {
		if _, err := os.Stat(certFile); err == nil {
			if _, err := os.Stat(keyFile); err == nil {
				return certFile, keyFile, false, nil
			}
		}
		// One or both files missing — don't auto-generate, let the user fix it.
		return certFile, keyFile, false, nil
	}

	// Neither cert nor key provided — generate self-signed for development.
	tlsDir := filepath.Join(dataDir, "tls")
	generatedCert := filepath.Join(tlsDir, "server.crt")
	generatedKey := filepath.Join(tlsDir, "server.key")

	// Check if we already generated one previously.
	if _, err := os.Stat(generatedCert); err == nil {
		if _, err := os.Stat(generatedKey); err == nil {
			return generatedCert, generatedKey, true, nil
		}
	}

	// Generate new self-signed certificate.
	if err := os.MkdirAll(tlsDir, 0700); err != nil {
		return "", "", false, fmt.Errorf("create TLS directory: %w", err)
	}

	certPEM, keyPEM, err := generateSelfSignedCert()
	if err != nil {
		return "", "", false, fmt.Errorf("generate self-signed cert: %w", err)
	}

	if err := os.WriteFile(generatedCert, certPEM, 0644); err != nil {
		return "", "", false, fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(generatedKey, keyPEM, 0600); err != nil {
		return "", "", false, fmt.Errorf("write key: %w", err)
	}

	return generatedCert, generatedKey, true, nil
}

// generateSelfSignedCert creates a self-signed TLS certificate valid for
// localhost, 127.0.0.1, and all detected LAN IP addresses.
func generateSelfSignedCert() (certPEM, keyPEM []byte, err error) {
	// Generate ECDSA P-256 key (fast, small, widely supported)
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"SoHoLINK Node (self-signed)"},
			CommonName:   "localhost",
		},
		NotBefore: now.Add(-1 * time.Hour), // 1 hour grace for clock skew
		NotAfter:  now.Add(365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		DNSNames:    []string{"localhost", "*.local"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	// Add all LAN IP addresses so the cert works for inter-machine federation.
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				template.IPAddresses = append(template.IPAddresses, ipNet.IP)
			}
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

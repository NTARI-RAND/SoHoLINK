package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// GenerateSelfSignedCert creates an X.509 certificate with CN = did signed by
// privKey (Ed25519). The certificate is valid for certTTL (default 1 year).
//
// The resulting tls.Certificate can be used as the identity credential in
// NewMTLSConfig — the CN field carries the node's DID so that peers can
// verify the identity of the connection against their trusted-DID list.
func GenerateSelfSignedCert(did string, privKey ed25519.PrivateKey, certTTL time.Duration) (tls.Certificate, error) {
	if certTTL <= 0 {
		certTTL = 365 * 24 * time.Hour
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("mtls: generate serial: %w", err)
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   did,
			Organization: []string{"SoHoLINK"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(certTTL),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	pubKey := privKey.Public().(ed25519.PublicKey)
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pubKey, privKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("mtls: create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("mtls: marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("mtls: load key pair: %w", err)
	}
	return cert, nil
}

// NewMTLSConfig returns a *tls.Config suitable for both server and client use
// with mutual TLS. The local node presents cert as its identity. Peers must
// present a certificate whose CN is in trustedDIDs.
//
// Minimum TLS version is 1.3 — TLS 1.3 is mandatory for all SoHoLINK mesh
// connections; older versions are refused.
func NewMTLSConfig(cert tls.Certificate, trustedDIDs []string) *tls.Config {
	trusted := make(map[string]struct{}, len(trustedDIDs))
	for _, did := range trustedDIDs {
		trusted[strings.ToLower(did)] = struct{}{}
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert, // verify in VerifyPeerCertificate
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("mtls: peer presented no certificate")
			}
			peerCert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("mtls: parse peer certificate: %w", err)
			}
			cn := strings.ToLower(peerCert.Subject.CommonName)
			if len(trusted) > 0 {
				if _, ok := trusted[cn]; !ok {
					return fmt.Errorf("mtls: peer DID %q not in trusted list", cn)
				}
			}
			return nil
		},
		// InsecureSkipVerify is false by default — we verify manually above.
	}
}

// NewMTLSClientConfig returns a *tls.Config for outbound mTLS connections.
// Behaves the same as NewMTLSConfig but sets ServerName to serverDID for
// SNI and skips standard hostname verification (we verify CN = DID instead).
func NewMTLSClientConfig(cert tls.Certificate, trustedDIDs []string, serverDID string) *tls.Config {
	cfg := NewMTLSConfig(cert, trustedDIDs)
	cfg.ServerName = serverDID
	cfg.InsecureSkipVerify = true // hostname verification replaced by DID CN check above
	return cfg
}

// PeerDIDFromConn extracts the CN (DID) from the peer certificate of an
// established TLS connection. Returns an error if no peer cert is available.
func PeerDIDFromConn(tlsState tls.ConnectionState) (string, error) {
	if len(tlsState.PeerCertificates) == 0 {
		return "", fmt.Errorf("mtls: no peer certificate in TLS connection")
	}
	return tlsState.PeerCertificates[0].Subject.CommonName, nil
}

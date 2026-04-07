package network

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestGenerateKeypair(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: unexpected error: %v", err)
	}
	if priv == "" || pub == "" {
		t.Fatal("GenerateKeypair: returned empty string(s)")
	}

	privBytes, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		t.Fatalf("private key is not valid base64: %v", err)
	}
	if len(privBytes) != 32 {
		t.Errorf("private key: got %d bytes, want 32", len(privBytes))
	}

	pubBytes, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		t.Fatalf("public key is not valid base64: %v", err)
	}
	if len(pubBytes) != 32 {
		t.Errorf("public key: got %d bytes, want 32", len(pubBytes))
	}
}

func TestGenerateKeypair_UniqueOnEachCall(t *testing.T) {
	priv1, pub1, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	priv2, pub2, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if priv1 == priv2 {
		t.Error("two calls produced identical private keys")
	}
	if pub1 == pub2 {
		t.Error("two calls produced identical public keys")
	}
}

func TestWriteConfig_WithEndpointAndKeepalive(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wg*.conf")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	cfg := GenerateConfig("node-1", "PRIVKEY==", "10.100.0.1/24", 51820, []PeerConfig{
		{
			PublicKey:           "PUBKEY==",
			AllowedIPs:          []string{"10.100.0.2/32", "10.100.0.3/32"},
			Endpoint:            "1.2.3.4:51820",
			PersistentKeepalive: 25,
		},
	})

	if err := WriteConfig(cfg, f.Name()); err != nil {
		t.Fatalf("WriteConfig: unexpected error: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"# SoHoLINK WireGuard config \u2014 node: node-1",
		"[Interface]",
		"PrivateKey = PRIVKEY==",
		"Address = 10.100.0.1/24",
		"ListenPort = 51820",
		"[Peer]",
		"PublicKey = PUBKEY==",
		"AllowedIPs = 10.100.0.2/32, 10.100.0.3/32",
		"Endpoint = 1.2.3.4:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q\ngot:\n%s", want, content)
		}
	}
}

func TestWriteConfig_NoEndpointNoKeepalive(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wg*.conf")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	cfg := GenerateConfig("node-2", "PRIVKEY==", "10.100.0.2/24", 51820, []PeerConfig{
		{
			PublicKey:  "PUBKEY==",
			AllowedIPs: []string{"10.100.0.1/32"},
		},
	})

	if err := WriteConfig(cfg, f.Name()); err != nil {
		t.Fatalf("WriteConfig: unexpected error: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "Endpoint") {
		t.Errorf("config should not contain Endpoint line when endpoint is empty\ngot:\n%s", content)
	}
	if strings.Contains(content, "PersistentKeepalive") {
		t.Errorf("config should not contain PersistentKeepalive line when keepalive is zero\ngot:\n%s", content)
	}
}

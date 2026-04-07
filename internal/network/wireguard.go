package network

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// GenerateKeypair generates a Curve25519 keypair for WireGuard.
// Both keys are returned as base64 standard-encoded strings.
func GenerateKeypair() (privateKey, publicKey string, err error) {
	var privBytes [32]byte
	if _, err = rand.Read(privBytes[:]); err != nil {
		return "", "", fmt.Errorf("generate keypair: read random bytes: %w", err)
	}

	pubBytes, err := curve25519.X25519(privBytes[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("generate keypair: derive public key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(privBytes[:]),
		base64.StdEncoding.EncodeToString(pubBytes),
		nil
}

// PeerConfig describes a WireGuard peer entry.
type PeerConfig struct {
	PublicKey           string
	AllowedIPs          []string
	Endpoint            string // optional — empty means not yet known; line is omitted
	PersistentKeepalive int    // seconds; 0 means omit the line
}

// InterfaceConfig holds the full WireGuard interface configuration for a node.
type InterfaceConfig struct {
	NodeID     string
	PrivateKey string
	Address    string // e.g. 10.100.0.1/24
	ListenPort int
	Peers      []PeerConfig
}

// GenerateConfig constructs an InterfaceConfig from the provided parameters.
func GenerateConfig(nodeID, privateKey, address string, listenPort int, peers []PeerConfig) InterfaceConfig {
	return InterfaceConfig{
		NodeID:     nodeID,
		PrivateKey: privateKey,
		Address:    address,
		ListenPort: listenPort,
		Peers:      peers,
	}
}

// WriteConfig renders cfg as a wg.conf file and writes it to path.
// The file is written with mode 0600 (owner read/write only) because it
// contains the node's WireGuard private key.
func WriteConfig(cfg InterfaceConfig, path string) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# SoHoLINK WireGuard config — node: %s\n", cfg.NodeID)
	fmt.Fprintf(&b, "[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", cfg.PrivateKey)
	fmt.Fprintf(&b, "Address = %s\n", cfg.Address)
	fmt.Fprintf(&b, "ListenPort = %d\n", cfg.ListenPort)

	for _, peer := range cfg.Peers {
		fmt.Fprintf(&b, "\n[Peer]\n")
		fmt.Fprintf(&b, "PublicKey = %s\n", peer.PublicKey)
		fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", "))
		if peer.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", peer.Endpoint)
		}
		if peer.PersistentKeepalive > 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", peer.PersistentKeepalive)
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		return fmt.Errorf("write wireguard config: %w", err)
	}
	return nil
}

// allowlist-genkey generates a fresh Ed25519 keypair for signing SoHoLINK
// allowlist documents. Outputs are base64-encoded.
//
// The private key file is written with mode 0600. The public key is also
// printed to stdout for convenient copy-paste into build configuration.
//
// Usage:
//
//	allowlist-genkey -priv priv.b64 -pub pub.b64
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
)

func main() {
	privPath := flag.String("priv", "", "output path for base64-encoded private key (required, refuses to overwrite)")
	pubPath := flag.String("pub", "", "output path for base64-encoded public key (required, refuses to overwrite)")
	flag.Parse()

	if *privPath == "" || *pubPath == "" {
		fmt.Fprintln(os.Stderr, "error: both -priv and -pub are required")
		flag.Usage()
		os.Exit(2)
	}

	if err := refuseOverwrite(*privPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := refuseOverwrite(*pubPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate key: %v\n", err)
		os.Exit(1)
	}

	privB64 := base64.StdEncoding.EncodeToString(priv)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	if err := os.WriteFile(*privPath, []byte(privB64+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "error: write priv: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*pubPath, []byte(pubB64+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write pub: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(pubB64)
}

func refuseOverwrite(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("refusing to overwrite existing file: %s", path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %v", path, err)
	}
	return nil
}

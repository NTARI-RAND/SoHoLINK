// allowlist-sign reads an unsigned SoHoLINK allowlist JSON document, signs
// it with the provided Ed25519 private key, and emits the signed document.
//
// The private key must be base64-encoded (as produced by allowlist-genkey).
// The signed output is the same JSON shape with the "signature" field
// populated.
//
// Usage:
//
//	allowlist-sign -input al.json -key priv.b64 -output signed.json
//	allowlist-sign -key priv.b64 < al.json > signed.json
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
)

func main() {
	inputPath := flag.String("input", "", "path to unsigned allowlist JSON (default: stdin)")
	keyPath := flag.String("key", "", "path to base64-encoded Ed25519 private key (required)")
	outputPath := flag.String("output", "", "output path for signed allowlist JSON (default: stdout)")
	flag.Parse()

	if *keyPath == "" {
		fmt.Fprintln(os.Stderr, "error: -key is required")
		flag.Usage()
		os.Exit(2)
	}

	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read key: %v\n", err)
		os.Exit(1)
	}
	privB64 := strings.TrimSpace(string(keyBytes))
	privBytes, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: decode key: %v\n", err)
		os.Exit(1)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		fmt.Fprintf(os.Stderr, "error: key length %d, want %d\n", len(privBytes), ed25519.PrivateKeySize)
		os.Exit(1)
	}
	priv := ed25519.PrivateKey(privBytes)

	var inputBytes []byte
	if *inputPath == "" {
		inputBytes, err = io.ReadAll(os.Stdin)
	} else {
		inputBytes, err = os.ReadFile(*inputPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read input: %v\n", err)
		os.Exit(1)
	}

	al := &agent.Allowlist{}
	if err := json.Unmarshal(inputBytes, al); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse input: %v\n", err)
		os.Exit(1)
	}

	if err := al.Sign(priv); err != nil {
		fmt.Fprintf(os.Stderr, "error: sign: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(al, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: marshal output: %v\n", err)
		os.Exit(1)
	}
	out = append(out, '\n')

	if *outputPath == "" {
		if _, err := os.Stdout.Write(out); err != nil {
			fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := os.WriteFile(*outputPath, out, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
			os.Exit(1)
		}
	}
}

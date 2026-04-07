//go:build linux

package tpm

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// linuxAttester uses tpm2-tools binaries to access the TPM 2.0 device.
// Install: apt-get install tpm2-tools (or equivalent).
// The process must run as root or be a member of the 'tss' group for
// access to /dev/tpmrm0 (the resource-manager device node).
type linuxAttester struct {
	available bool
}

// NewAttester returns the best available Attester for Linux.
// It checks for /dev/tpmrm0 (preferred) or /dev/tpm0, and whether
// the tpm2_quote binary is on PATH.
func NewAttester() Attester {
	a := &linuxAttester{}

	// Check for TPM device node
	tpmPresent := false
	for _, dev := range []string{"/dev/tpmrm0", "/dev/tpm0"} {
		if _, err := os.Stat(dev); err == nil {
			tpmPresent = true
			break
		}
	}
	if !tpmPresent {
		return &NoopAttester{}
	}

	// Check for tpm2_quote on PATH
	if _, err := exec.LookPath("tpm2_quote"); err != nil {
		return &NoopAttester{}
	}

	a.available = true
	return a
}

func (a *linuxAttester) Available() bool { return a.available }

// Quote produces a TPM2 PCR quote using tpm2_quote.
// The quote and signature are returned as raw binary blobs.
func (a *linuxAttester) Quote(ctx context.Context, pcrIndices []int, nonce []byte) ([]byte, []byte, error) {
	if !a.available {
		return nil, nil, ErrNotAvailable
	}

	// tpm2_quote writes quote and signature to temp files
	quoteFile, err := os.CreateTemp("", "tpm2-quote-*.bin")
	if err != nil {
		return nil, nil, fmt.Errorf("tpm: temp file: %w", err)
	}
	defer os.Remove(quoteFile.Name())
	quoteFile.Close()

	sigFile, err := os.CreateTemp("", "tpm2-sig-*.bin")
	if err != nil {
		return nil, nil, fmt.Errorf("tpm: temp file: %w", err)
	}
	defer os.Remove(sigFile.Name())
	sigFile.Close()

	// Build PCR selection string: e.g. "sha256:0,1,2,3,4,7"
	pcrStr := buildPCRSelection(pcrIndices)

	// Encode nonce as hex for tpm2_quote --qualification flag
	nonceHex := hex.EncodeToString(nonce)

	args := []string{
		"tpm2_quote",
		"--pcr-list", pcrStr,
		"--message", quoteFile.Name(),
		"--signature", sigFile.Name(),
		"--qualification", nonceHex,
		"--hash-algorithm", "sha256",
	}

	if out, err := runCmd(ctx, args[0], args[1:]...); err != nil {
		return nil, nil, fmt.Errorf("tpm2_quote: %w (output: %s)", err, out)
	}

	quoteData, err := os.ReadFile(quoteFile.Name())
	if err != nil {
		return nil, nil, fmt.Errorf("tpm: read quote: %w", err)
	}
	sigData, err := os.ReadFile(sigFile.Name())
	if err != nil {
		return nil, nil, fmt.Errorf("tpm: read sig: %w", err)
	}

	return quoteData, sigData, nil
}

// EKCert retrieves the Endorsement Key certificate from NVRAM.
func (a *linuxAttester) EKCert(ctx context.Context) ([]byte, error) {
	if !a.available {
		return nil, ErrNotAvailable
	}

	certFile, err := os.CreateTemp("", "tpm2-ekcert-*.der")
	if err != nil {
		return nil, fmt.Errorf("tpm: temp file: %w", err)
	}
	defer os.Remove(certFile.Name())
	certFile.Close()

	// NV index 0x01C00002 is the standard EK certificate location
	if out, err := runCmd(ctx, "tpm2_nvread",
		"--output", certFile.Name(),
		"0x01C00002",
	); err != nil {
		return nil, fmt.Errorf("tpm2_nvread EK cert: %w (output: %s)", err, out)
	}

	return os.ReadFile(certFile.Name())
}

// PCRValues reads the SHA-256 PCR bank for the given indices.
func (a *linuxAttester) PCRValues(ctx context.Context, pcrs []int) (map[int][]byte, error) {
	if !a.available {
		return nil, ErrNotAvailable
	}

	pcrStr := buildPCRSelection(pcrs)
	out, err := runCmd(ctx, "tpm2_pcrread", pcrStr)
	if err != nil {
		return nil, fmt.Errorf("tpm2_pcrread: %w (output: %s)", err, out)
	}

	return parsePCROutput(out, pcrs), nil
}

// buildPCRSelection formats PCR indices as "sha256:0,1,2,3".
func buildPCRSelection(indices []int) string {
	parts := make([]string, len(indices))
	for i, idx := range indices {
		parts[i] = strconv.Itoa(idx)
	}
	return "sha256:" + strings.Join(parts, ",")
}

// parsePCROutput parses tpm2_pcrread output lines like "  sha256:0 : 0xABCD..."
func parsePCROutput(out string, pcrs []int) map[int][]byte {
	result := make(map[int][]byte, len(pcrs))
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// e.g. "sha256 :  0 : 0xaabbccdd..."
		if !strings.Contains(line, "0x") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		idxStr := strings.TrimSpace(parts[len(parts)-2])
		valStr := strings.TrimSpace(parts[len(parts)-1])
		valStr = strings.TrimPrefix(valStr, "0x")
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		val, err := hex.DecodeString(valStr)
		if err != nil {
			continue
		}
		result[idx] = val
	}
	return result
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

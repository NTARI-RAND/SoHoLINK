// Package tpm provides a platform-agnostic interface for TPM 2.0 attestation.
//
// Each platform implementation uses available system tools:
//   - Linux:   tpm2-tools (tpm2_quote, tpm2_readpublic, tpm2_getcap)
//   - Windows: PowerShell Get-Tpm / Confirm-SecureBootUEFI cmdlets
//   - macOS:   NoopAttester (Secure Enclave is not accessible via TPM 2.0 API)
//   - other:   NoopAttester
//
// When no TPM hardware is present or the tools are unavailable, Available()
// returns false and all other methods return ErrNotAvailable. The compliance
// framework treats this gracefully — nodes without a TPM fall back to the
// existing Ed25519 software attestation path.
package tpm

import (
	"context"
	"errors"
)

// ErrNotAvailable is returned when no TPM 2.0 device is accessible on the
// current platform or the required system tools are not installed.
var ErrNotAvailable = errors.New("tpm: no TPM 2.0 device available")

// Attester is the interface satisfied by all platform implementations.
type Attester interface {
	// Available returns true when a real TPM 2.0 device is accessible and
	// the required system tools are installed.
	Available() bool

	// Quote produces a TPM PCR quote. pcrIndices selects which PCRs to
	// include (e.g. [0,1,2,3,4,7] covers firmware + secure boot chain).
	// nonce is a caller-supplied anti-replay value (up to 32 bytes).
	// Returns the raw quote blob and its signature, both as DER/binary.
	Quote(ctx context.Context, pcrIndices []int, nonce []byte) (quote []byte, sig []byte, err error)

	// EKCert returns the DER-encoded Endorsement Key certificate, which
	// provides a supply-chain anchor to the TPM manufacturer's CA.
	EKCert(ctx context.Context) ([]byte, error)

	// PCRValues returns the SHA-256 PCR bank values for the given indices.
	PCRValues(ctx context.Context, pcrs []int) (map[int][]byte, error)
}

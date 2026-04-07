//go:build darwin

package tpm

// NewAttester returns a NoopAttester on macOS.
// Apple Silicon and Intel Macs with Apple T2 use the Secure Enclave / T2
// security chip, which does not expose a standard TPM 2.0 device interface.
// Compliance attestation on macOS nodes falls back to the Ed25519 software
// signature path in the compliance manager.
func NewAttester() Attester {
	return &NoopAttester{}
}

//go:build !linux && !windows && !darwin

package tpm

// NewAttester returns a NoopAttester on unsupported platforms.
func NewAttester() Attester {
	return &NoopAttester{}
}

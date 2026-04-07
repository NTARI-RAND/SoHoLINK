package tpm

import "context"

// NoopAttester is returned on platforms where no TPM 2.0 device is available.
type NoopAttester struct{}

func (n *NoopAttester) Available() bool { return false }

func (n *NoopAttester) Quote(_ context.Context, _ []int, _ []byte) ([]byte, []byte, error) {
	return nil, nil, ErrNotAvailable
}

func (n *NoopAttester) EKCert(_ context.Context) ([]byte, error) {
	return nil, ErrNotAvailable
}

func (n *NoopAttester) PCRValues(_ context.Context, _ []int) (map[int][]byte, error) {
	return nil, ErrNotAvailable
}

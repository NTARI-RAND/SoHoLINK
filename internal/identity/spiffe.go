package identity

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// Source wraps a SPIRE workload API X.509 source.
// It implements both x509svid.Source and x509bundle.Source and auto-rotates
// SVIDs as SPIRE issues new ones (default 1hr TTL).
type Source struct {
	x509Source *workloadapi.X509Source
}

// NewSource connects to the SPIRE agent socket at socketPath and returns a
// Source that streams live X.509 SVIDs. If socketPath is empty the library
// falls back to the SPIFFE_ENDPOINT_SOCKET environment variable.
func NewSource(ctx context.Context, socketPath string) (*Source, error) {
	var opts []workloadapi.X509SourceOption
	if socketPath != "" {
		opts = append(opts, workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)))
	}
	x509Source, err := workloadapi.NewX509Source(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create x509 source: %w", err)
	}
	return &Source{x509Source: x509Source}, nil
}

// TLSClientConfig returns a *tls.Config for mTLS outbound calls that presents
// our SVID and requires the server to present serverID.
func TLSClientConfig(source *Source, serverID spiffeid.ID) *tls.Config {
	return tlsconfig.MTLSClientConfig(source.x509Source, source.x509Source, tlsconfig.AuthorizeID(serverID))
}

// TLSServerConfig returns a *tls.Config for mTLS inbound connections that
// presents our SVID and accepts any peer with a valid SPIFFE identity.
func TLSServerConfig(source *Source) *tls.Config {
	return tlsconfig.MTLSServerConfig(source.x509Source, source.x509Source, tlsconfig.AuthorizeAny())
}

// Close shuts down the X.509 source and releases its SPIRE connection.
func Close(source *Source) error {
	return source.x509Source.Close()
}

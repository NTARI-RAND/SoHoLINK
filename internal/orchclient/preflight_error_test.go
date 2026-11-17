package orchclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

type preflightErrorTransport struct{ cause error }

func (f preflightErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.cause
}

func TestPreflightTransportErrorHasContext(t *testing.T) {
	cause := errors.New("connection unavailable")
	client := New("http://coordinator.invalid")
	client.httpClient.Transport = preflightErrorTransport{cause: cause}
	_, err := client.PreflightJob(context.Background(), orchestrator.SubmitJobRequest{})
	if !errors.Is(err, cause) {
		t.Fatalf("transport cause lost: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "orchclient: preflight:") {
		t.Fatalf("missing preflight error context: %v", err)
	}
}

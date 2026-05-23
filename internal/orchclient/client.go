package orchclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

const defaultTimeout = 10 * time.Second

// Client submits jobs to the orchestrator's internal HTTP listener.
// It is the portal's side of the TODO 25 fix: the portal calls this
// instead of holding its own empty NodeRegistry that never receives
// heartbeats.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New constructs a Client targeting the orchestrator's internal listener.
// baseURL is the scheme+host+port of the internal listener
// (e.g. "http://orchestrator:8083"). No trailing slash.
func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// SubmitJob encodes req as JSON, POSTs it to POST {baseURL}/internal/jobs/submit,
// and decodes the orchestrator.SubmitJobResponse from a 2xx response body.
// Any non-2xx response is returned as an error containing the HTTP status
// code and the response body verbatim. Error-class discrimination (validation
// vs no-nodes vs internal) is deferred — callers treat all errors uniformly
// for now; see Chat audit note.
func (c *Client) SubmitJob(ctx context.Context, req orchestrator.SubmitJobRequest) (orchestrator.SubmitJobResponse, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return orchestrator.SubmitJobResponse{}, fmt.Errorf("orchclient: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/jobs/submit", bytes.NewReader(b))
	if err != nil {
		return orchestrator.SubmitJobResponse{}, fmt.Errorf("orchclient: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return orchestrator.SubmitJobResponse{}, fmt.Errorf("orchclient: do request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return orchestrator.SubmitJobResponse{}, fmt.Errorf("orchclient: read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return orchestrator.SubmitJobResponse{}, fmt.Errorf("orchclient: submit job: status %d: %s",
			resp.StatusCode, string(body))
	}
	var result orchestrator.SubmitJobResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return orchestrator.SubmitJobResponse{}, fmt.Errorf("orchclient: decode response: %w", err)
	}
	return result, nil
}

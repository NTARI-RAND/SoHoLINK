package orchclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

// PreflightJob checks the orchestrator's live registry over its private API.
// The client's existing timeout bounds the read-only request.
func (c *Client) PreflightJob(ctx context.Context, req orchestrator.SubmitJobRequest) (orchestrator.JobPreflight, error) {
	var result orchestrator.JobPreflight
	resp, err := c.preflightRequest(ctx, req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		return result, orchestrator.ErrInvalidPreflight
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("orchclient: preflight unavailable: status %d", resp.StatusCode)
	}
	const limit = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return result, fmt.Errorf("orchclient: read preflight response body: %w", err)
	}
	if len(body) > limit {
		return result, fmt.Errorf("orchclient: preflight: status %d: response body exceeds %d bytes", resp.StatusCode, limit)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return orchestrator.JobPreflight{}, fmt.Errorf("orchclient: decode preflight: %w", err)
	}
	return result, nil
}

func (c *Client) preflightRequest(ctx context.Context, req orchestrator.SubmitJobRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("orchclient: marshal preflight: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/jobs/preflight", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("orchclient: build preflight: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("orchclient: preflight: %w", err)
	}
	return resp, nil
}

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
)

// AgentConfig holds the static configuration for a node agent instance.
type AgentConfig struct {
	NodeID           string
	ProviderID       string
	NodeClass        string
	CountryCode      string
	Region           string
	ControlPlaneAddr string // e.g. "https://control.soholink.org:8443"
	SPIFFESocketPath string // path to the SPIRE agent Unix socket
	TokenSecret      []byte
}

// JobAssignment is a single entry returned by PollJobs.
type JobAssignment struct {
	JobID    string `json:"job_id"`
	JobToken string `json:"job_token"`
}

// HeartbeatAgent manages registration, heartbeating, and job polling
// against the SoHoLINK control plane API over mTLS.
type HeartbeatAgent struct {
	cfg      AgentConfig
	hw       HardwareProfile
	client   *http.Client
	idSource *identity.Source
}

// NewHeartbeatAgent connects to the SPIRE agent socket, obtains an X.509 SVID,
// and constructs an mTLS HTTP client that trusts only the control plane's
// SPIFFE identity (spiffe://soholink.org/orchestrator).
//
// Note: the spec references spiffeid.RequireIDFromString — the actual function
// in go-spiffe/v2 is spiffeid.RequireFromString.
func NewHeartbeatAgent(ctx context.Context, cfg AgentConfig, hw HardwareProfile) (*HeartbeatAgent, error) {
	idSource, err := identity.NewSource(ctx, cfg.SPIFFESocketPath)
	if err != nil {
		return nil, fmt.Errorf("new heartbeat agent: identity source: %w", err)
	}

	serverID := spiffeid.RequireFromString("spiffe://soholink.org/orchestrator")
	tlsCfg := identity.TLSClientConfig(idSource, serverID)

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
		Timeout:   15 * time.Second,
	}

	return &HeartbeatAgent{
		cfg:      cfg,
		hw:       hw,
		client:   client,
		idSource: idSource,
	}, nil
}

// registerPayload is the JSON body for POST /nodes/register.
// Field names match the API server's snake_case contract.
type registerPayload struct {
	NodeID          string            `json:"node_id"`
	ProviderID      string            `json:"provider_id"`
	NodeClass       string            `json:"node_class"`
	CountryCode     string            `json:"country_code"`
	Region          string            `json:"region"`
	HardwareProfile registerHWPayload `json:"hardware_profile"`
}

type registerHWPayload struct {
	CPUCores      int  `json:"cpu_cores"`
	RAMMB         int  `json:"ram_mb"`
	GPUPresent    bool `json:"gpu_present"`
	StorageGB     int  `json:"storage_gb"`
	BandwidthMbps int  `json:"bandwidth_mbps"`
}

// Register sends the node's identity and current hardware profile to the
// control plane. Safe to call multiple times; the API upserts on conflict.
func (a *HeartbeatAgent) Register(ctx context.Context) error {
	payload := registerPayload{
		NodeID:      a.cfg.NodeID,
		ProviderID:  a.cfg.ProviderID,
		NodeClass:   a.cfg.NodeClass,
		CountryCode: a.cfg.CountryCode,
		Region:      a.cfg.Region,
		HardwareProfile: registerHWPayload{
			CPUCores:      a.hw.CPUCores,
			RAMMB:         int(a.hw.RAMMB),     // HardwareProfile.RAMMB is int64
			GPUPresent:    a.hw.GPUPresent,
			StorageGB:     int(a.hw.StorageGB), // HardwareProfile.StorageGB is int64
			BandwidthMbps: a.hw.BandwidthMbps,
		},
	}
	return a.postJSON(ctx, "/nodes/register", payload)
}

// Heartbeat notifies the control plane that this node is still alive.
func (a *HeartbeatAgent) Heartbeat(ctx context.Context) error {
	return a.postJSON(ctx, "/nodes/heartbeat", map[string]string{
		"node_id": a.cfg.NodeID,
	})
}

// PollJobs retrieves scheduled job assignments for this node from the
// control plane.
func (a *HeartbeatAgent) PollJobs(ctx context.Context) ([]JobAssignment, error) {
	url := a.cfg.ControlPlaneAddr + "/nodes/jobs?node_id=" + a.cfg.NodeID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("poll jobs: build request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll jobs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll jobs: unexpected status %d", resp.StatusCode)
	}

	var jobs []JobAssignment
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, fmt.Errorf("poll jobs: decode: %w", err)
	}
	return jobs, nil
}

// StartHeartbeatLoop registers the node on startup, then on every interval:
//   - detects current hardware and re-registers if anything has changed
//   - sends a heartbeat regardless
//
// Transient errors (network, API) are swallowed so the loop keeps running.
// The loop exits cleanly when ctx is cancelled, returning nil.
func StartHeartbeatLoop(ctx context.Context, agent *HeartbeatAgent, interval time.Duration) error {
	if err := agent.Register(ctx); err != nil {
		return fmt.Errorf("heartbeat loop: initial register: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if fresh, err := Detect(ctx); err == nil && HasChanged(agent.hw, fresh) {
				agent.hw = fresh
				// Re-register on hardware change; swallow error so heartbeat continues.
				_ = agent.Register(ctx)
			}
			// Heartbeat errors are swallowed — a missed beat is not fatal.
			_ = agent.Heartbeat(ctx)
		}
	}
}

// postJSON marshals body as JSON and POSTs it to ControlPlaneAddr+path.
// Returns an error if the response status is not 200.
func (a *HeartbeatAgent) postJSON(ctx context.Context, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("post %s: marshal: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.ControlPlaneAddr+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("post %s: build request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post %s: unexpected status %d", path, resp.StatusCode)
	}
	return nil
}

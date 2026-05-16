package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	JobID     string `json:"job_id"`
	JobToken  string `json:"job_token"`
	Image     string `json:"container_image"`
	PrinterID string `json:"printer_id,omitempty"`
}

// HeartbeatAgent manages registration, heartbeating, and job polling
// against the SoHoLINK control plane API over mTLS.
type HeartbeatAgent struct {
	cfg         AgentConfig
	hw          HardwareProfile
	client      *http.Client
	idSource    *identity.Source
	optOutStore *OptOutStore
}

// NewHeartbeatAgent connects to the SPIRE agent socket, obtains an X.509 SVID,
// and constructs an mTLS HTTP client that trusts only the control plane's
// SPIFFE identity (spiffe://soholink.org/orchestrator).
//
// Note: the spec references spiffeid.RequireIDFromString — the actual function
// in go-spiffe/v2 is spiffeid.RequireFromString.
func NewHeartbeatAgent(ctx context.Context, cfg AgentConfig, hw HardwareProfile, optOutStore *OptOutStore) (*HeartbeatAgent, error) {
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
		cfg:         cfg,
		hw:          hw,
		client:      client,
		idSource:    idSource,
		optOutStore: optOutStore,
	}, nil
}

// NewTelemetryClient returns an mTLS HTTP client that presents this node's
// SPIRE-issued SVID to the control plane. Use this for telemetry and job
// completion calls in runJob — the plain http.Client will be rejected by
// the control plane's RequireSPIFFE middleware.
func (a *HeartbeatAgent) NewTelemetryClient() *http.Client {
	serverID := spiffeid.RequireFromString("spiffe://soholink.org/orchestrator")
	tlsCfg := identity.TLSClientConfig(a.idSource, serverID)
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
		Timeout:   15 * time.Second,
	}
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

type printerPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type registerHWPayload struct {
	CPUCores      int              `json:"cpu_cores"`
	RAMMB         int              `json:"ram_mb"`
	GPUPresent    bool             `json:"gpu_present"`
	StorageGB     int              `json:"storage_gb"`
	BandwidthMbps int              `json:"bandwidth_mbps"`
	Printers      []printerPayload `json:"printers,omitempty"`
}

// heartbeatResp is the decoded response from POST /nodes/heartbeat.
type heartbeatResp struct {
	OK     bool `json:"ok"`
	OptOut *struct {
		Version         int             `json:"version"`
		ComputeEnabled  bool            `json:"compute_enabled"`
		StorageEnabled  bool            `json:"storage_enabled"`
		PrintingEnabled bool            `json:"printing_enabled"`
		EnabledPrinters map[string]bool `json:"enabled_printers"`
	} `json:"opt_out"`
	RequestPrinterReport bool `json:"request_printer_report"`
}

// Register sends the node's identity and current hardware profile to the
// control plane. Safe to call multiple times; the API upserts on conflict.
func (a *HeartbeatAgent) Register(ctx context.Context) error {
	printers := make([]printerPayload, 0, len(a.hw.Printers))
	for _, p := range a.hw.Printers {
		printers = append(printers, printerPayload{ID: p.ID, Name: p.Name})
	}
	payload := registerPayload{
		NodeID:      a.cfg.NodeID,
		ProviderID:  a.cfg.ProviderID,
		NodeClass:   a.cfg.NodeClass,
		CountryCode: a.cfg.CountryCode,
		Region:      a.cfg.Region,
		HardwareProfile: registerHWPayload{
			CPUCores:      a.hw.CPUCores,
			RAMMB:         int(a.hw.RAMMB),
			GPUPresent:    a.hw.GPUPresent,
			StorageGB:     int(a.hw.StorageGB),
			BandwidthMbps: a.hw.BandwidthMbps,
			Printers:      printers,
		},
	}
	return a.postJSON(ctx, "/nodes/register", payload)
}

// Heartbeat notifies the control plane that this node is still alive.
// It sends the current opt_out_version and printer_hash so the server can
// push updated opt-out state when stale and request a full printer re-report
// when the hash does not match. Returned opt-out updates are applied to
// optOutStore and persisted to disk.
func (a *HeartbeatAgent) Heartbeat(ctx context.Context) error {
	var version int
	if a.optOutStore != nil {
		version = a.optOutStore.Get().Version
	}

	payload := map[string]any{
		"node_id":         a.cfg.NodeID,
		"opt_out_version": version,
		"printer_hash":    PrinterHash(a.hw.Printers),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("heartbeat: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.ControlPlaneAddr+"/nodes/heartbeat", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("heartbeat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat: unexpected status %d", resp.StatusCode)
	}

	var hbResp heartbeatResp
	if err := json.NewDecoder(resp.Body).Decode(&hbResp); err != nil {
		return fmt.Errorf("heartbeat: decode response: %w", err)
	}

	if hbResp.OptOut != nil && a.optOutStore != nil {
		newOO := ResourceOptOut{
			Version:         hbResp.OptOut.Version,
			ComputeEnabled:  hbResp.OptOut.ComputeEnabled,
			StorageEnabled:  hbResp.OptOut.StorageEnabled,
			PrintingEnabled: hbResp.OptOut.PrintingEnabled,
			EnabledPrinters: hbResp.OptOut.EnabledPrinters,
		}
		if newOO.EnabledPrinters == nil {
			newOO.EnabledPrinters = map[string]bool{}
		}
		a.optOutStore.Set(newOO)
		if saveErr := SaveOptOutToFile(OptOutCachePath(), newOO); saveErr != nil {
			log.Printf("heartbeat: save opt-out: %v", saveErr)
		}
	}

	if hbResp.RequestPrinterReport {
		if err := a.ReportPrinters(ctx); err != nil {
			log.Printf("heartbeat: report printers: %v", err)
		}
	}

	return nil
}

// ReportPrinters sends the full current printer list to the control plane.
// Called when the server signals a hash mismatch via RequestPrinterReport.
func (a *HeartbeatAgent) ReportPrinters(ctx context.Context) error {
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	printers := make([]entry, 0, len(a.hw.Printers))
	for _, p := range a.hw.Printers {
		printers = append(printers, entry{ID: p.ID, Name: p.Name})
	}
	return a.postJSON(ctx, "/nodes/printers", map[string]any{
		"node_id":  a.cfg.NodeID,
		"printers": printers,
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

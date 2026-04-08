package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// TelemetryPayload is the signed telemetry record emitted by the agent
// during job execution. The Signature field is an HMAC-SHA256 over the
// base64url-encoded canonical message string.
type TelemetryPayload struct {
	NodeID        string    `json:"node_id"`
	JobID         string    `json:"job_id"`
	CPUPct        float64   `json:"cpu_pct"`
	RAMPct        float64   `json:"ram_pct"`
	BandwidthMbps int       `json:"bandwidth_mbps"`
	Timestamp     time.Time `json:"timestamp"`
	Signature     string    `json:"signature"`
}

// SignTelemetry attaches an HMAC-SHA256 signature to payload and returns
// the updated payload. The canonical message is:
//
//	base64RawURL( nodeID|jobID|cpu_pct|ram_pct|timestamp_RFC3339 )
//
// The signature is base64RawURL( HMAC-SHA256( canonical, secret ) ).
func SignTelemetry(payload TelemetryPayload, secret []byte) (TelemetryPayload, error) {
	if payload.NodeID == "" || payload.JobID == "" {
		return TelemetryPayload{}, fmt.Errorf("sign telemetry: NodeID and JobID must not be empty")
	}
	raw := payload.NodeID + "|" +
		payload.JobID + "|" +
		fmt.Sprintf("%.2f", payload.CPUPct) + "|" +
		fmt.Sprintf("%.2f", payload.RAMPct) + "|" +
		payload.Timestamp.UTC().Format(time.RFC3339)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	payload.Signature = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload, nil
}

// CollectTelemetry samples current CPU and RAM utilisation, assembles a
// TelemetryPayload, and signs it with the agent's token secret.
// BandwidthMbps is left at 0 — network metering is added in Phase 3.
func CollectTelemetry(ctx context.Context, nodeID, jobID string, secret []byte) (TelemetryPayload, error) {
	pcts, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err != nil {
		return TelemetryPayload{}, fmt.Errorf("collect telemetry: cpu percent: %w", err)
	}
	var cpuPct float64
	if len(pcts) > 0 {
		cpuPct = pcts[0]
	}

	vmStat, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return TelemetryPayload{}, fmt.Errorf("collect telemetry: memory: %w", err)
	}

	p := TelemetryPayload{
		NodeID:    nodeID,
		JobID:     jobID,
		CPUPct:    cpuPct,
		RAMPct:    vmStat.UsedPercent,
		Timestamp: time.Now().UTC(),
	}
	return SignTelemetry(p, secret)
}

// EmitTelemetry POSTs the signed payload to the control plane endpoint
// POST /jobs/{jobID}/telemetry. Returns an error for non-200 responses.
func EmitTelemetry(ctx context.Context, client *http.Client, controlPlaneAddr string, payload TelemetryPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("emit telemetry: marshal: %w", err)
	}

	url := controlPlaneAddr + "/jobs/" + payload.JobID + "/telemetry"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("emit telemetry: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("emit telemetry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("emit telemetry: unexpected status %d", resp.StatusCode)
	}
	return nil
}

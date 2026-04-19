package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// NodeConfig is the persisted agent identity written to agent.conf on first run.
// All fields are required for the agent to start normally.
type NodeConfig struct {
	NodeID         string `json:"node_id"`
	TokenSecret    string `json:"token_secret"`
	SpireJoinToken string `json:"spire_join_token,omitempty"`
}

// DefaultConfigPath returns the platform config file path.
// On Windows this is %PROGRAMDATA%\SoHoLINK\agent.conf.
// Falls back to the current directory on other platforms.
func DefaultConfigPath() string {
	if dir := os.Getenv("PROGRAMDATA"); dir != "" {
		return filepath.Join(dir, "SoHoLINK", "agent.conf")
	}
	return "agent.conf"
}

// LoadConfig reads and parses the agent config file at path.
// Returns an error if the file does not exist or is malformed.
func LoadConfig(path string) (NodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("load config: %w", err)
	}
	var cfg NodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return NodeConfig{}, fmt.Errorf("load config: parse: %w", err)
	}
	if cfg.NodeID == "" || cfg.TokenSecret == "" {
		return NodeConfig{}, fmt.Errorf("load config: node_id and token_secret are required")
	}
	return cfg, nil
}

// SaveConfig writes cfg as JSON to path, creating parent directories as needed.
func SaveConfig(path string, cfg NodeConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("save config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("save config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("save config: write: %w", err)
	}
	return nil
}

// claimResponse is the JSON body returned by POST /nodes/claim.
type claimResponse struct {
	NodeID         string `json:"node_id"`
	TokenSecret    string `json:"token_secret"`
	SpireJoinToken string `json:"spire_join_token,omitempty"`
}

// ClaimNode calls POST /nodes/claim on the control plane using the provided
// mTLS client and registration token. On success it returns a NodeConfig
// ready to be persisted with SaveConfig.
func ClaimNode(ctx context.Context, client *http.Client, controlPlaneAddr, regToken string, hw HardwareProfile, hostname, countryCode, region string) (NodeConfig, error) {
	body, err := json.Marshal(map[string]any{
		"token":        regToken,
		"hostname":     hostname,
		"country_code": countryCode,
		"region":       region,
		"hardware_profile": map[string]any{
			"cpu_cores":      hw.CPUCores,
			"ram_mb":         hw.RAMMB,
			"gpu_present":    hw.GPUPresent,
			"storage_gb":     hw.StorageGB,
			"bandwidth_mbps": hw.BandwidthMbps,
		},
	})
	if err != nil {
		return NodeConfig{}, fmt.Errorf("claim node: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		controlPlaneAddr+"/nodes/claim", bytes.NewReader(body))
	if err != nil {
		return NodeConfig{}, fmt.Errorf("claim node: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("claim node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NodeConfig{}, fmt.Errorf("claim node: unexpected status %d", resp.StatusCode)
	}

	var cr claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return NodeConfig{}, fmt.Errorf("claim node: decode: %w", err)
	}
	if cr.NodeID == "" || cr.TokenSecret == "" {
		return NodeConfig{}, fmt.Errorf("claim node: incomplete response from control plane")
	}

	return NodeConfig{
		NodeID:         cr.NodeID,
		TokenSecret:    cr.TokenSecret,
		SpireJoinToken: cr.SpireJoinToken,
	}, nil
}

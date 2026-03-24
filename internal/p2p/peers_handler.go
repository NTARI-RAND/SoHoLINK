package p2p

import (
	"encoding/json"
	"net/http"
	"time"
)

// GPUProfileJSON represents detailed GPU capabilities in the API response.
type GPUProfileJSON struct {
	Model             string  `json:"model"`
	VRAMFree          int64   `json:"vram_free_mb"`
	VRAMTotal         int64   `json:"vram_total_mb"`
	ComputeCapability string  `json:"compute_capability,omitempty"`
	Temperature       float32 `json:"temperature_celsius,omitempty"`
	PCIeBandwidth     int64   `json:"pcie_bandwidth_mbs,omitempty"`
}

// PeerJSON is the JSON representation of a peer for the /api/peers endpoint.
type PeerJSON struct {
	DID       string           `json:"did"`
	APIAddr   string           `json:"api_addr"`
	IPFSAddr  string           `json:"ipfs_addr,omitempty"`
	CPU       float64          `json:"cpu_cores"`
	RAMGB     float64          `json:"ram_gb"`
	DiskGB    int64            `json:"disk_gb"`
	GPU       string           `json:"gpu,omitempty"`
	GPUProfile *GPUProfileJSON `json:"gpu_profile,omitempty"`
	Region    string           `json:"region,omitempty"`
	LastSeen  time.Time        `json:"last_seen"`
}

// HandlePeers returns an http.HandlerFunc that serves GET /api/peers.
// It lists all currently live LAN-discovered peers from the mesh.
func (m *Mesh) HandlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	peers := m.Peers()
	out := make([]PeerJSON, 0, len(peers))
	for _, p := range peers {
		var gpuProfile *GPUProfileJSON
		if p.GPU != "" {
			gpuProfile = &GPUProfileJSON{
				Model:             p.GPU,
				VRAMFree:          p.GPUVRAMFree,
				VRAMTotal:         p.GPUVRAMTotal,
				ComputeCapability: p.GPUComputeCapability,
				Temperature:       p.GPUTemperature,
				PCIeBandwidth:     p.GPUPCIeBandwidth,
			}
		}
		out = append(out, PeerJSON{
			DID:        p.DID,
			APIAddr:    p.APIAddr,
			IPFSAddr:   p.IPFSAddr,
			CPU:        p.CPU,
			RAMGB:      p.RAMGB,
			DiskGB:     p.DiskGB,
			GPU:        p.GPU,
			GPUProfile: gpuProfile,
			Region:     p.Region,
			LastSeen:   p.LastSeen,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"count": len(out),
		"peers": out,
	})
}

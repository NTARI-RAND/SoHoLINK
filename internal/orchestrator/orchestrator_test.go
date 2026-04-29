package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/types"
)

// newOnlineNode is a test helper that builds a NodeEntry with Status "online".
func newOnlineNode(id, country string, cpu, ramMB, storageGB int, gpu bool) NodeEntry {
	return NodeEntry{
		NodeID:        id,
		ProviderID:    "test-provider",
		NodeClass:     "A",
		CountryCode:   country,
		Status:        "online",
		LastHeartbeat: time.Now(),
		HardwareProfile: HardwareProfile{
			CPUCores:  cpu,
			RAMMB:     ramMB,
			StorageGB: storageGB,
			GPUPresent: gpu,
		},
	}
}

func TestNodeRegistry_FindMatch_NoNodesAvailable(t *testing.T) {
	r := NewNodeRegistry()

	_, err := r.FindMatch(MatchRequest{
		WorkloadType: types.MarketplaceAIInference,
		CPUCores:     2,
		RAMMB:        4096,
	})
	if err == nil {
		t.Fatal("expected error for empty registry, got nil")
	}
}

func TestNodeRegistry_FindMatch_NodeSelected(t *testing.T) {
	r := NewNodeRegistry()
	r.Register(newOnlineNode("node-us-1", "US", 8, 16384, 100, false))

	candidates, err := r.FindMatch(MatchRequest{
		WorkloadType: types.MarketplaceBatchCompute,
		CPUCores:     4,
		RAMMB:        8192,
		StorageGB:    50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].NodeID != "node-us-1" {
		t.Errorf("expected node-us-1, got %s", candidates[0].NodeID)
	}
}

func TestNodeRegistry_FindMatch_GeoConstraint(t *testing.T) {
	r := NewNodeRegistry()
	r.Register(newOnlineNode("node-us-1", "US", 8, 16384, 100, false))
	r.Register(newOnlineNode("node-gb-1", "GB", 8, 16384, 100, false))

	candidates, err := r.FindMatch(MatchRequest{
		WorkloadType:      types.MarketplaceBatchCompute,
		CountryConstraint: "US",
		CPUCores:          2,
		RAMMB:             4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].CountryCode != "US" {
		t.Errorf("expected US node, got %s", candidates[0].CountryCode)
	}
}

func TestNodeRegistry_FindMatch_GPURequired(t *testing.T) {
	r := NewNodeRegistry()
	r.Register(newOnlineNode("node-cpu-only", "US", 8, 16384, 100, false))
	r.Register(newOnlineNode("node-gpu-1", "US", 8, 16384, 100, true))

	candidates, err := r.FindMatch(MatchRequest{
		WorkloadType: types.MarketplaceAIInference,
		CPUCores:     2,
		RAMMB:        4096,
		GPURequired:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 GPU candidate, got %d", len(candidates))
	}
	if candidates[0].NodeID != "node-gpu-1" {
		t.Errorf("expected node-gpu-1, got %s", candidates[0].NodeID)
	}
}

func TestNodeRegistry_FindMatch_OfflineNodeExcluded(t *testing.T) {
	r := NewNodeRegistry()
	offline := newOnlineNode("node-offline", "US", 8, 16384, 100, false)
	offline.Status = "offline"
	r.Register(offline)

	_, err := r.FindMatch(MatchRequest{
		WorkloadType: types.MarketplaceBatchCompute,
		CPUCores:     2,
		RAMMB:        4096,
	})
	if err == nil {
		t.Fatal("expected error: offline node should not match")
	}
}

func TestNodeRegistry_FindMatch_InsufficientResources(t *testing.T) {
	r := NewNodeRegistry()
	r.Register(newOnlineNode("node-small", "US", 2, 2048, 20, false))

	_, err := r.FindMatch(MatchRequest{
		WorkloadType: types.MarketplaceBatchCompute,
		CPUCores:     8,
		RAMMB:        16384,
		StorageGB:    100,
	})
	if err == nil {
		t.Fatal("expected error: node does not meet resource requirements")
	}
}

func TestNodeRegistry_Heartbeat_UnknownNode(t *testing.T) {
	r := NewNodeRegistry()
	err := r.Heartbeat("nonexistent-node")
	if err == nil {
		t.Fatal("expected error for heartbeat on unknown node")
	}
}

func TestNodeRegistry_Evict(t *testing.T) {
	r := NewNodeRegistry()
	r.Register(newOnlineNode("node-1", "US", 4, 8192, 50, false))

	r.Evict("node-1")

	_, err := r.FindMatch(MatchRequest{CPUCores: 1, RAMMB: 1024})
	if err == nil {
		t.Fatal("expected error after eviction")
	}
}

func TestNodeRegistry_EvictStale(t *testing.T) {
	r := NewNodeRegistry()

	stale := newOnlineNode("node-stale", "US", 4, 8192, 50, false)
	stale.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	r.Register(stale)

	fresh := newOnlineNode("node-fresh", "US", 4, 8192, 50, false)
	r.Register(fresh)

	// Evict anything older than 5 minutes.
	r.evictStale(5 * time.Minute)

	candidates, err := r.FindMatch(MatchRequest{CPUCores: 1, RAMMB: 1024})
	if err != nil {
		t.Fatalf("expected fresh node to survive eviction: %v", err)
	}
	if len(candidates) != 1 || candidates[0].NodeID != "node-fresh" {
		t.Errorf("expected only node-fresh, got %+v", candidates)
	}
}

func TestSubmitJobRequest_Validate(t *testing.T) {
	cases := []struct {
		name        string
		req         SubmitJobRequest
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid",
			req:     SubmitJobRequest{ConsumerID: "c", WorkloadType: types.MarketplaceAppHosting},
			wantErr: false,
		},
		{
			name:        "empty consumer",
			req:         SubmitJobRequest{WorkloadType: types.MarketplaceAppHosting},
			wantErr:     true,
			errContains: "ConsumerID",
		},
		{
			name:        "empty workload type",
			req:         SubmitJobRequest{ConsumerID: "c"},
			wantErr:     true,
			errContains: "WorkloadType is required",
		},
		{
			name:        "unknown workload type",
			req:         SubmitJobRequest{ConsumerID: "c", WorkloadType: types.MarketplaceWorkloadType("banana")},
			wantErr:     true,
			errContains: "banana",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// writeTempAllowlist marshals al to a temp file and returns its path.
func writeTempAllowlist(t *testing.T, al agent.Allowlist) string {
	t.Helper()
	data, err := json.Marshal(al)
	if err != nil {
		t.Fatalf("marshal allowlist: %v", err)
	}
	path := filepath.Join(t.TempDir(), "allowlist.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	return path
}

func TestSubmitJob_RejectsImageNotInAllowlist(t *testing.T) {
	path := writeTempAllowlist(t, agent.Allowlist{
		Version:  1,
		IssuedAt: time.Now(),
		Entries: []agent.AllowlistEntry{
			{
				Name:   "soholink/compute-worker",
				Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Type:   agent.WorkloadCompute,
				Egress: agent.EgressOutbound,
			},
		},
	})
	orch := New(nil, NewNodeRegistry(), nil, nil, path)

	_, err := orch.SubmitJob(context.Background(), SubmitJobRequest{
		ConsumerID:     "c1",
		WorkloadType:   types.MarketplaceAppHosting,
		ContainerImage: "soholink/other-worker@sha256:1111111111111111111111111111111111111111111111111111111111111111",
	})
	if err == nil {
		t.Fatal("expected error for image not in allowlist, got nil")
	}
	if !strings.Contains(err.Error(), "image not in allowlist") {
		t.Errorf("expected 'image not in allowlist' in error, got: %v", err)
	}
}

func TestSubmitJob_RejectsMappingInconsistency(t *testing.T) {
	// Allowlist declares image as storage, but ai_inference maps to compute.
	path := writeTempAllowlist(t, agent.Allowlist{
		Version:  1,
		IssuedAt: time.Now(),
		Entries: []agent.AllowlistEntry{
			{
				Name:   "soholink/storage-worker",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Type:   agent.WorkloadStorage,
				Egress: agent.EgressOutbound,
			},
		},
	})
	orch := New(nil, NewNodeRegistry(), nil, nil, path)

	_, err := orch.SubmitJob(context.Background(), SubmitJobRequest{
		ConsumerID:     "c1",
		WorkloadType:   types.MarketplaceAIInference,
		ContainerImage: "soholink/storage-worker@sha256:1111111111111111111111111111111111111111111111111111111111111111",
	})
	if err == nil {
		t.Fatal("expected error for workload type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "workload type mismatch") {
		t.Errorf("expected 'workload type mismatch' in error, got: %v", err)
	}
}

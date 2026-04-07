package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// newTestScheduler creates a FedScheduler wired to an in-memory store.
func newTestScheduler(t *testing.T) *FedScheduler {
	t.Helper()
	s, err := store.NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	return NewFedScheduler(s)
}

// seedWorkload plants a WorkloadState directly into the scheduler's map,
// bypassing the full scheduling path (no node discovery needed for unit tests).
func seedWorkload(sched *FedScheduler, workloadID string, placements []Placement) {
	w := &Workload{
		WorkloadID: workloadID,
		OwnerDID:   "did:soho:owner1",
		Type:       "container",
		Replicas:   len(placements),
		Spec:       WorkloadSpec{CPUCores: 0.5, MemoryMB: 256},
	}
	sched.mu.Lock()
	sched.ActiveWorkloads[workloadID] = &WorkloadState{
		Workload:   w,
		Placements: placements,
		Health:     HealthStatus{Status: "running"},
	}
	sched.mu.Unlock()
}

// ── RescheduleFailed ─────────────────────────────────────────────────────────

func TestRescheduleFailed_NoWorkload(t *testing.T) {
	sched := newTestScheduler(t)
	// Should return nil quietly when the workload doesn't exist.
	err := sched.RescheduleFailed(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil for unknown workload, got: %v", err)
	}
}

func TestRescheduleFailed_NoFailedPlacements(t *testing.T) {
	sched := newTestScheduler(t)
	placements := []Placement{
		{PlacementID: "pl-1", WorkloadID: "wl-1", NodeDID: "did:soho:nodeA", Status: "running", StartedAt: time.Now()},
	}
	seedWorkload(sched, "wl-1", placements)

	// All placements are "running" — nothing to reschedule.
	err := sched.RescheduleFailed(context.Background(), "wl-1")
	if err != nil {
		// No nodes available error is acceptable since discovery has no nodes.
		// What matters is no panic and the existing placement is untouched.
		t.Logf("RescheduleFailed returned (expected, no nodes registered): %v", err)
	}

	sched.mu.RLock()
	defer sched.mu.RUnlock()
	ws := sched.ActiveWorkloads["wl-1"]
	if ws == nil {
		t.Fatal("workload removed unexpectedly")
	}
	if ws.Placements[0].Status != "running" {
		t.Errorf("healthy placement status changed to %q", ws.Placements[0].Status)
	}
}

// ── WorkloadMonitor.checkAll ──────────────────────────────────────────────────

func TestWorkloadMonitor_CheckAll_AllHealthy(t *testing.T) {
	sched := newTestScheduler(t)
	placements := []Placement{
		{PlacementID: "pl-1", WorkloadID: "wl-1", NodeDID: "did:soho:nodeA", Status: "running"},
		{PlacementID: "pl-2", WorkloadID: "wl-1", NodeDID: "did:soho:nodeB", Status: "running"},
	}
	seedWorkload(sched, "wl-1", placements)

	sched.monitor.checkAll(context.Background())

	sched.mu.RLock()
	defer sched.mu.RUnlock()
	if sched.ActiveWorkloads["wl-1"].Health.Status != "healthy" {
		t.Errorf("health = %q, want %q", sched.ActiveWorkloads["wl-1"].Health.Status, "healthy")
	}
}

func TestWorkloadMonitor_CheckAll_Degraded(t *testing.T) {
	sched := newTestScheduler(t)
	placements := []Placement{
		{PlacementID: "pl-1", WorkloadID: "wl-2", NodeDID: "did:soho:nodeA", Status: "running"},
		{PlacementID: "pl-2", WorkloadID: "wl-2", NodeDID: "did:soho:nodeB", Status: "failed"},
	}
	seedWorkload(sched, "wl-2", placements)

	sched.monitor.checkAll(context.Background())

	sched.mu.RLock()
	defer sched.mu.RUnlock()
	h := sched.ActiveWorkloads["wl-2"].Health.Status
	// After checkAll the health should be "degraded" or "recovering" (if reschedule ran).
	if h != "degraded" && h != "recovering" {
		t.Errorf("health = %q, want degraded or recovering", h)
	}
}

func TestWorkloadMonitor_CheckAll_Unhealthy(t *testing.T) {
	sched := newTestScheduler(t)
	placements := []Placement{
		{PlacementID: "pl-1", WorkloadID: "wl-3", NodeDID: "did:soho:nodeA", Status: "failed"},
	}
	seedWorkload(sched, "wl-3", placements)

	sched.monitor.checkAll(context.Background())

	sched.mu.RLock()
	defer sched.mu.RUnlock()
	h := sched.ActiveWorkloads["wl-3"].Health.Status
	// Should be "unhealthy" (no candidates) or "recovering" if nodes were found.
	if h != "unhealthy" && h != "recovering" {
		t.Errorf("health = %q, want unhealthy or recovering", h)
	}
}

// ── ListActiveWorkloads snapshot safety ──────────────────────────────────────

func TestListActiveWorkloads_Snapshot(t *testing.T) {
	sched := newTestScheduler(t)
	placements := []Placement{
		{PlacementID: "pl-1", WorkloadID: "wl-snap", NodeDID: "did:soho:nodeA", Status: "running"},
	}
	seedWorkload(sched, "wl-snap", placements)

	list := sched.ListActiveWorkloads()
	if len(list) != 1 {
		t.Fatalf("expected 1 workload, got %d", len(list))
	}

	// Mutating the snapshot must not affect the scheduler's internal state.
	list[0].Placements[0].Status = "poisoned"

	sched.mu.RLock()
	defer sched.mu.RUnlock()
	if sched.ActiveWorkloads["wl-snap"].Placements[0].Status == "poisoned" {
		t.Error("snapshot mutation leaked into scheduler state (deep copy broken)")
	}
}

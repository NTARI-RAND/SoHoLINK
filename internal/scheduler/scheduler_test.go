package scheduler

import (
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

func TestClassScore(t *testing.T) {
	cases := []struct {
		class string
		want  float64
	}{
		{"A", 4.0},
		{"B", 3.0},
		{"C", 2.0},
		{"D", 1.0},
		{"", 0.0},
		{"X", 0.0},
	}
	for _, tc := range cases {
		got := classScore(tc.class)
		if got != tc.want {
			t.Errorf("classScore(%q) = %v, want %v", tc.class, got, tc.want)
		}
	}
}

func TestFreshnessScore(t *testing.T) {
	if s := freshnessScore(time.Now()); s < 0.999 {
		t.Errorf("fresh node: want >= 0.999, got %v", s)
	}
	if s := freshnessScore(time.Now().Add(-30 * time.Minute)); s != 0.0 {
		t.Errorf("30-min stale node: want 0.0, got %v", s)
	}
	if s := freshnessScore(time.Now().Add(-60 * time.Minute)); s != 0.0 {
		t.Errorf("60-min stale node: want 0.0, got %v", s)
	}
	mid := freshnessScore(time.Now().Add(-15 * time.Minute))
	if mid < 0.45 || mid > 0.55 {
		t.Errorf("15-min stale node: want ~0.5, got %v", mid)
	}
}

func TestSchedule_InsufficientCandidates(t *testing.T) {
	_, err := Schedule(nil, orchestrator.SLAStandard)
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
	_, err = Schedule([]orchestrator.NodeEntry{makeNode("A", 4)}, orchestrator.SLAReliable)
	if err == nil {
		t.Fatal("expected error when candidates < tier")
	}
}

func TestSchedule_ClassOrdering(t *testing.T) {
	candidates := []orchestrator.NodeEntry{
		makeNode("C", 4),
		makeNode("A", 4),
		makeNode("B", 4),
	}
	result, err := Schedule(candidates, orchestrator.SLAStandard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeClass != "A" {
		t.Errorf("top node should be class A, got %q", result[0].NodeClass)
	}
}

func TestSchedule_FreshnessBreaksTie(t *testing.T) {
	stale := makeNodeWithHeartbeat("B", 4, time.Now().Add(-20*time.Minute))
	fresh := makeNodeWithHeartbeat("B", 4, time.Now())
	stale.NodeID = "stale"
	fresh.NodeID = "fresh"

	result, err := Schedule([]orchestrator.NodeEntry{stale, fresh}, orchestrator.SLAStandard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeID != "fresh" {
		t.Errorf("fresher node should rank first, got %q", result[0].NodeID)
	}
}

func TestSchedule_CapacityBreaksTie(t *testing.T) {
	lo := makeNode("A", 2)
	hi := makeNode("A", 8)
	lo.NodeID = "lo"
	hi.NodeID = "hi"

	result, err := Schedule([]orchestrator.NodeEntry{lo, hi}, orchestrator.SLAStandard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeID != "hi" {
		t.Errorf("higher-CPU node should rank first, got %q", result[0].NodeID)
	}
}

func TestSchedule_SLAReliableReturnsTwo(t *testing.T) {
	candidates := []orchestrator.NodeEntry{
		makeNode("A", 4),
		makeNode("B", 4),
		makeNode("C", 4),
	}
	result, err := Schedule(candidates, orchestrator.SLAReliable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("SLAReliable should return 2 nodes, got %d", len(result))
	}
}

func TestSchedule_SLAPremiumReturnsThree(t *testing.T) {
	candidates := []orchestrator.NodeEntry{
		makeNode("A", 4),
		makeNode("B", 4),
		makeNode("C", 4),
	}
	result, err := Schedule(candidates, orchestrator.SLAPremium)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("SLAPremium should return 3 nodes, got %d", len(result))
	}
}

func makeNode(class string, cpuCores int) orchestrator.NodeEntry {
	return makeNodeWithHeartbeat(class, cpuCores, time.Now())
}

func makeNodeWithHeartbeat(class string, cpuCores int, heartbeat time.Time) orchestrator.NodeEntry {
	return orchestrator.NodeEntry{
		NodeID:    class + "-node",
		NodeClass: class,
		Status:    "online",
		HardwareProfile: orchestrator.HardwareProfile{
			CPUCores: cpuCores,
		},
		LastHeartbeat: heartbeat,
	}
}

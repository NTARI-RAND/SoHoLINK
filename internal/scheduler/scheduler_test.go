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
	_, err := Schedule(nil, orchestrator.SLAStandard, orchestrator.PlacementContext{})
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
	_, err = Schedule([]orchestrator.NodeEntry{makeNode("A", 4)}, orchestrator.SLAReliable, orchestrator.PlacementContext{})
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
	result, err := Schedule(candidates, orchestrator.SLAStandard, orchestrator.PlacementContext{})
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

	result, err := Schedule([]orchestrator.NodeEntry{stale, fresh}, orchestrator.SLAStandard, orchestrator.PlacementContext{})
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

	result, err := Schedule([]orchestrator.NodeEntry{lo, hi}, orchestrator.SLAStandard, orchestrator.PlacementContext{})
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
	result, err := Schedule(candidates, orchestrator.SLAReliable, orchestrator.PlacementContext{})
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
	result, err := Schedule(candidates, orchestrator.SLAPremium, orchestrator.PlacementContext{})
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

// ── B3: locality-first + idle-first scoring ──────────────────────────────────

func TestLocalityScore_Tiers(t *testing.T) {
	pctx := orchestrator.PlacementContext{RequesterCountry: "US", RequesterRegion: "KY"}

	sameRegion := makeNode("A", 4)
	sameRegion.CountryCode, sameRegion.Region = "US", "KY"
	sameCountry := makeNode("A", 4)
	sameCountry.CountryCode, sameCountry.Region = "US", "CA"
	global := makeNode("A", 4)
	global.CountryCode, global.Region = "DE", "BE"

	if got := localityScore(sameRegion, pctx); got != 0.6 {
		t.Errorf("same region: got %v, want 0.6", got)
	}
	if got := localityScore(sameCountry, pctx); got != 0.3 {
		t.Errorf("same country: got %v, want 0.3", got)
	}
	if got := localityScore(global, pctx); got != 0.0 {
		t.Errorf("global: got %v, want 0.0", got)
	}
}

func TestLocalityScore_EmptyValuesNeverMatch(t *testing.T) {
	// Empty region/country on either side must contribute 0, never match.
	node := makeNode("A", 4)
	node.CountryCode, node.Region = "", ""
	if got := localityScore(node, orchestrator.PlacementContext{}); got != 0.0 {
		t.Errorf("empty-vs-empty: got %v, want 0.0", got)
	}
	node.CountryCode = "US"
	if got := localityScore(node, orchestrator.PlacementContext{}); got != 0.0 {
		t.Errorf("empty requester: got %v, want 0.0", got)
	}
}

func TestSchedule_SameRegionBeatsSameCountryBeatsGlobal(t *testing.T) {
	pctx := orchestrator.PlacementContext{RequesterCountry: "US", RequesterRegion: "KY"}

	sameRegion := makeNode("A", 4)
	sameRegion.NodeID, sameRegion.CountryCode, sameRegion.Region = "region", "US", "KY"
	sameCountry := makeNode("A", 4)
	sameCountry.NodeID, sameCountry.CountryCode, sameCountry.Region = "country", "US", "CA"
	global := makeNode("A", 4)
	global.NodeID, global.CountryCode, global.Region = "global", "DE", "BE"

	result, err := Schedule([]orchestrator.NodeEntry{global, sameCountry, sameRegion}, orchestrator.SLAPremium, pctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"region", "country", "global"}
	for i, id := range want {
		if result[i].NodeID != id {
			t.Errorf("rank %d: got %q, want %q", i, result[i].NodeID, id)
		}
	}
}

func TestSchedule_LocalityDominatesClass(t *testing.T) {
	// wLocality intent: a same-region class-C node must outrank an
	// out-of-region class-A node (10.0×0.6 = 6.0 > classScore gap of 2.0).
	pctx := orchestrator.PlacementContext{RequesterCountry: "US", RequesterRegion: "KY"}

	localC := makeNode("C", 4)
	localC.NodeID, localC.CountryCode, localC.Region = "local-c", "US", "KY"
	remoteA := makeNode("A", 4)
	remoteA.NodeID, remoteA.CountryCode, remoteA.Region = "remote-a", "DE", "BE"

	result, err := Schedule([]orchestrator.NodeEntry{remoteA, localC}, orchestrator.SLAStandard, pctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeID != "local-c" {
		t.Errorf("same-region class-C should beat out-of-region class-A, got %q", result[0].NodeID)
	}
}

func TestIdleScore_FreshSample(t *testing.T) {
	node := makeNode("A", 4)
	node.LoadSampledAt = time.Now()
	node.CPUUtilPct = 25.0
	if got := idleScore(node); got != 0.75 {
		t.Errorf("fresh 25%% CPU: got %v, want 0.75", got)
	}
	node.CPUUtilPct = 100.0
	if got := idleScore(node); got != 0.0 {
		t.Errorf("fresh 100%% CPU: got %v, want 0.0", got)
	}
	node.CPUUtilPct = 150.0 // out-of-range input clamps
	if got := idleScore(node); got != 0.0 {
		t.Errorf("fresh 150%% CPU: got %v, want 0.0 (clamped)", got)
	}
	node.CPUUtilPct = -5.0
	if got := idleScore(node); got != 1.0 {
		t.Errorf("fresh -5%% CPU: got %v, want 1.0 (clamped)", got)
	}
}

func TestIdleScore_AbsentOrStaleSampleScoresZero(t *testing.T) {
	// The invariant: silence never outranks an honest busy reporter.
	absent := makeNode("A", 4) // LoadSampledAt zero value
	if got := idleScore(absent); got != 0.0 {
		t.Errorf("absent sample: got %v, want 0.0", got)
	}
	stale := makeNode("A", 4)
	stale.LoadSampledAt = time.Now().Add(-4 * time.Minute) // older than 3×60s TTL
	stale.CPUUtilPct = 0.0                                 // claims fully idle — but stale
	if got := idleScore(stale); got != 0.0 {
		t.Errorf("stale sample: got %v, want 0.0", got)
	}
}

func TestIdleScore_OwnerActiveZeroes(t *testing.T) {
	node := makeNode("A", 4)
	node.LoadSampledAt = time.Now()
	node.CPUUtilPct = 0.0
	node.OwnerActive = true
	if got := idleScore(node); got != 0.0 {
		t.Errorf("owner active: got %v, want 0.0", got)
	}
}

func TestSchedule_IdleBeatsBusyAmongEquals(t *testing.T) {
	idle := makeNode("A", 4)
	idle.NodeID = "idle"
	idle.LoadSampledAt = time.Now()
	idle.CPUUtilPct = 5.0

	busy := makeNode("A", 4)
	busy.NodeID = "busy"
	busy.LoadSampledAt = time.Now()
	busy.CPUUtilPct = 95.0

	result, err := Schedule([]orchestrator.NodeEntry{busy, idle}, orchestrator.SLAStandard, orchestrator.PlacementContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeID != "idle" {
		t.Errorf("idle node should rank first among equals, got %q", result[0].NodeID)
	}
}

func TestSchedule_StaleIdleClaimLosesToHonestBusyWithBetterTerms(t *testing.T) {
	// A node that went silent (stale sample claiming 0%% CPU) scores 0 on the
	// idle term; an honest busy reporter also scores 0 there — so the honest
	// node's better legacy terms (class) decide, and silence never wins on
	// idleness alone.
	silent := makeNode("B", 4)
	silent.NodeID = "silent"
	silent.LoadSampledAt = time.Now().Add(-10 * time.Minute)
	silent.CPUUtilPct = 0.0

	honestBusy := makeNode("A", 4)
	honestBusy.NodeID = "honest-busy"
	honestBusy.LoadSampledAt = time.Now()
	honestBusy.CPUUtilPct = 100.0

	result, err := Schedule([]orchestrator.NodeEntry{silent, honestBusy}, orchestrator.SLAStandard, orchestrator.PlacementContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeID != "honest-busy" {
		t.Errorf("honest busy class-A should beat silent stale class-B, got %q", result[0].NodeID)
	}
}

func TestSchedule_InFlightPenaltyRanksBelowEqualIdleNode(t *testing.T) {
	loaded := makeNode("A", 4)
	loaded.NodeID = "loaded"
	loaded.InFlight = 3

	free := makeNode("A", 4)
	free.NodeID = "free"

	result, err := Schedule([]orchestrator.NodeEntry{loaded, free}, orchestrator.SLAStandard, orchestrator.PlacementContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].NodeID != "free" {
		t.Errorf("node with in-flight placements should rank below an equal free node, got %q", result[0].NodeID)
	}
}

func TestSchedule_EmptyContextReproducesLegacyOrdering(t *testing.T) {
	// With an empty PlacementContext and zero load state, the new terms all
	// contribute 0 and the legacy class > freshness > capacity ordering holds.
	a := makeNode("A", 4)
	a.NodeID = "a"
	b := makeNode("B", 8)
	b.NodeID = "b"
	c := makeNode("C", 8)
	c.NodeID = "c"

	result, err := Schedule([]orchestrator.NodeEntry{c, b, a}, orchestrator.SLAPremium, orchestrator.PlacementContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if result[i].NodeID != id {
			t.Errorf("rank %d: got %q, want %q", i, result[i].NodeID, id)
		}
	}
}

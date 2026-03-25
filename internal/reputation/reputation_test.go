package reputation

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ComputeScore tests
// ---------------------------------------------------------------------------

func TestComputeScore(t *testing.T) {
	tests := []struct {
		name           string
		entry          ReputationEntry
		wantScore      int
	}{
		{
			name: "perfect metrics yields 100",
			entry: ReputationEntry{
				FailureRate:    0.0,
				SettlementRate: 1.0,
				AccuracyRate:   1.0,
			},
			wantScore: 100,
		},
		{
			name: "worst metrics yields 10 (history bonus only)",
			entry: ReputationEntry{
				FailureRate:    1.0,
				SettlementRate: 0.0,
				AccuracyRate:   0.0,
			},
			wantScore: 10,
		},
		{
			name: "50% across the board",
			entry: ReputationEntry{
				FailureRate:    0.5,
				SettlementRate: 0.5,
				AccuracyRate:   0.5,
			},
			wantScore: 55,
		},
		{
			name: "zero failure, zero settlement, full accuracy",
			entry: ReputationEntry{
				FailureRate:    0.0,
				SettlementRate: 0.0,
				AccuracyRate:   1.0,
			},
			wantScore: 60,
		},
		{
			name: "high failure drags score down",
			entry: ReputationEntry{
				FailureRate:    0.9,
				SettlementRate: 1.0,
				AccuracyRate:   1.0,
			},
			wantScore: 73,
		},
		{
			name: "neutral metrics",
			entry: ReputationEntry{
				FailureRate:    0.0,
				SettlementRate: 0.5,
				AccuracyRate:   0.5,
			},
			wantScore: 70, // completion=30 + settlement=20 + accuracy=10 + bonus=10
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.entry.ComputeScore()
			if got != tc.wantScore {
				t.Errorf("ComputeScore() = %d, want %d", got, tc.wantScore)
			}
		})
	}
}

func TestComputeScore_ClampedToRange(t *testing.T) {
	// Even with extreme values the score should stay in [0, 100]
	entry := ReputationEntry{
		FailureRate:    -1.0, // invalid but should not panic
		SettlementRate: 2.0,
		AccuracyRate:   2.0,
	}
	got := entry.ComputeScore()
	if got < 0 || got > 100 {
		t.Errorf("ComputeScore() = %d, want [0, 100]", got)
	}
}

// ---------------------------------------------------------------------------
// ComputeHash tests
// ---------------------------------------------------------------------------

func TestComputeHash_Deterministic(t *testing.T) {
	period := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := &ReputationEntry{
		NodeDID:        "did:soho:node1",
		Period:         period,
		JobsCompleted:  10,
		FailureRate:    0.1,
		SettlementRate: 0.9,
		PreviousHash:   "genesis",
	}
	h1 := entry.ComputeHash()
	h2 := entry.ComputeHash()
	if h1 != h2 {
		t.Fatalf("ComputeHash not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 { // SHA-256 hex
		t.Fatalf("expected 64-char hex hash, got len %d", len(h1))
	}
}

func TestComputeHash_DifferentInputs(t *testing.T) {
	period := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e1 := &ReputationEntry{NodeDID: "did:soho:a", Period: period, PreviousHash: "genesis"}
	e2 := &ReputationEntry{NodeDID: "did:soho:b", Period: period, PreviousHash: "genesis"}
	if e1.ComputeHash() == e2.ComputeHash() {
		t.Fatal("different inputs should produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// NewReputationLedger / AddEntry / GetLatestScore
// ---------------------------------------------------------------------------

func TestNewReputationLedger(t *testing.T) {
	rl := NewReputationLedger()
	if rl == nil {
		t.Fatal("NewReputationLedger returned nil")
	}
	// Unknown node should return neutral score
	if score := rl.GetLatestScore("did:soho:unknown"); score != 50 {
		t.Errorf("expected neutral score 50, got %d", score)
	}
}

func TestAddEntry_GenesisRequired(t *testing.T) {
	rl := NewReputationLedger()
	entry := &ReputationEntry{
		NodeDID:      "did:soho:node1",
		PreviousHash: "not-genesis",
	}
	if err := rl.AddEntry(entry); err == nil {
		t.Fatal("expected error for non-genesis first entry")
	}
}

func TestAddEntry_EmptyNodeDID(t *testing.T) {
	rl := NewReputationLedger()
	entry := &ReputationEntry{PreviousHash: "genesis"}
	if err := rl.AddEntry(entry); err == nil {
		t.Fatal("expected error for empty NodeDID")
	}
}

func TestAddEntry_ValidGenesisEntry(t *testing.T) {
	rl := NewReputationLedger()
	entry := &ReputationEntry{
		NodeDID:        "did:soho:node1",
		Period:         time.Now(),
		FailureRate:    0.0,
		SettlementRate: 1.0,
		AccuracyRate:   0.8,
		PreviousHash:   "genesis",
		Source:         "test",
	}
	if err := rl.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	if entry.EntryHash == "" {
		t.Fatal("EntryHash should have been computed")
	}
	if entry.ReputationScore == 0 {
		t.Fatal("ReputationScore should have been computed")
	}
}

func TestAddEntry_ChainLinking(t *testing.T) {
	rl := NewReputationLedger()
	nodeDID := "did:soho:node1"
	period := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// First entry
	e1 := &ReputationEntry{
		NodeDID:        nodeDID,
		Period:         period,
		SettlementRate: 1.0,
		AccuracyRate:   1.0,
		PreviousHash:   "genesis",
		Source:         "test",
	}
	if err := rl.AddEntry(e1); err != nil {
		t.Fatalf("first entry: %v", err)
	}

	// Second entry must link to first
	e2 := &ReputationEntry{
		NodeDID:        nodeDID,
		Period:         period.Add(7 * 24 * time.Hour),
		SettlementRate: 0.9,
		AccuracyRate:   0.8,
		PreviousHash:   e1.EntryHash,
		Source:         "test",
	}
	if err := rl.AddEntry(e2); err != nil {
		t.Fatalf("second entry: %v", err)
	}

	// Wrong link should fail
	e3 := &ReputationEntry{
		NodeDID:        nodeDID,
		Period:         period.Add(14 * 24 * time.Hour),
		PreviousHash:   "wrong-hash",
		Source:         "test",
	}
	if err := rl.AddEntry(e3); err == nil {
		t.Fatal("expected error for broken chain link")
	}
}

func TestAddEntry_HashMismatch(t *testing.T) {
	rl := NewReputationLedger()
	entry := &ReputationEntry{
		NodeDID:      "did:soho:node1",
		Period:       time.Now(),
		PreviousHash: "genesis",
		EntryHash:    "deliberately-wrong-hash",
	}
	if err := rl.AddEntry(entry); err == nil {
		t.Fatal("expected error for hash mismatch")
	}
}

// ---------------------------------------------------------------------------
// VerifyChain
// ---------------------------------------------------------------------------

func TestVerifyChain_ValidChain(t *testing.T) {
	rl := NewReputationLedger()
	nodeDID := "did:soho:node1"

	e1 := &ReputationEntry{
		NodeDID:        nodeDID,
		Period:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		SettlementRate: 1.0,
		AccuracyRate:   1.0,
		PreviousHash:   "genesis",
	}
	if err := rl.AddEntry(e1); err != nil {
		t.Fatal(err)
	}
	e2 := &ReputationEntry{
		NodeDID:        nodeDID,
		Period:         time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC),
		SettlementRate: 0.9,
		AccuracyRate:   0.8,
		PreviousHash:   e1.EntryHash,
	}
	if err := rl.AddEntry(e2); err != nil {
		t.Fatal(err)
	}

	if err := rl.VerifyChain(nodeDID); err != nil {
		t.Fatalf("VerifyChain failed on valid chain: %v", err)
	}
}

func TestVerifyChain_UnknownNode(t *testing.T) {
	rl := NewReputationLedger()
	if err := rl.VerifyChain("did:soho:nonexistent"); err == nil {
		t.Fatal("expected error for unknown node")
	}
}

// ---------------------------------------------------------------------------
// GetHistory / GetAllProviders / GetStats
// ---------------------------------------------------------------------------

func TestGetHistory_Empty(t *testing.T) {
	rl := NewReputationLedger()
	history := rl.GetHistory("did:soho:unknown")
	if history != nil {
		t.Fatalf("expected nil history, got %d entries", len(history))
	}
}

func TestGetHistory_ReturnsCopy(t *testing.T) {
	rl := NewReputationLedger()
	nodeDID := "did:soho:node1"
	e1 := &ReputationEntry{
		NodeDID: nodeDID, Period: time.Now(), PreviousHash: "genesis",
		SettlementRate: 1.0, AccuracyRate: 1.0,
	}
	if err := rl.AddEntry(e1); err != nil {
		t.Fatal(err)
	}
	h1 := rl.GetHistory(nodeDID)
	h2 := rl.GetHistory(nodeDID)
	// Mutating h1 should not affect h2
	h1[0] = nil
	if h2[0] == nil {
		t.Fatal("GetHistory should return independent copies")
	}
}

func TestGetAllProviders(t *testing.T) {
	rl := NewReputationLedger()
	for _, did := range []string{"did:soho:a", "did:soho:b"} {
		e := &ReputationEntry{
			NodeDID: did, Period: time.Now(), PreviousHash: "genesis",
			SettlementRate: 1.0, AccuracyRate: 1.0,
		}
		if err := rl.AddEntry(e); err != nil {
			t.Fatal(err)
		}
	}
	providers := rl.GetAllProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
}

func TestGetStats_NoHistory(t *testing.T) {
	rl := NewReputationLedger()
	stats := rl.GetStats("did:soho:unknown")
	if stats.CurrentScore != 50 {
		t.Errorf("expected neutral score 50, got %d", stats.CurrentScore)
	}
	if stats.EntryCount != 0 {
		t.Errorf("expected 0 entries, got %d", stats.EntryCount)
	}
}

func TestGetStats_WithHistory(t *testing.T) {
	rl := NewReputationLedger()
	nodeDID := "did:soho:node1"
	e := &ReputationEntry{
		NodeDID: nodeDID, Period: time.Now(), PreviousHash: "genesis",
		JobsCompleted: 10, JobsAttempted: 12,
		FailureRate: 0.1, SettlementRate: 0.9, AccuracyRate: 0.8,
	}
	if err := rl.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	stats := rl.GetStats(nodeDID)
	if stats.EntryCount != 1 {
		t.Errorf("expected 1 entry, got %d", stats.EntryCount)
	}
	if stats.TotalJobs != 10 {
		t.Errorf("expected 10 total jobs, got %d", stats.TotalJobs)
	}
}

// ---------------------------------------------------------------------------
// ReputationManager: RecordJobOutcome / FinalizeWeek
// ---------------------------------------------------------------------------

func TestRecordJobOutcome_AccumulatesMetrics(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	rm.nowFunc = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }

	rm.RecordJobOutcome("did:soho:node1", true, true, 5.0)
	rm.RecordJobOutcome("did:soho:node1", true, false, 3.0)
	rm.RecordJobOutcome("did:soho:node1", false, true, 4.0)

	rm.mu.RLock()
	m := rm.metrics["did:soho:node1"]
	rm.mu.RUnlock()

	if m.JobsAttempted != 3 {
		t.Errorf("expected 3 attempted, got %d", m.JobsAttempted)
	}
	if m.SettlementCount != 2 {
		t.Errorf("expected 2 settlements, got %d", m.SettlementCount)
	}
	if m.CancelCount != 1 {
		t.Errorf("expected 1 cancel, got %d", m.CancelCount)
	}
	if m.AccurateCount != 2 {
		t.Errorf("expected 2 accurate, got %d", m.AccurateCount)
	}
}

func TestFinalizeWeek_CreatesEntries(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	rm.nowFunc = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }

	rm.RecordJobOutcome("did:soho:node1", true, true, 5.0)
	rm.RecordJobOutcome("did:soho:node1", true, true, 3.0)

	if err := rm.FinalizeWeek(); err != nil {
		t.Fatalf("FinalizeWeek: %v", err)
	}

	score := rl.GetLatestScore("did:soho:node1")
	if score == 50 {
		t.Fatal("score should have changed from neutral after finalization")
	}

	history := rl.GetHistory("did:soho:node1")
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Source != "payment_telemetry" {
		t.Errorf("expected source payment_telemetry, got %s", history[0].Source)
	}
}

func TestFinalizeWeek_ResetsMetrics(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	rm.nowFunc = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }

	rm.RecordJobOutcome("did:soho:node1", true, true, 5.0)
	if err := rm.FinalizeWeek(); err != nil {
		t.Fatal(err)
	}

	rm.mu.RLock()
	_, exists := rm.metrics["did:soho:node1"]
	rm.mu.RUnlock()
	if exists {
		t.Fatal("metrics should be reset after FinalizeWeek")
	}
}

func TestFinalizeWeek_MultipleWeeks(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	rm.nowFunc = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }

	// Week 1
	rm.RecordJobOutcome("did:soho:node1", true, true, 5.0)
	if err := rm.FinalizeWeek(); err != nil {
		t.Fatal(err)
	}

	// Week 2
	rm.RecordJobOutcome("did:soho:node1", true, true, 3.0)
	if err := rm.FinalizeWeek(); err != nil {
		t.Fatal(err)
	}

	history := rl.GetHistory("did:soho:node1")
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}

	// Verify chain integrity
	if err := rl.VerifyChain("did:soho:node1"); err != nil {
		t.Fatalf("chain verification failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ComputeDynamicPrice / GetPricingMultiplier
// ---------------------------------------------------------------------------

func TestComputeDynamicPrice(t *testing.T) {
	tests := []struct {
		name      string
		score     int // we'll set up a node with this score
		basePrice int64
		wantMin   int64
		wantMax   int64
	}{
		{"neutral score=50", 50, 100, 100, 100},
		{"excellent score near 100", 100, 100, 140, 160},
		{"poor score near 0", 10, 100, 50, 70},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := NewReputationLedger()
			rm := NewReputationManager(rl, 100)

			// Inject an entry with the desired score
			entry := &ReputationEntry{
				NodeDID:         "did:soho:node1",
				Period:          time.Now(),
				PreviousHash:    "genesis",
				ReputationScore: tc.score,
			}
			entry.EntryHash = entry.ComputeHash()
			if err := rl.AddEntry(entry); err != nil {
				t.Fatal(err)
			}

			price := rm.ComputeDynamicPrice("did:soho:node1", tc.basePrice)
			if price < tc.wantMin || price > tc.wantMax {
				t.Errorf("price=%d, want [%d, %d]", price, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestComputeDynamicPrice_UnknownNode(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	// Unknown node has score 50 -> multiplier 1.0
	price := rm.ComputeDynamicPrice("did:soho:unknown", 100)
	if price != 100 {
		t.Errorf("expected 100 for unknown node, got %d", price)
	}
}

func TestGetPricingMultiplier(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	// Unknown node -> score 50 -> multiplier 1.0
	mult := rm.GetPricingMultiplier("did:soho:unknown")
	if mult != 1.0 {
		t.Errorf("expected multiplier 1.0, got %f", mult)
	}
}

// ---------------------------------------------------------------------------
// GetReputationMultiplier (standalone function)
// ---------------------------------------------------------------------------

func TestGetReputationMultiplier(t *testing.T) {
	tests := []struct {
		score int
		want  float64
	}{
		{100, 1.5},
		{50, 1.0},
		{0, 0.5},
		{75, 1.25},
	}
	for _, tc := range tests {
		got := GetReputationMultiplier(tc.score)
		if got != tc.want {
			t.Errorf("GetReputationMultiplier(%d) = %f, want %f", tc.score, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// FilterByReputationThreshold
// ---------------------------------------------------------------------------

func TestFilterByReputationThreshold(t *testing.T) {
	rl := NewReputationLedger()

	// Add a high-score node
	high := &ReputationEntry{
		NodeDID: "did:soho:good", Period: time.Now(), PreviousHash: "genesis",
		SettlementRate: 1.0, AccuracyRate: 1.0, ReputationScore: 90,
	}
	high.EntryHash = high.ComputeHash()
	if err := rl.AddEntry(high); err != nil {
		t.Fatal(err)
	}

	// Add a low-score node
	low := &ReputationEntry{
		NodeDID: "did:soho:bad", Period: time.Now(), PreviousHash: "genesis",
		SettlementRate: 0.1, AccuracyRate: 0.1, ReputationScore: 20,
	}
	low.EntryHash = low.ComputeHash()
	if err := rl.AddEntry(low); err != nil {
		t.Fatal(err)
	}

	nodes := map[string]interface{}{
		"did:soho:good":    struct{}{},
		"did:soho:bad":     struct{}{},
		"did:soho:unknown": struct{}{}, // score 50
	}

	filtered := FilterByReputationThreshold(rl, nodes, 50)
	if _, ok := filtered["did:soho:good"]; !ok {
		t.Error("good node should pass threshold 50")
	}
	if _, ok := filtered["did:soho:bad"]; ok {
		t.Error("bad node (score 20) should not pass threshold 50")
	}
	if _, ok := filtered["did:soho:unknown"]; !ok {
		t.Error("unknown node (score 50) should pass threshold 50")
	}
}

func TestFilterByReputationThreshold_EmptyNodes(t *testing.T) {
	rl := NewReputationLedger()
	nodes := map[string]interface{}{}
	filtered := FilterByReputationThreshold(rl, nodes, 50)
	if len(filtered) != 0 {
		t.Error("expected empty result for empty input")
	}
}

// ---------------------------------------------------------------------------
// SetMaxPricingBonus
// ---------------------------------------------------------------------------

func TestSetMaxPricingBonus_Valid(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	rm.SetMaxPricingBonus(0.25)
	if rm.maxPricingBonus != 0.25 {
		t.Errorf("expected 0.25, got %f", rm.maxPricingBonus)
	}
}

func TestSetMaxPricingBonus_Invalid(t *testing.T) {
	rl := NewReputationLedger()
	rm := NewReputationManager(rl, 100)
	original := rm.maxPricingBonus
	rm.SetMaxPricingBonus(-0.5) // invalid
	if rm.maxPricingBonus != original {
		t.Error("invalid bonus should be ignored")
	}
	rm.SetMaxPricingBonus(1.5) // invalid
	if rm.maxPricingBonus != original {
		t.Error("invalid bonus > 1.0 should be ignored")
	}
}

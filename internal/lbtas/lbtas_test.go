package lbtas

import (
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// ─── CalculateWeight ─────────────────────────────────────────────────────────

func TestCalculateWeight_LowTransactions(t *testing.T) {
	tests := []struct {
		total int
		want  float64
	}{
		{0, 0.3},
		{1, 0.3},
		{9, 0.3},
		{10, 0.1},
		{25, 0.1},
		{49, 0.1},
		{50, 0.05},
		{100, 0.05},
		{1000, 0.05},
	}
	for _, tc := range tests {
		got := CalculateWeight(tc.total)
		if got != tc.want {
			t.Errorf("CalculateWeight(%d) = %v, want %v", tc.total, got, tc.want)
		}
	}
}

// ─── WeightedAverage ─────────────────────────────────────────────────────────

func TestWeightedAverage(t *testing.T) {
	tests := []struct {
		old, new, weight, want float64
	}{
		{4.0, 5.0, 0.3, 4.0*0.7 + 5.0*0.3},
		{0.0, 5.0, 1.0, 5.0},
		{5.0, 0.0, 1.0, 0.0},
		{3.0, 3.0, 0.5, 3.0},
		{0.0, 0.0, 0.3, 0.0},
	}
	for _, tc := range tests {
		got := WeightedAverage(tc.old, tc.new, tc.weight)
		if diff := got - tc.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("WeightedAverage(%v, %v, %v) = %v, want %v",
				tc.old, tc.new, tc.weight, got, tc.want)
		}
	}
}

// ─── CalculateOverallScore ───────────────────────────────────────────────────

func TestCalculateOverallScore_Perfect(t *testing.T) {
	score := &LBTASScore{
		PaymentReliability: 5.0,
		ExecutionQuality:   5.0,
		Communication:      5.0,
		ResourceUsage:      5.0,
	}
	got := CalculateOverallScore(score)
	if got != 100 {
		t.Errorf("perfect scores: CalculateOverallScore = %d, want 100", got)
	}
}

func TestCalculateOverallScore_Zero(t *testing.T) {
	score := &LBTASScore{}
	got := CalculateOverallScore(score)
	if got != 0 {
		t.Errorf("zero scores: CalculateOverallScore = %d, want 0", got)
	}
}

func TestCalculateOverallScore_WithDisputes(t *testing.T) {
	score := &LBTASScore{
		PaymentReliability:   5.0,
		ExecutionQuality:     5.0,
		Communication:        5.0,
		ResourceUsage:        5.0,
		TotalTransactions:    10,
		DisputedTransactions: 5, // 50% dispute ratio -> 10 point penalty
	}
	got := CalculateOverallScore(score)
	// weighted = 100, penalty = 0.5 * 20 = 10, overall = 90
	if got != 90 {
		t.Errorf("50%% disputes: CalculateOverallScore = %d, want 90", got)
	}
}

func TestCalculateOverallScore_AllDisputed(t *testing.T) {
	score := &LBTASScore{
		PaymentReliability:   5.0,
		ExecutionQuality:     5.0,
		Communication:        5.0,
		ResourceUsage:        5.0,
		TotalTransactions:    10,
		DisputedTransactions: 10, // 100% dispute ratio -> 20 point penalty
	}
	got := CalculateOverallScore(score)
	if got != 80 {
		t.Errorf("all disputed: CalculateOverallScore = %d, want 80", got)
	}
}

func TestCalculateOverallScore_ClampsToZero(t *testing.T) {
	score := &LBTASScore{
		PaymentReliability:   0.5,
		ExecutionQuality:     0.5,
		Communication:        0.5,
		ResourceUsage:        0.5,
		TotalTransactions:    10,
		DisputedTransactions: 10, // max penalty
	}
	got := CalculateOverallScore(score)
	if got < 0 {
		t.Errorf("score should not be negative, got %d", got)
	}
}

// ─── UpdateScoreFromRating ───────────────────────────────────────────────────

func TestUpdateScoreFromRating_PaymentReliability(t *testing.T) {
	score := &LBTASScore{
		DID:                "did:example:user1",
		PaymentReliability: 3.0,
		TotalTransactions:  0, // weight = 0.3
	}
	rating := LBTASRating{
		Score:    5,
		Category: "payment_reliability",
	}
	UpdateScoreFromRating(score, rating)

	expected := WeightedAverage(3.0, 5.0, 0.3)
	if diff := score.PaymentReliability - expected; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("PaymentReliability = %v, want %v", score.PaymentReliability, expected)
	}
	if score.TotalTransactions != 1 {
		t.Errorf("TotalTransactions = %d, want 1", score.TotalTransactions)
	}
	if score.CompletedTransactions != 1 {
		t.Errorf("CompletedTransactions = %d, want 1", score.CompletedTransactions)
	}
	if len(score.ScoreHistory) != 1 {
		t.Errorf("ScoreHistory length = %d, want 1", len(score.ScoreHistory))
	}
}

func TestUpdateScoreFromRating_AutoResolved(t *testing.T) {
	score := &LBTASScore{
		PaymentReliability: 3.0,
		ExecutionQuality:   3.0,
		Communication:      3.0,
		ResourceUsage:      3.0,
	}
	rating := LBTASRating{Score: 5, Category: "auto_resolved"}
	UpdateScoreFromRating(score, rating)

	// auto_resolved uses weight*0.5 across all categories
	weight := CalculateWeight(0) * 0.5 // 0.3 * 0.5 = 0.15
	expected := WeightedAverage(3.0, 5.0, weight)
	if diff := score.PaymentReliability - expected; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("auto_resolved PaymentReliability = %v, want %v", score.PaymentReliability, expected)
	}
}

func TestUpdateScoreFromRating_HistoryTruncation(t *testing.T) {
	score := &LBTASScore{
		ScoreHistory: make([]ScoreSnapshot, 100),
	}
	rating := LBTASRating{Score: 4, Category: "communication"}
	UpdateScoreFromRating(score, rating)

	if len(score.ScoreHistory) != 100 {
		t.Errorf("ScoreHistory should stay at 100 after truncation, got %d", len(score.ScoreHistory))
	}
}

// ─── ApplyPenalty ────────────────────────────────────────────────────────────

func TestApplyPenalty(t *testing.T) {
	score := &LBTASScore{OverallScore: 80}
	ApplyPenalty(score, 15)
	if score.OverallScore != 65 {
		t.Errorf("after penalty of 15: OverallScore = %d, want 65", score.OverallScore)
	}
}

func TestApplyPenalty_ClampsToZero(t *testing.T) {
	score := &LBTASScore{OverallScore: 10}
	ApplyPenalty(score, 20)
	if score.OverallScore != 0 {
		t.Errorf("penalty clamped: OverallScore = %d, want 0", score.OverallScore)
	}
}

func TestApplyPenalty_UpdatesTimestamp(t *testing.T) {
	before := time.Now().Add(-time.Second)
	score := &LBTASScore{OverallScore: 50}
	ApplyPenalty(score, 5)
	if score.UpdatedAt.Before(before) {
		t.Error("ApplyPenalty should update UpdatedAt")
	}
}

// ─── ValidateRating ──────────────────────────────────────────────────────────

func TestValidateRating_Valid(t *testing.T) {
	tests := []LBTASRating{
		{Score: 0, Category: "payment_reliability"},
		{Score: 5, Category: "execution_quality"},
		{Score: 3, Category: "communication", Feedback: "good"},
		{Score: 1, Category: "auto_resolved"},
	}
	for _, r := range tests {
		if err := ValidateRating(r); err != nil {
			t.Errorf("ValidateRating(%v) unexpected error: %v", r, err)
		}
	}
}

func TestValidateRating_InvalidScore(t *testing.T) {
	tests := []int{-1, 6, 100, -100}
	for _, s := range tests {
		r := LBTASRating{Score: s, Category: "payment_reliability"}
		if err := ValidateRating(r); err == nil {
			t.Errorf("ValidateRating(score=%d) should fail", s)
		}
	}
}

func TestValidateRating_InvalidCategory(t *testing.T) {
	r := LBTASRating{Score: 3, Category: "nonexistent_category"}
	if err := ValidateRating(r); err == nil {
		t.Error("ValidateRating with invalid category should fail")
	}
}

func TestValidateRating_FeedbackTooLong(t *testing.T) {
	r := LBTASRating{
		Score:    3,
		Category: "communication",
		Feedback: string(make([]byte, 501)),
	}
	if err := ValidateRating(r); err == nil {
		t.Error("ValidateRating with >500 char feedback should fail")
	}
}

func TestValidateRating_FeedbackAtLimit(t *testing.T) {
	r := LBTASRating{
		Score:    3,
		Category: "communication",
		Feedback: string(make([]byte, 500)),
	}
	if err := ValidateRating(r); err != nil {
		t.Errorf("ValidateRating with exactly 500 char feedback should pass: %v", err)
	}
}

// ─── CanFileDispute ──────────────────────────────────────────────────────────

func TestCanFileDispute_NilRatings(t *testing.T) {
	tx := &ResourceTransaction{}
	if CanFileDispute(tx) {
		t.Error("CanFileDispute should be false when ratings are nil")
	}
}

func TestCanFileDispute_OneNilRating(t *testing.T) {
	tx := &ResourceTransaction{
		ProviderRating: &LBTASRating{Score: 5},
	}
	if CanFileDispute(tx) {
		t.Error("CanFileDispute should be false when one rating is nil")
	}
}

func TestCanFileDispute_SmallDivergence(t *testing.T) {
	tx := &ResourceTransaction{
		ProviderRating: &LBTASRating{Score: 4},
		UserRating:     &LBTASRating{Score: 3},
	}
	if CanFileDispute(tx) {
		t.Error("CanFileDispute should be false for divergence of 1")
	}
}

func TestCanFileDispute_ExactThreshold(t *testing.T) {
	tx := &ResourceTransaction{
		ProviderRating: &LBTASRating{Score: 5},
		UserRating:     &LBTASRating{Score: 2},
	}
	if !CanFileDispute(tx) {
		t.Error("CanFileDispute should be true for divergence of 3")
	}
}

func TestCanFileDispute_LargeDivergence(t *testing.T) {
	tx := &ResourceTransaction{
		ProviderRating: &LBTASRating{Score: 0},
		UserRating:     &LBTASRating{Score: 5},
	}
	if !CanFileDispute(tx) {
		t.Error("CanFileDispute should be true for divergence of 5")
	}
}

// ─── TransactionFromRow / TransactionToRow ───────────────────────────────────

func TestTransactionFromRow_Nil(t *testing.T) {
	if got := TransactionFromRow(nil); got != nil {
		t.Error("TransactionFromRow(nil) should return nil")
	}
}

func TestTransactionToRow_Nil(t *testing.T) {
	if got := TransactionToRow(nil); got != nil {
		t.Error("TransactionToRow(nil) should return nil")
	}
}

func TestTransactionFromRow_BasicFields(t *testing.T) {
	now := time.Now()
	row := &store.ResourceTransactionRow{
		TransactionID:   "tx-001",
		UserDID:         "did:user:1",
		ProviderDID:     "did:provider:1",
		ResourceType:    "compute",
		ResourceID:      "res-001",
		State:           "executing",
		PaymentAmount:   1000,
		PaymentCurrency: "sats",
		PaymentEscrowed: true,
		ResultsReady:    false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	tx := TransactionFromRow(row)
	if tx.TransactionID != "tx-001" {
		t.Errorf("TransactionID = %q, want %q", tx.TransactionID, "tx-001")
	}
	if tx.State != StateExecuting {
		t.Errorf("State = %q, want %q", tx.State, StateExecuting)
	}
	if tx.PaymentAmount != 1000 {
		t.Errorf("PaymentAmount = %d, want 1000", tx.PaymentAmount)
	}
	if !tx.PaymentEscrowed {
		t.Error("PaymentEscrowed should be true")
	}
}

func TestTransactionFromRow_WithBlockchainAnchor(t *testing.T) {
	blockNum := int64(42)
	hash := make([]byte, 32)
	hash[0] = 0xAB
	row := &store.ResourceTransactionRow{
		TransactionID:   "tx-002",
		State:           "completed",
		BlockchainBlock: &blockNum,
		BlockchainHash:  hash,
	}

	tx := TransactionFromRow(row)
	if tx.BlockchainAnchor == nil {
		t.Fatal("BlockchainAnchor should not be nil")
	}
	if tx.BlockchainAnchor.BlockHeight != 42 {
		t.Errorf("BlockHeight = %d, want 42", tx.BlockchainAnchor.BlockHeight)
	}
	if tx.BlockchainAnchor.DataHash[0] != 0xAB {
		t.Errorf("DataHash[0] = %x, want 0xAB", tx.BlockchainAnchor.DataHash[0])
	}
}

func TestTransactionToRow_WithBlockchainAnchor(t *testing.T) {
	tx := &ResourceTransaction{
		TransactionID: "tx-003",
		State:         StateCompleted,
		BlockchainAnchor: &BlockchainAnchor{
			BlockHeight: 99,
		},
	}
	tx.BlockchainAnchor.DataHash[0] = 0xCD

	row := TransactionToRow(tx)
	if row.BlockchainBlock == nil || *row.BlockchainBlock != 99 {
		t.Error("BlockchainBlock should be 99")
	}
	if len(row.BlockchainHash) < 1 || row.BlockchainHash[0] != 0xCD {
		t.Error("BlockchainHash[0] should be 0xCD")
	}
}

func TestTransactionRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := &ResourceTransaction{
		TransactionID:   "tx-rt",
		UserDID:         "did:user:rt",
		ProviderDID:     "did:provider:rt",
		ResourceType:    "storage",
		ResourceID:      "stor-001",
		State:           StateAwaitingUserRating,
		PaymentAmount:   500,
		PaymentCurrency: "sats",
		PaymentEscrowed: true,
		ResultsReady:    true,
		ResultsPath:     "/results/path",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	row := TransactionToRow(original)
	restored := TransactionFromRow(row)

	if restored.TransactionID != original.TransactionID {
		t.Errorf("round-trip TransactionID mismatch")
	}
	if restored.State != original.State {
		t.Errorf("round-trip State = %q, want %q", restored.State, original.State)
	}
	if restored.PaymentAmount != original.PaymentAmount {
		t.Errorf("round-trip PaymentAmount mismatch")
	}
}

// ─── ScoreFromRow / ScoreToRow ───────────────────────────────────────────────

func TestScoreFromRow_Nil(t *testing.T) {
	if got := ScoreFromRow(nil); got != nil {
		t.Error("ScoreFromRow(nil) should return nil")
	}
}

func TestScoreToRow_Nil(t *testing.T) {
	if got := ScoreToRow(nil); got != nil {
		t.Error("ScoreToRow(nil) should return nil")
	}
}

func TestScoreRoundTrip(t *testing.T) {
	original := &LBTASScore{
		DID:                   "did:test:score",
		OverallScore:          75,
		PaymentReliability:    4.2,
		ExecutionQuality:      3.8,
		Communication:         4.0,
		ResourceUsage:         3.5,
		TotalTransactions:     100,
		CompletedTransactions: 95,
		DisputedTransactions:  5,
		LastAnchorBlock:       42,
		ScoreHistory: []ScoreSnapshot{
			{Score: 70, Timestamp: time.Now().Truncate(time.Second)},
			{Score: 75, Timestamp: time.Now().Truncate(time.Second)},
		},
	}
	original.LastAnchorHash[0] = 0xFF

	row := ScoreToRow(original)
	restored := ScoreFromRow(row)

	if restored.DID != original.DID {
		t.Errorf("DID mismatch: %q vs %q", restored.DID, original.DID)
	}
	if restored.OverallScore != original.OverallScore {
		t.Errorf("OverallScore mismatch: %d vs %d", restored.OverallScore, original.OverallScore)
	}
	if restored.PaymentReliability != original.PaymentReliability {
		t.Errorf("PaymentReliability mismatch")
	}
	if restored.TotalTransactions != original.TotalTransactions {
		t.Errorf("TotalTransactions mismatch")
	}
	if len(restored.ScoreHistory) != 2 {
		t.Errorf("ScoreHistory length = %d, want 2", len(restored.ScoreHistory))
	}
	if restored.LastAnchorBlock != 42 {
		t.Errorf("LastAnchorBlock = %d, want 42", restored.LastAnchorBlock)
	}
	if restored.LastAnchorHash[0] != 0xFF {
		t.Errorf("LastAnchorHash[0] = %x, want 0xFF", restored.LastAnchorHash[0])
	}
}

func TestScoreToRow_NoAnchorBlock(t *testing.T) {
	score := &LBTASScore{
		DID:             "did:test:noanchor",
		LastAnchorBlock: 0, // zero means no anchor
	}
	row := ScoreToRow(score)
	if row.LastAnchorBlock != nil {
		t.Error("LastAnchorBlock should be nil when zero")
	}
}

// ─── LBTASRating.Bytes ──────────────────────────────────────────────────────

func TestLBTASRating_Bytes_Deterministic(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r := &LBTASRating{
		Score:     3,
		Category:  "payment_reliability",
		Feedback:  "good work",
		Timestamp: ts,
	}

	b1 := r.Bytes()
	b2 := r.Bytes()
	if len(b1) != len(b2) {
		t.Fatal("Bytes() not deterministic: length mismatch")
	}
	for i := range b1 {
		if b1[i] != b2[i] {
			t.Fatalf("Bytes() not deterministic at index %d", i)
		}
	}
}

func TestLBTASRating_Bytes_DifferentScores(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := &LBTASRating{Score: 1, Category: "payment_reliability", Timestamp: ts}
	r2 := &LBTASRating{Score: 2, Category: "payment_reliability", Timestamp: ts}

	b1 := r1.Bytes()
	b2 := r2.Bytes()
	if b1[0] == b2[0] {
		t.Error("different scores should produce different first byte")
	}
}

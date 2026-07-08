//go:build integration

package operator_test

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	protoop "github.com/NTARI-RAND/sohocloud-protocol/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// These tests exercise the Stage-4 read-model methods (readmodel.go) against the
// isolated test DB. They rely on the connectTestDB / onboardToActive helpers in
// repository_test.go (same package, same build tag). Every method under test is a
// pure read; the tests assert the shapes the console GET pages consume.

// TestListOperators_CountsAndRows verifies ListOperators partitions operators
// into the queue's status buckets and returns rows newest-first.
func TestListOperators_CountsAndRows(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	// Active operator (fully onboarded).
	onboardToActive(t, r, "cloudy", 100)

	// Pending operator (created, no gates).
	if err := r.CreateOperator(ctx, "pend", "Pending", "pend@example.com", ""); err != nil {
		t.Fatalf("create pending: %v", err)
	}

	// Conformance-passed but NOT activated. IMPORTANT (finding for steps 2-3): in
	// this build no write path sets onboarding_state='verified' — AutoActivate is
	// the only writer and it jumps straight to 'active'. So an operator that has
	// registered keys, verified email, and passed conformance but has NOT been
	// auto-activated is STILL in onboarding_state='pending_verification'. The read
	// model reports that faithfully: it lands in the Pending bucket, and
	// ConformancePassed is true even though the state is pending.
	if err := r.CreateOperator(ctx, "passed", "Passed", "passed@example.com", ""); err != nil {
		t.Fatalf("create passed: %v", err)
	}
	_, pubs := freshKeyset(t)
	if err := r.AddKeys(ctx, "passed", pubs, operator.AlgoEd25519, evenThresholds(100)); err != nil {
		t.Fatalf("AddKeys passed: %v", err)
	}
	if err := r.MarkEmailVerified(ctx, "passed"); err != nil {
		t.Fatalf("MarkEmailVerified passed: %v", err)
	}
	if err := r.MarkConformancePassed(ctx, "passed"); err != nil {
		t.Fatalf("MarkConformancePassed passed: %v", err)
	}

	// Revoked operator.
	onboardToActive(t, r, "gone", 100)
	if err := r.Revoke(ctx, "gone"); err != nil {
		t.Fatalf("Revoke gone: %v", err)
	}

	list, counts, err := r.ListOperators(ctx)
	if err != nil {
		t.Fatalf("ListOperators: %v", err)
	}
	if counts.Total != 4 {
		t.Fatalf("total: got %d, want 4", counts.Total)
	}
	if counts.Active != 1 {
		t.Errorf("active count: got %d, want 1", counts.Active)
	}
	if counts.Revoked != 1 {
		t.Errorf("revoked count: got %d, want 1", counts.Revoked)
	}
	// Both "pend" (nothing done) and "passed" (conformance passed but not
	// activated) sit in pending_verification — the build has no 'verified' state.
	if counts.Pending != 2 {
		t.Errorf("pending count: got %d, want 2", counts.Pending)
	}
	if counts.Verified != 0 {
		t.Errorf("verified count: got %d, want 0 (no write path sets 'verified')", counts.Verified)
	}
	if counts.VerifiedPassed != 0 {
		t.Errorf("verified_passed count: got %d, want 0", counts.VerifiedPassed)
	}
	if len(list) != 4 {
		t.Fatalf("rows: got %d, want 4", len(list))
	}

	byID := map[string]operator.OperatorSummary{}
	for _, s := range list {
		byID[s.ID] = s
	}
	active := byID["cloudy"]
	if active.ActiveKeyCount != operator.KeyIndexCount {
		t.Errorf("cloudy active keys: got %d, want %d", active.ActiveKeyCount, operator.KeyIndexCount)
	}
	if !active.EmailVerified {
		t.Error("cloudy should be email-verified")
	}
	passed := byID["passed"]
	if passed.OnboardingState != "pending_verification" {
		t.Errorf("passed operator state: got %q, want pending_verification", passed.OnboardingState)
	}
	if !passed.ConformancePassed || passed.ConformancePassedAt == nil {
		t.Error("passed operator should have conformance passed with a timestamp")
	}
	// ReadyToActivate requires onboarding_state=='verified', which never occurs in
	// this build; the read model reports it as false accordingly.
	if passed.ReadyToActivate {
		t.Error("passed operator is not ReadyToActivate (state never reaches 'verified')")
	}
	pend := byID["pend"]
	if pend.ReadyToActivate {
		t.Error("pending operator must not be ReadyToActivate")
	}
	if pend.ActiveKeyCount != 0 {
		t.Errorf("pending active keys: got %d, want 0", pend.ActiveKeyCount)
	}
}

// TestGetOperator_ActiveDashboard verifies GetOperator assembles keys, lifecycle,
// latest run, and the keyset-hash-match flag for an active operator.
func TestGetOperator_ActiveDashboard(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	onboardToActive(t, r, "cloudy", 100)

	d, err := r.GetOperator(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetOperator: %v", err)
	}
	if d.ID != "cloudy" || d.OnboardingState != "active" || d.Status != "active" {
		t.Fatalf("unexpected core: %+v", d)
	}
	if d.ActiveKeyCount != operator.KeyIndexCount {
		t.Errorf("active key count: got %d, want %d", d.ActiveKeyCount, operator.KeyIndexCount)
	}
	if len(d.Keys) != operator.KeyIndexCount {
		t.Fatalf("keys: got %d, want %d", len(d.Keys), operator.KeyIndexCount)
	}
	for _, k := range d.Keys {
		if k.State != "active" {
			t.Errorf("key %d state: got %q, want active", k.KeyIndex, k.State)
		}
		if k.PublicKeyB64Trunc == "" {
			t.Errorf("key %d has empty truncated pubkey", k.KeyIndex)
		}
		if k.ExpirationThreshold != 100 {
			t.Errorf("key %d threshold: got %d, want 100", k.KeyIndex, k.ExpirationThreshold)
		}
		if k.NearThreshold {
			t.Errorf("key %d should not be near threshold (usage 0, threshold 100)", k.KeyIndex)
		}
	}
	// Active operator that onboarded via MarkConformancePassed: lifecycle should
	// show created + email verified + conformance passed + activated, none revoked.
	if !d.Lifecycle.Activated {
		t.Error("lifecycle should be Activated")
	}
	if d.Lifecycle.Revoked {
		t.Error("lifecycle should not be Revoked")
	}
	if !d.Lifecycle.EmailVerified {
		t.Error("lifecycle should be EmailVerified")
	}
	if d.Lifecycle.ConformancePassed == nil {
		t.Error("lifecycle should have ConformancePassed timestamp")
	}
	// onboardToActive uses MarkConformancePassed (not a full run), so there is no
	// conformance_runs row; LatestRun should be nil.
	if d.LatestRun != nil {
		t.Errorf("expected no run for MarkConformancePassed path, got %+v", d.LatestRun)
	}
	// Keyset hash matches (conformance bound to the current 7 keys, still active).
	if !d.KeysetHashMatches {
		t.Error("keyset hash should match for a freshly-activated operator")
	}
	if d.TransmissionsLast24h != 0 {
		t.Errorf("transmissions/24h: got %d, want 0", d.TransmissionsLast24h)
	}
}

// TestGetOperator_NotFound verifies the not-found mapping.
func TestGetOperator_NotFound(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	if _, err := r.GetOperator(context.Background(), "ghost"); err != operator.ErrOperatorNotFound {
		t.Fatalf("GetOperator(ghost): got %v, want ErrOperatorNotFound", err)
	}
}

// TestGetOperator_NearThresholdBadge verifies that a key whose usage is within the
// near-threshold window flags NearThreshold. Threshold 3 with usage driven up to 2
// by RecordUsageAndExpire (2 <= 3 sits inside the window of 10) flags near.
func TestGetOperator_NearThresholdBadge(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	// Low threshold so a couple of uses lands inside the near-threshold window
	// without expiring below the MinActiveKeys floor.
	onboardToActive(t, r, "cloudy", 3)

	d, err := r.GetOperator(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetOperator: %v", err)
	}
	// Every active key has threshold 3; the near-window is 10, so usage 0 already
	// satisfies usage >= threshold-window (0 >= 3-10 = -7). All should flag near.
	nearCount := 0
	for _, k := range d.Keys {
		if k.NearThreshold {
			nearCount++
		}
	}
	if nearCount != operator.KeyIndexCount {
		t.Errorf("near-threshold keys: got %d, want %d", nearCount, operator.KeyIndexCount)
	}
}

// TestGetOperator_LatestRunSuiteVerdicts verifies GetOperator surfaces the latest
// conformance run and its per-suite verdicts after a full conformance run.
func TestGetOperator_LatestRunSuiteVerdicts(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	// Build an operator with keys + verified email, then run a real conformance
	// run to green using the same helper the conformance tests use.
	privs := onboardViaConformance(t, r, "cloudy", 100)
	_ = privs

	d, err := r.GetOperator(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetOperator: %v", err)
	}
	if d.LatestRun == nil {
		t.Fatal("expected a latest conformance run")
	}
	if d.LatestRun.Status != "passed" {
		t.Errorf("run status: got %q, want passed", d.LatestRun.Status)
	}
	if len(d.LatestRun.Suites) != 3 {
		t.Fatalf("suites: got %d, want 3 (A,B,C)", len(d.LatestRun.Suites))
	}
	seen := map[operator.ConformanceSuite]bool{}
	for _, sv := range d.LatestRun.Suites {
		seen[sv.Suite] = true
		if !sv.Graded || !sv.Passed {
			t.Errorf("suite %s: graded=%v passed=%v, want both true", sv.Suite, sv.Graded, sv.Passed)
		}
	}
	for _, want := range []operator.ConformanceSuite{operator.SuiteA, operator.SuiteB, operator.SuiteC} {
		if !seen[want] {
			t.Errorf("missing suite %s in verdicts", want)
		}
	}
}

// TestTransmissionsLast24h_CountsProductionNonces verifies the 24h transmission
// count reflects production-scope nonces accepted via the CAS path.
func TestTransmissionsLast24h_CountsProductionNonces(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	onboardToActive(t, r, "cloudy", 100)

	if n, err := r.TransmissionsLast24h(ctx, "cloudy"); err != nil || n != 0 {
		t.Fatalf("initial transmissions: got %d err %v, want 0 nil", n, err)
	}

	// Record two production transmissions via the anti-replay CAS (the same path
	// OperatorAuth drives): each inserts a production-scope nonce.
	future := time.Now().Add(10 * time.Minute)
	for i, nonce := range [][]byte{mkNonce(0x01), mkNonce(0x02)} {
		if err := r.CheckAndAdvance(ctx, "cloudy", "soholink", uint64(i+1), nonce, "production", future); err != nil {
			t.Fatalf("CheckAndAdvance %d: %v", i, err)
		}
	}
	// A conformance-scope nonce must NOT be counted.
	if err := r.CheckAndAdvance(ctx, "cloudy", "soholink", 99, mkNonce(0x03), "conformance", future); err != nil {
		t.Fatalf("CheckAndAdvance conformance: %v", err)
	}

	n, err := r.TransmissionsLast24h(ctx, "cloudy")
	if err != nil {
		t.Fatalf("TransmissionsLast24h: %v", err)
	}
	if n != 2 {
		t.Errorf("transmissions/24h: got %d, want 2 (production only)", n)
	}
}

// TestFeeReadModel_HistoryNextSeqCurrent verifies FeeHistory, NextSeq, and
// CurrentFeeDeclarationView against published declarations.
func TestFeeReadModel_HistoryNextSeqCurrent(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	const coord = "soholink"

	// No declarations yet.
	if seq, err := r.NextSeq(ctx, coord); err != nil || seq != 0 {
		t.Fatalf("NextSeq empty: got %d err %v, want 0 nil", seq, err)
	}
	if _, err := r.CurrentFeeDeclarationView(ctx, coord); err != operator.ErrNoFeeDeclaration {
		t.Fatalf("CurrentFeeDeclarationView empty: got %v, want ErrNoFeeDeclaration", err)
	}
	if h, err := r.FeeHistory(ctx, coord); err != nil || len(h) != 0 {
		t.Fatalf("FeeHistory empty: got %d err %v, want 0 nil", len(h), err)
	}

	base := time.Now().Truncate(time.Second)
	publish := func(seq uint64, contrib, plat int, eff time.Time) {
		decl := fees.FeeDeclaration{
			CoordinatorID: coord,
			Terms:         fees.Terms{ContributorShareBps: contrib, PlatformFeeBps: plat},
			EffectiveAt:   eff,
			Seq:           seq,
			Signature:     []byte("sig-placeholder-not-verified-by-repo"),
		}
		if err := r.PublishFeeDeclaration(ctx, decl); err != nil {
			t.Fatalf("PublishFeeDeclaration seq %d: %v", seq, err)
		}
	}
	publish(1, 6500, 3500, base.Add(1*time.Hour))
	publish(2, 7000, 3000, base.Add(2*time.Hour))

	if seq, err := r.NextSeq(ctx, coord); err != nil || seq != 3 {
		t.Fatalf("NextSeq after two: got %d err %v, want 3 nil", seq, err)
	}

	hist, err := r.FeeHistory(ctx, coord)
	if err != nil {
		t.Fatalf("FeeHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len: got %d, want 2", len(hist))
	}
	// Newest (seq 2) first, flagged current.
	if hist[0].Seq != 2 || !hist[0].IsCurrent {
		t.Errorf("history[0]: seq %d current %v, want 2 true", hist[0].Seq, hist[0].IsCurrent)
	}
	if hist[1].Seq != 1 || hist[1].IsCurrent {
		t.Errorf("history[1]: seq %d current %v, want 1 false", hist[1].Seq, hist[1].IsCurrent)
	}
	if hist[0].ContributorShareBps != 7000 || hist[0].PlatformFeeBps != 3000 {
		t.Errorf("history[0] terms: got %d/%d, want 7000/3000", hist[0].ContributorShareBps, hist[0].PlatformFeeBps)
	}

	cur, err := r.CurrentFeeDeclarationView(ctx, coord)
	if err != nil {
		t.Fatalf("CurrentFeeDeclarationView: %v", err)
	}
	if cur.Seq != 2 || !cur.IsCurrent || cur.ContributorShareBps != 7000 {
		t.Errorf("current view: %+v, want seq 2, current, contributor 7000", cur)
	}
}

// onboardViaConformance onboards an operator through a REAL conformance run (not
// the MarkConformancePassed shortcut) so a conformance_runs row with graded A/B/C
// suites exists for the read model to surface. It creates the operator, registers
// 7 keys, verifies email, starts a run, produces valid Suite A + Suite B
// responses (Suite C is server-side), finalizes, and auto-activates. Returns the
// private keys.
func onboardViaConformance(t *testing.T, r *operator.Repository, id string, threshold int) []ed25519.PrivateKey {
	t.Helper()
	ctx := context.Background()
	if err := r.CreateOperator(ctx, id, "Cloudy", id+"@example.com", ""); err != nil {
		t.Fatalf("CreateOperator: %v", err)
	}
	privs, pubs := freshKeyset(t)
	if err := r.AddKeys(ctx, id, pubs, operator.AlgoEd25519, evenThresholds(threshold)); err != nil {
		t.Fatalf("AddKeys: %v", err)
	}
	if err := r.MarkEmailVerified(ctx, id); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}

	runID, chA, chB, err := r.StartConformanceRun(ctx, id)
	if err != nil {
		t.Fatalf("StartConformanceRun: %v", err)
	}
	if len(chA) != 1 || len(chB) != 1 {
		t.Fatalf("expected 1 A + 1 B challenge, got %d/%d", len(chA), len(chB))
	}

	// Suite A: recompute the canonical bytes and sign a ConformanceResponse.
	a := chA[0]
	canonA := protoop.OperatorTransmission{
		OperatorID: a.OperatorID, TsUnixNano: a.TsUnixNano, Nonce: a.Nonce,
		Seq: a.Seq, Algo: a.Algo, Idx0: a.Idx0, Idx1: a.Idx1,
	}.CanonicalBytes()
	crA := protoop.ConformanceResponse{OperatorID: id, Challenge: canonA, Algo: a.Algo}
	crA.Sign(privs[a.Idx0], privs[a.Idx1], a.Idx0, a.Idx1)
	respA := operator.ResponseA{
		ChallengeID: a.ChallengeID, CanonicalBytes: canonA,
		Idx0: a.Idx0, Idx1: a.Idx1, Sig0: crA.Sig0, Sig1: crA.Sig1,
	}
	if res, err := r.GradeSuiteA(ctx, id, runID, respA); err != nil || !res.Passed {
		t.Fatalf("GradeSuiteA: res=%+v err=%v", res, err)
	}

	// Suite B: full transmission over the fresh fields, signed with 2 keys.
	b := chB[0]
	txB := protoop.OperatorTransmission{
		OperatorID: id, TsUnixNano: b.TsUnixNano, Nonce: b.Nonce, Seq: b.Seq, Algo: b.Algo,
	}
	txB.Sign(privs[2], privs[5], 2, 5)
	respB := operator.ResponseB{
		ChallengeID: b.ChallengeID, Idx0: 2, Idx1: 5, Sig0: txB.Sig0, Sig1: txB.Sig1,
	}
	if res, err := r.GradeSuiteB(ctx, id, runID, respB); err != nil || !res.Passed {
		t.Fatalf("GradeSuiteB: res=%+v err=%v", res, err)
	}

	// Suite C: server-side scratch exercise.
	if res, err := r.GradeSuiteC(ctx, id, runID); err != nil || !res.Passed {
		t.Fatalf("GradeSuiteC: res=%+v err=%v", res, err)
	}

	passed, err := r.FinalizeRun(ctx, id, runID)
	if err != nil || !passed {
		t.Fatalf("FinalizeRun: passed=%v err=%v", passed, err)
	}
	if err := r.AutoActivate(ctx, id); err != nil {
		t.Fatalf("AutoActivate: %v", err)
	}
	return privs
}

// mkNonce returns a distinct 16-byte nonce seeded with the given fill byte.
func mkNonce(fill byte) []byte {
	b := make([]byte, 16)
	for i := range b {
		b[i] = fill
	}
	// Vary the last byte so different fills are distinct even if callers reuse fill.
	b[len(b)-1] = fill
	return b
}

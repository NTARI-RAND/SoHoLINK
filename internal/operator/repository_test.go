//go:build integration

package operator_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	protoop "github.com/NTARI-RAND/sohocloud-protocol/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// connectTestDB mirrors the store package's test harness: TEST_DATABASE_URL
// ONLY (never DATABASE_URL), refuses any database whose name lacks "test", runs
// migrations, and wipes the operator tables for a clean slate. Per the Dev XXIV
// data-loss lesson the destructive guard fires before any TRUNCATE.
func connectTestDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test (see docs/test-database.md)")
	}
	db, err := store.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("store.Connect: %v", err)
	}
	t.Cleanup(func() { db.Pool.Close() })
	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	var dbName string
	if err := db.Pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if !strings.Contains(dbName, "test") {
		t.Fatalf("refusing to run destructive test: database %q does not contain \"test\"", dbName)
	}
	// operators CASCADE wipes operator_keys, operator_verifications,
	// conformance_runs/challenges; operator_replay, operator_nonces, and
	// fee_declarations have no FK to operators, so wipe them explicitly.
	for _, stmt := range []string{
		`TRUNCATE operators CASCADE`,
		`TRUNCATE operator_replay`,
		`TRUNCATE operator_nonces`,
		`TRUNCATE fee_declarations`,
	} {
		if _, err := db.Pool.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("wipe (%s): %v", stmt, err)
		}
	}
	return db
}

// freshKeyset returns 7 keypairs and the 7 raw public keys, in index order.
func freshKeyset(t *testing.T) ([]ed25519.PrivateKey, [][]byte) {
	t.Helper()
	privs := make([]ed25519.PrivateKey, operator.KeyIndexCount)
	pubs := make([][]byte, operator.KeyIndexCount)
	for i := range privs {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("gen key %d: %v", i, err)
		}
		privs[i] = priv
		pubs[i] = pub
	}
	return privs, pubs
}

func evenThresholds(n int) []int {
	th := make([]int, operator.KeyIndexCount)
	for i := range th {
		th[i] = n
	}
	return th
}

// onboardToActive creates an operator, registers 7 keys, verifies email, marks
// conformance passed, and auto-activates. Returns the private keys.
func onboardToActive(t *testing.T, r *operator.Repository, id string, threshold int) []ed25519.PrivateKey {
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
	if err := r.MarkConformancePassed(ctx, id); err != nil {
		t.Fatalf("MarkConformancePassed: %v", err)
	}
	if err := r.AutoActivate(ctx, id); err != nil {
		t.Fatalf("AutoActivate: %v", err)
	}
	return privs
}

func TestCreateOperator_DuplicateRejected(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	if err := r.CreateOperator(ctx, "cloudy", "Cloudy", "Ops@Cloudy.io", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Duplicate slug.
	if err := r.CreateOperator(ctx, "cloudy", "Cloudy2", "other@cloudy.io", ""); err != operator.ErrDuplicateOperator {
		t.Errorf("duplicate slug: got %v, want ErrDuplicateOperator", err)
	}
	// Duplicate email differing only by case/whitespace (normalization).
	if err := r.CreateOperator(ctx, "cloudy2", "Cloudy2", "  ops@cloudy.io ", ""); err != operator.ErrDuplicateOperator {
		t.Errorf("duplicate normalized email: got %v, want ErrDuplicateOperator", err)
	}
}

func TestGetActiveKeyMap_ChokepointGating(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	// Pending operator with keys: authenticate nothing.
	if err := r.CreateOperator(ctx, "pend", "Pending", "pend@example.com", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, pubs := freshKeyset(t)
	if err := r.AddKeys(ctx, "pend", pubs, operator.AlgoEd25519, evenThresholds(100)); err != nil {
		t.Fatalf("AddKeys: %v", err)
	}
	km, err := r.GetActiveKeyMap(ctx, "pend")
	if err != nil {
		t.Fatalf("GetActiveKeyMap(pending): %v", err)
	}
	if km != nil {
		t.Fatal("pending operator must authenticate nothing (nil keymap)")
	}

	// Fully onboarded/active operator: returns 7 keys.
	onboardToActive(t, r, "cloudy", 100)
	km, err = r.GetActiveKeyMap(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetActiveKeyMap(active): %v", err)
	}
	if len(km) != operator.KeyIndexCount {
		t.Fatalf("active operator: got %d keys, want %d", len(km), operator.KeyIndexCount)
	}

	// Revoke: authenticate nothing again.
	if err := r.Revoke(ctx, "cloudy"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	km, err = r.GetActiveKeyMap(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetActiveKeyMap(revoked): %v", err)
	}
	if km != nil {
		t.Fatal("revoked operator must authenticate nothing")
	}

	// Nonexistent operator: nil, no error.
	km, err = r.GetActiveKeyMap(ctx, "ghost")
	if err != nil || km != nil {
		t.Fatalf("ghost operator: km=%v err=%v, want nil,nil", km, err)
	}
}

func TestAutoActivate_KeysetHashBinding(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	// Missing gates: activation refused.
	if err := r.CreateOperator(ctx, "cloudy", "Cloudy", "c@example.com", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.AutoActivate(ctx, "cloudy"); err != operator.ErrActivationPreconditions {
		t.Errorf("no gates: got %v, want ErrActivationPreconditions", err)
	}

	_, pubs := freshKeyset(t)
	if err := r.AddKeys(ctx, "cloudy", pubs, operator.AlgoEd25519, evenThresholds(100)); err != nil {
		t.Fatalf("AddKeys: %v", err)
	}
	if err := r.MarkEmailVerified(ctx, "cloudy"); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
	// Conformance not yet passed.
	if err := r.AutoActivate(ctx, "cloudy"); err != operator.ErrActivationPreconditions {
		t.Errorf("no conformance: got %v, want ErrActivationPreconditions", err)
	}
	if err := r.MarkConformancePassed(ctx, "cloudy"); err != nil {
		t.Fatalf("MarkConformancePassed: %v", err)
	}
	if err := r.AutoActivate(ctx, "cloudy"); err != nil {
		t.Fatalf("AutoActivate: %v", err)
	}
	// Idempotent.
	if err := r.AutoActivate(ctx, "cloudy"); err != nil {
		t.Errorf("second AutoActivate should be idempotent: %v", err)
	}

	// AddKeys after activation must be refused (rotation is the post-active path).
	_, pubs2 := freshKeyset(t)
	if err := r.AddKeys(ctx, "cloudy", pubs2, operator.AlgoEd25519, evenThresholds(100)); err == nil {
		t.Error("AddKeys on an active operator should be refused")
	}
}

func TestAddKeys_ClearsPriorConformancePass(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	if err := r.CreateOperator(ctx, "cloudy", "Cloudy", "c@example.com", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, pubs := freshKeyset(t)
	if err := r.AddKeys(ctx, "cloudy", pubs, operator.AlgoEd25519, evenThresholds(100)); err != nil {
		t.Fatalf("AddKeys: %v", err)
	}
	if err := r.MarkEmailVerified(ctx, "cloudy"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := r.MarkConformancePassed(ctx, "cloudy"); err != nil {
		t.Fatalf("conformance: %v", err)
	}
	// Re-register keys: this clears the conformance pass, so activation fails.
	_, pubs2 := freshKeyset(t)
	if err := r.AddKeys(ctx, "cloudy", pubs2, operator.AlgoEd25519, evenThresholds(100)); err != nil {
		t.Fatalf("re-AddKeys: %v", err)
	}
	if err := r.AutoActivate(ctx, "cloudy"); err != operator.ErrActivationPreconditions {
		t.Errorf("activation after keyset change: got %v, want ErrActivationPreconditions", err)
	}
}

func TestCheckAndAdvance_ReplayAndNonce(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()
	exp := time.Now().Add(10 * time.Minute)

	nonce1 := randNonce(t)
	// First transmission accepted.
	if err := r.CheckAndAdvance(ctx, "cloudy", "soholink", 1, nonce1, "production", exp); err != nil {
		t.Fatalf("first CAS: %v", err)
	}
	// Same nonce again (even with a new seq): replay.
	if err := r.CheckAndAdvance(ctx, "cloudy", "soholink", 2, nonce1, "production", exp); err != operator.ErrReplay {
		t.Errorf("nonce reuse: got %v, want ErrReplay", err)
	}
	// New nonce, stale seq (already seen seq 1): replay.
	if err := r.CheckAndAdvance(ctx, "cloudy", "soholink", 1, randNonce(t), "production", exp); err != operator.ErrReplay {
		t.Errorf("seq replay: got %v, want ErrReplay", err)
	}
	// New nonce, fresh higher seq: accepted.
	if err := r.CheckAndAdvance(ctx, "cloudy", "soholink", 5, randNonce(t), "production", exp); err != nil {
		t.Errorf("fresh seq 5: %v", err)
	}
	// Same nonce reused across a DIFFERENT scope is still a global single-use
	// (nonce is the PK) — but a different coordinator with a fresh nonce and its
	// own window accepts seq 1.
	if err := r.CheckAndAdvance(ctx, "cloudy", "other-coord", 1, randNonce(t), "production", exp); err != nil {
		t.Errorf("independent coordinator window: %v", err)
	}
}

func TestRecordUsageAndExpire_FloorRespected(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	// Low threshold so keys expire quickly; floor is MinActiveKeys (3).
	onboardToActive(t, r, "cloudy", 1)

	// Drive many transmissions across rotating index pairs so each key crosses
	// its threshold (1). Expiry must never drop below MinActiveKeys.
	pairs := [][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 0}, {1, 2}, {3, 4}, {5, 6}}
	sawSwap := false
	for _, p := range pairs {
		swap, err := r.RecordUsageAndExpire(ctx, "cloudy", p[0], p[1])
		if err != nil {
			t.Fatalf("RecordUsageAndExpire%v: %v", p, err)
		}
		sawSwap = sawSwap || swap
	}
	if !sawSwap {
		t.Error("expected X-Operator-Swap-Required to fire at least once")
	}

	var active int
	if err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM operator_keys WHERE operator_id='cloudy' AND state='active'`,
	).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active < operator.MinActiveKeys {
		t.Errorf("active keys dropped to %d, below floor %d", active, operator.MinActiveKeys)
	}
}

func TestRegisterReplacementKey_SignedRotation(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	privs := onboardToActive(t, r, "cloudy", 100)

	// Generate a replacement keypair for index 4.
	newPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen replacement: %v", err)
	}
	rot := protoop.OperatorRotation{
		OperatorID:   "cloudy",
		KeyIndex:     4,
		NewPublicKey: newPub,
		Algo:         operator.AlgoEd25519,
		TsUnixNano:   time.Now().UnixNano(),
		Nonce:        randNonce(t),
		Seq:          1,
	}
	// Authorize with two CURRENT keys (indices 0 and 2, distinct from index 4).
	rot.Sign(privs[0], privs[2], 0, 2)

	if err := r.RegisterReplacementKey(ctx, rot, 100); err != nil {
		t.Fatalf("RegisterReplacementKey: %v", err)
	}

	// The active key at index 4 is now the new public key; old one retired.
	km, err := r.GetActiveKeyMap(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetActiveKeyMap: %v", err)
	}
	if string(km[4].PublicKey) != string(newPub) {
		t.Error("index 4 active key was not replaced with the new public key")
	}
	if len(km) != operator.KeyIndexCount {
		t.Errorf("keyset count changed to %d after rotation", len(km))
	}

	// A rotation with a bad signature is rejected (verify-before-insert).
	bad := rot
	bad.KeyIndex = 5
	bad.Nonce = randNonce(t)
	newPub2, _, _ := ed25519.GenerateKey(rand.Reader)
	bad.NewPublicKey = newPub2
	bad.Sign(privs[0], privs[2], 0, 2)
	bad.Sig0[0] ^= 0xFF
	if err := r.RegisterReplacementKey(ctx, bad, 100); err == nil {
		t.Error("rotation with a tampered signature should be rejected")
	}
}

func randNonce(t *testing.T) []byte {
	t.Helper()
	n := make([]byte, operator.MinNonceLen)
	if _, err := rand.Read(n); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	return n
}

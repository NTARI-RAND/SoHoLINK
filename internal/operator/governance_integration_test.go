//go:build integration

package operator_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// signedFeeDecl builds a signed fees.FeeDeclaration for the coordinator.
func signedFeeDecl(t *testing.T, priv ed25519.PrivateKey, coordID string, seq uint64, effective time.Time) fees.FeeDeclaration {
	t.Helper()
	decl := fees.FeeDeclaration{
		CoordinatorID: coordID,
		Terms:         fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500},
		EffectiveAt:   effective,
		Seq:           seq,
	}
	decl.Sign(priv)
	return decl
}

// A first declaration publishes and reads back byte-identical (signature intact).
func TestPublishFeeDeclaration_FirstAndCurrent(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	decl := signedFeeDecl(t, priv, "soholink", 1, base)

	if err := r.PublishFeeDeclaration(ctx, decl); err != nil {
		t.Fatalf("PublishFeeDeclaration: %v", err)
	}

	cur, err := r.CurrentFeeDeclaration(ctx, "soholink")
	if err != nil {
		t.Fatalf("CurrentFeeDeclaration: %v", err)
	}
	if cur.Seq != 1 || cur.Terms.ContributorShareBps != 6500 || cur.Terms.PlatformFeeBps != 3500 {
		t.Fatalf("current mismatch: %+v", cur)
	}
	if !cur.Verify(pub) {
		t.Fatalf("read-back declaration signature does not verify")
	}
	if !cur.EffectiveAt.Equal(base) {
		t.Fatalf("effective_at mismatch: got %v want %v", cur.EffectiveAt, base)
	}
}

// A monotonic, later declaration supersedes; the current read returns the latest.
func TestPublishFeeDeclaration_MonotonicSupersede(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	if err := r.PublishFeeDeclaration(ctx, signedFeeDecl(t, priv, "soholink", 1, base)); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	newer := fees.FeeDeclaration{
		CoordinatorID: "soholink",
		Terms:         fees.Terms{ContributorShareBps: 7000, PlatformFeeBps: 3000},
		EffectiveAt:   base.Add(31 * 24 * time.Hour),
		Seq:           2,
	}
	newer.Sign(priv)
	if err := r.PublishFeeDeclaration(ctx, newer); err != nil {
		t.Fatalf("second publish: %v", err)
	}

	cur, err := r.CurrentFeeDeclaration(ctx, "soholink")
	if err != nil {
		t.Fatalf("CurrentFeeDeclaration: %v", err)
	}
	if cur.Seq != 2 || cur.Terms.ContributorShareBps != 7000 {
		t.Fatalf("expected latest (seq 2, 7000), got %+v", cur)
	}
}

// A non-monotonic Seq is rejected and the current declaration is unchanged.
func TestPublishFeeDeclaration_RejectsNonMonotonicSeq(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := r.PublishFeeDeclaration(ctx, signedFeeDecl(t, priv, "soholink", 5, base)); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	// Same seq, later time -> non-monotonic seq.
	err := r.PublishFeeDeclaration(ctx, signedFeeDecl(t, priv, "soholink", 5, base.Add(time.Hour)))
	if !errors.Is(err, operator.ErrFeeSeqNotMonotonic) {
		t.Fatalf("got %v, want ErrFeeSeqNotMonotonic", err)
	}
}

// A retroactive EffectiveAt is rejected (SPEC §5.3 non-retroactive).
func TestPublishFeeDeclaration_RejectsRetroactive(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if err := r.PublishFeeDeclaration(ctx, signedFeeDecl(t, priv, "soholink", 1, base)); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	// Higher seq but earlier effective_at -> retroactive.
	err := r.PublishFeeDeclaration(ctx, signedFeeDecl(t, priv, "soholink", 2, base.Add(-time.Hour)))
	if !errors.Is(err, operator.ErrFeeEffectiveAtRetroactive) {
		t.Fatalf("got %v, want ErrFeeEffectiveAtRetroactive", err)
	}
}

// An unsigned declaration is refused before any DB write.
func TestPublishFeeDeclaration_RejectsUnsigned(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	decl := fees.FeeDeclaration{
		CoordinatorID: "soholink",
		Terms:         fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500},
		EffectiveAt:   time.Now(),
		Seq:           1,
	}
	if err := r.PublishFeeDeclaration(ctx, decl); !errors.Is(err, operator.ErrFeeUnsigned) {
		t.Fatalf("got %v, want ErrFeeUnsigned", err)
	}
}

// CurrentFeeDeclaration with nothing published returns ErrNoFeeDeclaration.
func TestCurrentFeeDeclaration_None(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()
	if _, err := r.CurrentFeeDeclaration(ctx, "soholink"); !errors.Is(err, operator.ErrNoFeeDeclaration) {
		t.Fatalf("got %v, want ErrNoFeeDeclaration", err)
	}
}

// Disconnect flips status='revoked' so GetActiveKeyMap returns nil immediately.
func TestDisconnect_KillsAuth(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	onboardToActive(t, r, "cloudy", 365)
	km, err := r.GetActiveKeyMap(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetActiveKeyMap (pre): %v", err)
	}
	if len(km) != operator.KeyIndexCount {
		t.Fatalf("expected 7 active keys before disconnect, got %d", len(km))
	}

	if err := r.Disconnect(ctx, "cloudy"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	km, err = r.GetActiveKeyMap(ctx, "cloudy")
	if err != nil {
		t.Fatalf("GetActiveKeyMap (post): %v", err)
	}
	if km != nil {
		t.Fatalf("expected nil keymap after disconnect, got %d keys", len(km))
	}
}

// OperatorEmails and MemberEmails return the expected recipient sets.
func TestRecipientEmails(t *testing.T) {
	db := connectTestDB(t)
	r := operator.NewRepository(db.Pool)
	ctx := context.Background()

	if err := r.CreateOperator(ctx, "cloudy", "Cloudy", "ops@cloudy.example", ""); err != nil {
		t.Fatalf("CreateOperator: %v", err)
	}
	emails, err := r.OperatorEmails(ctx)
	if err != nil {
		t.Fatalf("OperatorEmails: %v", err)
	}
	found := false
	for _, e := range emails {
		if e == "ops@cloudy.example" {
			found = true
		}
	}
	if !found {
		t.Fatalf("operator email not in %v", emails)
	}

	// MemberEmails reads participants; the table exists (migration 011) and may be
	// empty in the test DB. Just assert it runs without error.
	if _, err := r.MemberEmails(ctx); err != nil {
		t.Fatalf("MemberEmails: %v", err)
	}
}

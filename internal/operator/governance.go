package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	"github.com/jackc/pgx/v5"
)

// This file implements the coordinator-side governance data layer used by the
// LOCAL-ONLY :8090 admin surface (step 4): publishing signed fee declarations,
// reading the current declaration for the public /fees read, and enumerating
// recipient email addresses for the messaging feature.
//
// Revoke and Disconnect (the operator kill switches) live in repository.go and
// are also part of the :8090 surface.

// Fee-declaration errors, distinguishable so the :8090 handler can map each to a
// status. All are governance-side (the admin authored a bad declaration), never
// surfaced on a public handler.
var (
	// ErrFeeSeqNotMonotonic is returned when a fee declaration's Seq is not
	// strictly greater than the current (latest) declaration's Seq for the same
	// coordinator. SPEC §5.3: a change is a NEW declaration with a later Seq.
	ErrFeeSeqNotMonotonic = errors.New("operator: fee declaration Seq must strictly exceed the current declaration")
	// ErrFeeEffectiveAtRetroactive is returned when a fee declaration's
	// EffectiveAt is not strictly later than the current declaration's
	// EffectiveAt. SPEC §5.3: fees are non-retroactive; a change takes effect at
	// a later instant, never earlier or equal.
	ErrFeeEffectiveAtRetroactive = errors.New("operator: fee declaration EffectiveAt must be strictly later than the current declaration (non-retroactive)")
	// ErrFeeUnsigned is returned when a declaration handed to PublishFeeDeclaration
	// carries no signature. The signing happens on the :8090 handler with the
	// coordinator key; the repository refuses to persist an unsigned artifact.
	ErrFeeUnsigned = errors.New("operator: fee declaration is not signed")
	// ErrNoFeeDeclaration is returned by CurrentFeeDeclaration when no declaration
	// has been published yet for the coordinator.
	ErrNoFeeDeclaration = errors.New("operator: no fee declaration published")
)

// PublishFeeDeclaration persists an already-signed fees.FeeDeclaration, enforcing
// the SPEC §5.3 legibility + non-retroactivity invariants against the CURRENT
// declaration under a serialized transaction:
//
//   - the declaration MUST carry a signature (signing is the caller's job, with
//     the coordinator key loaded from env — never here, never on a public path),
//   - its Seq MUST strictly exceed the current declaration's Seq, and
//   - its EffectiveAt MUST be strictly later than the current declaration's
//     EffectiveAt (non-retroactive: terms never change for already-offered work).
//
// The very first declaration for a coordinator has no predecessor and is
// accepted as-is (any Seq/EffectiveAt). The read-current + insert run in one
// transaction so two concurrent publishes cannot both pass the monotonicity
// check; the (coordinator_id, seq) UNIQUE constraint is the final backstop.
func (r *Repository) PublishFeeDeclaration(ctx context.Context, decl fees.FeeDeclaration) error {
	if len(decl.Signature) == 0 {
		return ErrFeeUnsigned
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin fee-publish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Read the current (latest by Seq) declaration for this coordinator, if any,
	// under a lock so a concurrent publish serializes behind us.
	var (
		curSeq         int64
		curEffectiveAt time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT seq, effective_at FROM fee_declarations
		WHERE coordinator_id = $1
		ORDER BY seq DESC
		LIMIT 1
		FOR UPDATE`,
		decl.CoordinatorID,
	).Scan(&curSeq, &curEffectiveAt)
	switch {
	case err == nil:
		// A predecessor exists: enforce strict monotonicity on both axes.
		if decl.Seq <= uint64(curSeq) {
			return ErrFeeSeqNotMonotonic
		}
		if !decl.EffectiveAt.After(curEffectiveAt) {
			return ErrFeeEffectiveAtRetroactive
		}
	case errors.Is(err, pgx.ErrNoRows):
		// First declaration for this coordinator; nothing to compare against.
	default:
		return fmt.Errorf("operator: load current fee declaration: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO fee_declarations
		    (coordinator_id, contributor_share_bps, platform_fee_bps, effective_at, seq, signature)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		decl.CoordinatorID,
		decl.Terms.ContributorShareBps,
		decl.Terms.PlatformFeeBps,
		decl.EffectiveAt,
		int64(decl.Seq),
		decl.Signature,
	)
	if err != nil {
		if isUniqueViolation(err) {
			// Lost the race on (coordinator_id, seq): another publish used this Seq.
			return ErrFeeSeqNotMonotonic
		}
		return fmt.Errorf("operator: insert fee declaration: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit fee declaration: %w", err)
	}
	return nil
}

// CurrentFeeDeclaration returns the latest (highest Seq) signed declaration for
// the coordinator, reconstructed as a fees.FeeDeclaration so the public /fees
// read can re-serve the exact signed artifact (and a reader can independently
// verify the signature). Returns ErrNoFeeDeclaration when none exists.
func (r *Repository) CurrentFeeDeclaration(ctx context.Context, coordinatorID string) (fees.FeeDeclaration, error) {
	var (
		decl                                fees.FeeDeclaration
		contributorShareBps, platformFeeBps int
		seq                                 int64
	)
	err := r.pool.QueryRow(ctx, `
		SELECT coordinator_id, contributor_share_bps, platform_fee_bps, effective_at, seq, signature
		FROM fee_declarations
		WHERE coordinator_id = $1
		ORDER BY seq DESC
		LIMIT 1`,
		coordinatorID,
	).Scan(
		&decl.CoordinatorID,
		&contributorShareBps,
		&platformFeeBps,
		&decl.EffectiveAt,
		&seq,
		&decl.Signature,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fees.FeeDeclaration{}, ErrNoFeeDeclaration
		}
		return fees.FeeDeclaration{}, fmt.Errorf("operator: load current fee declaration: %w", err)
	}
	decl.Terms = fees.Terms{ContributorShareBps: contributorShareBps, PlatformFeeBps: platformFeeBps}
	decl.Seq = uint64(seq)
	return decl, nil
}

// -----------------------------------------------------------------------------
// Messaging recipient enumeration (:8090 messaging feature).
// -----------------------------------------------------------------------------

// OperatorEmails returns the email addresses of all operators (regardless of
// onboarding state or status), for the :8090 messaging feature. The admin can
// message operators, members, or both. Emails are already normalized at insert.
func (r *Repository) OperatorEmails(ctx context.Context) ([]string, error) {
	return r.collectEmails(ctx, `SELECT email FROM operators WHERE email <> '' ORDER BY email`)
}

// MemberEmails returns the email addresses of all members (participants).
//
// TRANSITIONAL: members are a Cloudy-owned concern. SoHoLINK still holds member
// records in the `participants` table pending the frontend migration to Cloudy;
// this query reads them so the coordinator admin can reach current members
// during the transition. When member records move to Cloudy, this method and its
// use in the :8090 messaging handler move with them (the operators half stays).
func (r *Repository) MemberEmails(ctx context.Context) ([]string, error) {
	return r.collectEmails(ctx, `SELECT email FROM participants WHERE email <> '' ORDER BY email`)
}

// collectEmails runs a single-column email query and returns the deduplicated,
// non-empty results.
func (r *Repository) collectEmails(ctx context.Context, query string) ([]string, error) {
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("operator: query recipient emails: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("operator: scan recipient email: %w", err)
		}
		if email == "" {
			continue
		}
		if _, dup := seen[email]; dup {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operator: iterate recipient emails: %w", err)
	}
	return out, nil
}

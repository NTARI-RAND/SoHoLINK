package operator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// This file is the READ-MODEL for the operator console GET pages (Stage 4). Every
// method here is a PURE READ: it opens no transaction that mutates, writes no row,
// and advances no counter. The public operator dashboard/fees pages and the
// LOCAL-ONLY :8090 admin queue/detail pages render from these shapes.
//
// Separation of concerns: the write/verify chokepoint lives in repository.go,
// conformance.go, verification.go, and governance.go. This file only observes
// state those paths produced. It reads across operators, operator_keys,
// conformance_runs, operator_nonces (production scope), and fee_declarations.
//
// Nothing here gates on active-status the way GetActiveKeyMap does: the admin
// queue must see pending/verified/revoked operators, and the public dashboard is
// status-aware (it renders an "awaiting admission" recap for a not-yet-active
// operator). Callers decide what to show; the read model reports the raw facts.

// -----------------------------------------------------------------------------
// Overview: ListOperators + status counts (:8090 admin queue).
// -----------------------------------------------------------------------------

// OperatorStatusCounts is the queue-header .stat-grid on GET /admin/operators:
// how many operators sit in each disposition. Pending and Verified partition the
// not-yet-active operators by onboarding_state; Active and Revoked partition the
// rest by status. VerifiedPassed is the subset of verified operators that have
// also passed conformance (the "ready to admit" bucket the admin acts on).
type OperatorStatusCounts struct {
	Pending        int `json:"pending"`         // onboarding_state='pending_verification', status='active'
	Verified       int `json:"verified"`        // onboarding_state='verified', status='active'
	VerifiedPassed int `json:"verified_passed"` // verified AND conformance_passed_at IS NOT NULL
	Active         int `json:"active"`          // onboarding_state='active', status='active'
	Revoked        int `json:"revoked"`         // status='revoked'
	Total          int `json:"total"`
}

// OperatorSummary is one row of the admin queue / overview table: the columns the
// :8090 queue renders (Operator | Name | State | Email✓ | Conformance | Registered
// | Action). ActiveKeyCount lets the queue show "7/7 registered"; EmailVerified
// and ConformancePassed drive the Email✓ and Conformance columns; ReadyToActivate
// is the derived "Review →" affordance (verified + conformance passed).
type OperatorSummary struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Email               string     `json:"email"`
	Status              string     `json:"status"`           // 'active' | 'revoked'
	OnboardingState     string     `json:"onboarding_state"` // 'pending_verification' | 'verified' | 'active'
	EmailVerified       bool       `json:"email_verified"`
	ConformancePassed   bool       `json:"conformance_passed"`
	ConformancePassedAt *time.Time `json:"conformance_passed_at,omitempty"`
	ActiveKeyCount      int        `json:"active_key_count"`
	CreatedAt           time.Time  `json:"created_at"`
	// ReadyToActivate is the derived admission-eligible flag for the queue's
	// "Review →" affordance: verified state AND conformance passed AND status
	// still active. It is a UI hint only; AutoActivate re-checks the full
	// preconditions (including the keyset-hash match) under a lock before flipping.
	ReadyToActivate bool `json:"ready_to_activate"`
}

// ListOperators returns every operator as an OperatorSummary (newest first) plus
// the aggregate status counts for the queue header, in one read. Both are derived
// from a single scan of operators joined to a per-operator active-key count, so
// the counts and the rows are always consistent with each other.
//
// This is a pure read: no lock, no mutation. It intentionally does NOT filter by
// status — the admin queue shows pending, verified, active, and revoked
// operators together.
func (r *Repository) ListOperators(ctx context.Context) ([]OperatorSummary, OperatorStatusCounts, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.id, o.name, o.email, o.status, o.onboarding_state,
		       o.email_verified, o.conformance_passed_at, o.created_at,
		       COALESCE(k.active_keys, 0) AS active_keys
		FROM operators o
		LEFT JOIN (
			SELECT operator_id, COUNT(*) AS active_keys
			FROM operator_keys
			WHERE state = 'active'
			GROUP BY operator_id
		) k ON k.operator_id = o.id
		ORDER BY o.created_at DESC, o.id ASC`)
	if err != nil {
		return nil, OperatorStatusCounts{}, fmt.Errorf("operator: list operators: %w", err)
	}
	defer rows.Close()

	var (
		out    []OperatorSummary
		counts OperatorStatusCounts
	)
	for rows.Next() {
		var (
			s               OperatorSummary
			conformancePass *time.Time
		)
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Email, &s.Status, &s.OnboardingState,
			&s.EmailVerified, &conformancePass, &s.CreatedAt, &s.ActiveKeyCount,
		); err != nil {
			return nil, OperatorStatusCounts{}, fmt.Errorf("operator: scan operator summary: %w", err)
		}
		s.ConformancePassed = conformancePass != nil
		s.ConformancePassedAt = conformancePass
		s.ReadyToActivate = s.Status == "active" &&
			s.OnboardingState == "verified" && s.ConformancePassed

		counts.Total++
		switch {
		case s.Status == "revoked":
			counts.Revoked++
		case s.OnboardingState == "active":
			counts.Active++
		case s.OnboardingState == "verified":
			counts.Verified++
			if s.ConformancePassed {
				counts.VerifiedPassed++
			}
		case s.OnboardingState == "pending_verification":
			counts.Pending++
		}

		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, OperatorStatusCounts{}, fmt.Errorf("operator: iterate operator summaries: %w", err)
	}
	return out, counts, nil
}

// -----------------------------------------------------------------------------
// Detail / dashboard: GetOperator with keys + latest run + lifecycle.
// -----------------------------------------------------------------------------

// KeyView is one row of the signing-keys table on both the public dashboard and
// the :8090 detail page. PublicKeyB64Trunc is a display-only truncated base64 of
// the raw public key (the full key is public but the table shows a short form);
// callers render it inside a <code>. NearThreshold flags a key whose usage is
// within NearThresholdWindow of its expiration_threshold, so the dashboard can
// show the amber "rotate soon" badge described in design §10.
type KeyView struct {
	KeyIndex            int    `json:"key_index"`
	PublicKeyB64Trunc   string `json:"public_key_b64_trunc"` // display-only truncated base64
	Algo                string `json:"algo"`
	State               string `json:"state"` // 'active' | 'expired' | 'retired' | 'revoked'
	UsageCount          int    `json:"usage_count"`
	ExpirationThreshold int    `json:"expiration_threshold"`
	// NearThreshold is true for an ACTIVE key whose usage_count is within
	// NearThresholdWindow of its expiration_threshold (usage >= threshold-window),
	// including a key already at/over threshold. Drives the amber near-expiry badge.
	NearThreshold bool `json:"near_threshold"`
}

// NearThresholdWindow is how close (in remaining uses) an active key's usage must
// come to its expiration_threshold before KeyView.NearThreshold flags it for the
// dashboard's amber "rotate soon" badge. A pilot-scale heuristic; not a protocol
// invariant.
const NearThresholdWindow = 10

// ConformanceRunView is the latest conformance run for an operator plus its
// per-suite verdicts, for the dashboard's conformance stat and the :8090 detail
// page's per-check table. Suites maps 'A'/'B'/'C' to a pass/fail summary; the
// admin detail page renders one row per suite. Status is the run-level
// 'running'/'passed'/'failed'.
type ConformanceRunView struct {
	RunID      string             `json:"run_id"`
	Status     string             `json:"status"` // 'running' | 'passed' | 'failed'
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
	Suites     []SuiteVerdictView `json:"suites"`
}

// SuiteVerdictView is one suite's graded verdict within a run (design §9's three
// suites: A canonical bytes, B transmission, C rotation/expiry). Passed reflects
// the stored per-challenge result; Detail carries the first-differing-offset or
// failure reason the grader recorded. A suite with no consumed challenge yet has
// Graded=false (still pending an operator response).
type SuiteVerdictView struct {
	Suite  ConformanceSuite `json:"suite"`
	Graded bool             `json:"graded"` // false = challenge issued but not yet responded/graded
	Passed bool             `json:"passed"`
	Detail string           `json:"detail,omitempty"`
}

// LifecycleView is the onboarding timeline for the dashboard/detail page: created
// → verified → conformance passed → activated. Each timestamp is nil until that
// milestone is reached. Revoked, when set, records that the operator was later
// disconnected on :8090 (status flipped to 'revoked'); the registry does not
// store the revocation instant, so RevokedNow only reports the boolean fact.
type LifecycleView struct {
	CreatedAt         time.Time  `json:"created_at"`
	EmailVerified     bool       `json:"email_verified"`
	ConformancePassed *time.Time `json:"conformance_passed_at,omitempty"`
	Activated         bool       `json:"activated"` // onboarding_state = 'active'
	Revoked           bool       `json:"revoked"`   // status = 'revoked'
}

// OperatorDetail is the full read model behind GET /operators/{id} (public,
// status-aware dashboard) and GET /admin/operators/{id} (:8090 detail). It bundles
// the operator's core facts, its key/lifecycle views, the latest conformance run
// with per-suite verdicts, and the transmissions-in-last-24h count. Keys are
// ordered by key_index; both active and historical (expired/retired/revoked) rows
// are included so the detail page can show the full key history.
//
// KeysetHashMatches reports whether the CURRENT active keyset hash equals the
// conformance-passed keyset hash — the exact precondition AutoActivate enforces.
// The :8090 detail page uses it to enable/disable the "Activate operator" button
// (design §10 admin detail: "enabled only when verified+passed AND keyset-hash
// matches"). It is false whenever conformance has not passed.
type OperatorDetail struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	Email                string              `json:"email"`
	Phone                string              `json:"phone,omitempty"`
	Status               string              `json:"status"`
	OnboardingState      string              `json:"onboarding_state"`
	EmailVerified        bool                `json:"email_verified"`
	ConformancePassedAt  *time.Time          `json:"conformance_passed_at,omitempty"`
	ActiveKeyCount       int                 `json:"active_key_count"`
	Keys                 []KeyView           `json:"keys"`
	Lifecycle            LifecycleView       `json:"lifecycle"`
	LatestRun            *ConformanceRunView `json:"latest_run,omitempty"`
	TransmissionsLast24h int                 `json:"transmissions_last_24h"`
	KeysetHashMatches    bool                `json:"keyset_hash_matches"`
	ReadyToActivate      bool                `json:"ready_to_activate"`
}

// GetOperator loads the full detail for one operator: its core row, all keys
// (active + historical, ordered by index), the derived lifecycle, the latest
// conformance run with per-suite verdicts, the transmissions-in-last-24h count,
// and the keyset-hash-match flag the :8090 activate button gates on. Returns
// ErrOperatorNotFound if the id does not exist.
//
// Pure read; no lock. It recomputes the current active keyset hash to compare
// against the stored conformance_keyset_hash — the same H(sorted active pubkeys)
// used by activation — but writes nothing.
func (r *Repository) GetOperator(ctx context.Context, operatorID string) (OperatorDetail, error) {
	var (
		d               OperatorDetail
		phone           *string
		conformancePass *time.Time
		conformanceHash []byte
		createdAt       time.Time
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, email, phone, status, onboarding_state,
		       email_verified, conformance_passed_at, conformance_keyset_hash, created_at
		FROM operators WHERE id = $1`,
		operatorID,
	).Scan(
		&d.ID, &d.Name, &d.Email, &phone, &d.Status, &d.OnboardingState,
		&d.EmailVerified, &conformancePass, &conformanceHash, &createdAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OperatorDetail{}, ErrOperatorNotFound
		}
		return OperatorDetail{}, fmt.Errorf("operator: load operator detail: %w", err)
	}
	if phone != nil {
		d.Phone = *phone
	}
	d.ConformancePassedAt = conformancePass

	// Keys (active + historical), ordered by index then state so the active row at
	// an index sorts first for display.
	keys, activeCount, err := r.keyViews(ctx, operatorID)
	if err != nil {
		return OperatorDetail{}, err
	}
	d.Keys = keys
	d.ActiveKeyCount = activeCount

	// Lifecycle timeline.
	d.Lifecycle = LifecycleView{
		CreatedAt:         createdAt,
		EmailVerified:     d.EmailVerified,
		ConformancePassed: conformancePass,
		Activated:         d.OnboardingState == "active",
		Revoked:           d.Status == "revoked",
	}

	// Latest conformance run with per-suite verdicts (nil if none yet).
	run, err := r.latestRunView(ctx, operatorID)
	if err != nil {
		return OperatorDetail{}, err
	}
	d.LatestRun = run

	// Transmissions in the last 24h (production-scope nonces; see method doc).
	tx24, err := r.TransmissionsLast24h(ctx, operatorID)
	if err != nil {
		return OperatorDetail{}, err
	}
	d.TransmissionsLast24h = tx24

	// Keyset-hash match: recompute the current active keyset hash and compare to
	// the conformance-passed hash. Only meaningful when conformance passed AND
	// exactly the full keyset is active; otherwise it is false.
	if len(conformanceHash) > 0 {
		curHash, count, hErr := r.currentActiveKeysetHash(ctx, operatorID)
		if hErr != nil {
			return OperatorDetail{}, hErr
		}
		d.KeysetHashMatches = count == KeyIndexCount &&
			stringsCompareBytes(curHash, conformanceHash) == 0
	}

	d.ReadyToActivate = d.Status == "active" &&
		d.OnboardingState == "verified" &&
		conformancePass != nil &&
		d.KeysetHashMatches

	return d, nil
}

// keyViews loads all of an operator's keys as KeyViews (active + historical),
// ordered by key_index then by state-ordinal so the active row at an index sorts
// first. It returns the views and the count of active keys. Pure read.
func (r *Repository) keyViews(ctx context.Context, operatorID string) ([]KeyView, int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT key_index, public_key, algo, state::text, usage_count, expiration_threshold
		FROM operator_keys
		WHERE operator_id = $1
		ORDER BY key_index ASC,
		         CASE state WHEN 'active' THEN 0 WHEN 'expired' THEN 1
		                    WHEN 'retired' THEN 2 ELSE 3 END ASC,
		         created_at DESC`,
		operatorID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("operator: query key views: %w", err)
	}
	defer rows.Close()

	var (
		out         []KeyView
		activeCount int
	)
	for rows.Next() {
		var (
			kv  KeyView
			pub []byte
		)
		if err := rows.Scan(&kv.KeyIndex, &pub, &kv.Algo, &kv.State, &kv.UsageCount, &kv.ExpirationThreshold); err != nil {
			return nil, 0, fmt.Errorf("operator: scan key view: %w", err)
		}
		kv.PublicKeyB64Trunc = truncPubKey(pub)
		if kv.State == "active" {
			activeCount++
			if kv.UsageCount >= kv.ExpirationThreshold-NearThresholdWindow {
				kv.NearThreshold = true
			}
		}
		out = append(out, kv)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("operator: iterate key views: %w", err)
	}
	return out, activeCount, nil
}

// latestRunView loads the most recent conformance run for the operator and its
// per-suite verdicts (design §9 suites A/B/C). Returns (nil, nil) when the
// operator has never started a run. Pure read.
func (r *Repository) latestRunView(ctx context.Context, operatorID string) (*ConformanceRunView, error) {
	var (
		v          ConformanceRunView
		finishedAt *time.Time
	)
	err := r.pool.QueryRow(ctx, `
		SELECT run_id::text, status, started_at, finished_at
		FROM conformance_runs
		WHERE operator_id = $1
		ORDER BY started_at DESC
		LIMIT 1`,
		operatorID,
	).Scan(&v.RunID, &v.Status, &v.StartedAt, &finishedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // no run yet
		}
		return nil, fmt.Errorf("operator: load latest run: %w", err)
	}
	v.FinishedAt = finishedAt

	suites, err := r.suiteVerdicts(ctx, v.RunID)
	if err != nil {
		return nil, err
	}
	v.Suites = suites
	return &v, nil
}

// suiteVerdicts loads the per-suite verdicts for a run, one row per (suite, idx)
// challenge, collapsed to one entry per suite in A,B,C order. A suite whose
// challenge has been graded reports Passed/Detail from the stored result; a suite
// whose challenge exists but has not been consumed reports Graded=false. Pure read.
func (r *Repository) suiteVerdicts(ctx context.Context, runID string) ([]SuiteVerdictView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT suite, result, (consumed_at IS NOT NULL) AS consumed
		FROM conformance_challenges
		WHERE run_id = $1
		ORDER BY suite ASC, idx ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("operator: query suite verdicts: %w", err)
	}
	defer rows.Close()

	// Collapse to one verdict per suite: a suite passes only if every graded
	// challenge in it passed; the first failing detail is surfaced.
	bySuite := map[ConformanceSuite]*SuiteVerdictView{}
	order := []ConformanceSuite{}
	for rows.Next() {
		var (
			suite     string
			resultRaw []byte
			consumed  bool
		)
		if err := rows.Scan(&suite, &resultRaw, &consumed); err != nil {
			return nil, fmt.Errorf("operator: scan suite verdict: %w", err)
		}
		cs := ConformanceSuite(suite)
		v, ok := bySuite[cs]
		if !ok {
			v = &SuiteVerdictView{Suite: cs, Passed: true}
			bySuite[cs] = v
			order = append(order, cs)
		}
		if !consumed || len(resultRaw) == 0 {
			// Challenge issued but not yet responded/graded.
			v.Graded = false
			v.Passed = false
			continue
		}
		v.Graded = true
		var res ChallengeResult
		if err := unmarshalResult(resultRaw, &res); err != nil {
			return nil, err
		}
		if !res.Passed {
			v.Passed = false
			if v.Detail == "" {
				v.Detail = res.Detail
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operator: iterate suite verdicts: %w", err)
	}

	out := make([]SuiteVerdictView, 0, len(order))
	for _, cs := range order {
		out = append(out, *bySuite[cs])
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Transmissions-in-last-24h count.
// -----------------------------------------------------------------------------

// TransmissionsLast24h counts the operator's production transmissions accepted in
// the last 24 hours. Each accepted production transmission durably records a
// single-use nonce row (operator_nonces, scope='production') via the OperatorAuth
// CAS path, so a COUNT of production nonces created in the window is the
// transmission volume for the dashboard's stat-grid.
//
// Caveat (accurate for the current build): production nonces are NOT yet swept —
// no pruning job runs (the operator_nonces expiry index is reserved for a future
// sweeper). While unswept, this count is exact for the 24h window. When a nonce
// sweeper lands, it MUST retain rows younger than 24h (or this count must move to
// a dedicated transmissions ledger) or the dashboard number will under-report.
// Pure read.
func (r *Repository) TransmissionsLast24h(ctx context.Context, operatorID string) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM operator_nonces
		WHERE operator_id = $1
		  AND scope = 'production'
		  AND created_at > NOW() - INTERVAL '24 hours'`,
		operatorID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("operator: count 24h transmissions: %w", err)
	}
	return n, nil
}

// -----------------------------------------------------------------------------
// Fee read model: history + NextSeq + current signed declaration.
// -----------------------------------------------------------------------------

// FeeDeclarationView is one row of the fee-declaration history table (public
// GET /operators/{id}/fees and the :8090 fees surface). Terms are surfaced as
// basis points (no-information-asymmetry: rates are visible). IsCurrent marks the
// latest (highest Seq) declaration — the one presently in force.
type FeeDeclarationView struct {
	CoordinatorID       string    `json:"coordinator_id"`
	ContributorShareBps int       `json:"contributor_share_bps"`
	PlatformFeeBps      int       `json:"platform_fee_bps"`
	EffectiveAt         time.Time `json:"effective_at"`
	Seq                 uint64    `json:"seq"`
	CreatedAt           time.Time `json:"created_at"`
	IsCurrent           bool      `json:"is_current"`
}

// FeeHistory returns every fee declaration for the coordinator, newest (highest
// Seq) first, with IsCurrent set on the first (current) row. Empty slice (not an
// error) when none have been published. Pure read.
func (r *Repository) FeeHistory(ctx context.Context, coordinatorID string) ([]FeeDeclarationView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT coordinator_id, contributor_share_bps, platform_fee_bps,
		       effective_at, seq, created_at
		FROM fee_declarations
		WHERE coordinator_id = $1
		ORDER BY seq DESC`,
		coordinatorID,
	)
	if err != nil {
		return nil, fmt.Errorf("operator: query fee history: %w", err)
	}
	defer rows.Close()

	var out []FeeDeclarationView
	for rows.Next() {
		var (
			v   FeeDeclarationView
			seq int64
		)
		if err := rows.Scan(
			&v.CoordinatorID, &v.ContributorShareBps, &v.PlatformFeeBps,
			&v.EffectiveAt, &seq, &v.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("operator: scan fee declaration: %w", err)
		}
		v.Seq = uint64(seq)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operator: iterate fee history: %w", err)
	}
	if len(out) > 0 {
		out[0].IsCurrent = true
	}
	return out, nil
}

// NextSeq returns the Seq the NEXT fee declaration for the coordinator must use:
// current max Seq + 1, or 0 when none exist yet. The :8090 publish form pre-fills
// this so the admin authors a monotonic Seq; PublishFeeDeclaration still enforces
// strict monotonicity under a lock (this is a convenience read, not the guard).
// Pure read.
func (r *Repository) NextSeq(ctx context.Context, coordinatorID string) (uint64, error) {
	var maxSeq *int64
	if err := r.pool.QueryRow(ctx, `
		SELECT MAX(seq) FROM fee_declarations WHERE coordinator_id = $1`,
		coordinatorID,
	).Scan(&maxSeq); err != nil {
		return 0, fmt.Errorf("operator: query next fee seq: %w", err)
	}
	if maxSeq == nil {
		return 0, nil
	}
	return uint64(*maxSeq) + 1, nil
}

// CurrentFeeDeclarationView returns the current (highest Seq) fee declaration as a
// display view, or (zero, ErrNoFeeDeclaration) when none exist. This is the
// display-friendly sibling of CurrentFeeDeclaration (governance.go), which returns
// the signable fees.FeeDeclaration with its signature bytes for re-serving. The
// public /fees page uses this view for the rate summary; a client wanting to
// independently verify the signature uses CurrentFeeDeclaration. Pure read.
func (r *Repository) CurrentFeeDeclarationView(ctx context.Context, coordinatorID string) (FeeDeclarationView, error) {
	var (
		v   FeeDeclarationView
		seq int64
	)
	err := r.pool.QueryRow(ctx, `
		SELECT coordinator_id, contributor_share_bps, platform_fee_bps,
		       effective_at, seq, created_at
		FROM fee_declarations
		WHERE coordinator_id = $1
		ORDER BY seq DESC
		LIMIT 1`,
		coordinatorID,
	).Scan(&v.CoordinatorID, &v.ContributorShareBps, &v.PlatformFeeBps, &v.EffectiveAt, &seq, &v.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return FeeDeclarationView{}, ErrNoFeeDeclaration
		}
		return FeeDeclarationView{}, fmt.Errorf("operator: load current fee view: %w", err)
	}
	v.Seq = uint64(seq)
	v.IsCurrent = true
	return v, nil
}

// -----------------------------------------------------------------------------
// helpers.
// -----------------------------------------------------------------------------

// currentActiveKeysetHash recomputes H(sorted active pubkeys) for the operator
// outside a mutating transaction, for the read-model keyset-hash-match check. It
// mirrors activeKeysetHash (repository.go) but runs on the pool directly (no tx),
// since the read model never needs the row lock. Returns the hash and the active
// key count.
func (r *Repository) currentActiveKeysetHash(ctx context.Context, operatorID string) ([]byte, int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT public_key FROM operator_keys
		WHERE operator_id = $1 AND state = 'active'`,
		operatorID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("operator: query active keyset for read-model hash: %w", err)
	}
	defer rows.Close()

	var pubs [][]byte
	for rows.Next() {
		var pub []byte
		if err := rows.Scan(&pub); err != nil {
			return nil, 0, fmt.Errorf("operator: scan read-model keyset pubkey: %w", err)
		}
		cp := make([]byte, len(pub))
		copy(cp, pub)
		pubs = append(pubs, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("operator: iterate read-model keyset: %w", err)
	}
	return hashSortedPubkeys(pubs), len(pubs), nil
}

// truncPubKey returns a short, display-only base64 rendering of a raw public key:
// the first 12 base64 chars followed by an ellipsis. The full key is public, but
// the console tables show a truncated form inside a <code> for readability.
func truncPubKey(pub []byte) string {
	full := base64.StdEncoding.EncodeToString(pub)
	const shown = 12
	if len(full) <= shown {
		return full
	}
	return full[:shown] + "…"
}

// unmarshalResult decodes a stored per-challenge ChallengeResult JSON blob,
// wrapping the error in the read-model's error style.
func unmarshalResult(raw []byte, res *ChallengeResult) error {
	if err := json.Unmarshal(raw, res); err != nil {
		return fmt.Errorf("operator: unmarshal challenge result: %w", err)
	}
	return nil
}

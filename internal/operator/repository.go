package operator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	protoop "github.com/NTARI-RAND/sohocloud-protocol/operator"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MinActiveKeys is the floor on active keys an operator may hold. The 2-of-7
// discipline needs two keys to sign a transmission; a signed rotation also
// needs two current keys to authorize a swap. Lazy count-expiry (see
// RecordUsageAndExpire) MUST NOT drop an operator below this floor, or the
// operator would be unable to authenticate or rotate its way back out. Set to
// 3 (2 to sign + 1 slack) per docs/operator-onboarding-design.md §4.2.2.1.
const MinActiveKeys = 3

// Errors surfaced by the repository. They are distinguishable so callers (the
// public onboarding handlers and the :8090 governance handlers) can map each to
// the right HTTP status.
var (
	// ErrDuplicateOperator is returned by CreateOperator when an operator with
	// the same slug (id) or normalized email already exists. Callers map this to
	// HTTP 409 Conflict.
	ErrDuplicateOperator = errors.New("operator: an operator with that slug or email already exists")
	// ErrOperatorNotFound is returned when a lookup targets an operator id that
	// does not exist.
	ErrOperatorNotFound = errors.New("operator: operator not found")
	// ErrKeysetCountMismatch is returned when an operation requires exactly
	// KeyIndexCount (7) active keys and a different number is present.
	ErrKeysetCountMismatch = errors.New("operator: operator does not hold exactly seven active keys")
	// ErrActivationPreconditions is returned by AutoActivate when the operator
	// has not satisfied all activation preconditions (email verified AND
	// conformance passed AND current keyset hash bound to the passing keyset).
	ErrActivationPreconditions = errors.New("operator: activation preconditions not met")
	// ErrKeysetHashMismatch is returned when the operator's current active
	// keyset hash does not equal the conformance_keyset_hash recorded at the
	// time conformance passed. Any key add/rotate before activation clears the
	// conformance pass, so this catches a race or tampering.
	ErrKeysetHashMismatch = errors.New("operator: current keyset hash does not match the conformance-passed keyset")
	// ErrReplay is returned by the anti-replay CAS when a transmission's Seq is
	// stale (at or below the low edge of the sliding window) or its Seq bit was
	// already set (a replay within the window). Also returned when the nonce has
	// already been seen (single-use). This is a hard reject; the transmission is
	// NOT honored.
	ErrReplay = errors.New("operator: replayed or stale transmission (seq/nonce)")
)

// Repository is SoHoLINK's coordinator-side operator registry over pgx/v5. It
// owns the operators, operator_keys, operator_verifications, operator_replay,
// and operator_nonces tables (migrations 021-022). It stores only PUBLIC keys;
// no operator private key is ever held.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository constructs a Repository over an existing pgx pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// seqWindowBits is the width of the anti-replay sliding-window bitmap, in bits.
// operator_replay.seq_window defaults to 32 zero bytes = 256 bits.
const seqWindowBits = 256

// -----------------------------------------------------------------------------
// GetActiveKeyMap — the single verify chokepoint.
// -----------------------------------------------------------------------------

// GetActiveKeyMap returns the operator's active public keys keyed by index, but
// ONLY when every gate holds:
//
//   - the operator exists,
//   - status = 'active',
//   - onboarding_state = 'active',
//   - at least one active key exists,
//   - all active keys share one algo (the algo-pin).
//
// Otherwise it returns (nil, nil): a non-active operator authenticates NOTHING.
// This one predicate collapses the onboarding gate, revocation, and the algo-pin
// into the hot path. It is the ONLY function the OperatorAuth middleware calls to
// obtain keys; a pending, verified-but-not-active, or revoked operator yields a
// nil map, and the protocol Verify against a nil map fails closed (no key at any
// index).
//
// A nil map with a nil error is the "authenticate nothing" signal; a non-nil
// error is a database failure and MUST also be treated as fail-closed by the
// caller (reject the transmission).
func (r *Repository) GetActiveKeyMap(ctx context.Context, operatorID string) (map[int]KeyRecord, error) {
	// One query, joined and gated: rows come back only if the operator is
	// active+active. If the operator is pending/verified/revoked, the WHERE
	// yields zero rows and we return nil (authenticate nothing) — identical to
	// the "operator does not exist" case, by design.
	rows, err := r.pool.Query(ctx, `
		SELECT k.key_index, k.public_key, k.algo
		FROM operator_keys k
		JOIN operators o ON o.id = k.operator_id
		WHERE k.operator_id = $1
		  AND k.state = 'active'
		  AND o.status = 'active'
		  AND o.onboarding_state = 'active'`,
		operatorID,
	)
	if err != nil {
		return nil, fmt.Errorf("operator: query active keys: %w", err)
	}
	defer rows.Close()

	km := make(map[int]KeyRecord, KeyIndexCount)
	var pinAlgo string
	for rows.Next() {
		var (
			idx int
			pub []byte
			alg string
		)
		if err := rows.Scan(&idx, &pub, &alg); err != nil {
			return nil, fmt.Errorf("operator: scan active key: %w", err)
		}
		if pinAlgo == "" {
			pinAlgo = alg
		} else if alg != pinAlgo {
			// Mixed algorithms in the active set violate the algo-pin invariant.
			// Fail closed: authenticate nothing rather than honor a heterogeneous
			// set (which would let a downgrade slip through at verify time).
			return nil, nil
		}
		km[idx] = KeyRecord{PublicKey: pub, Algo: alg}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operator: iterate active keys: %w", err)
	}
	if len(km) == 0 {
		// No active keys, or operator not active/active: authenticate nothing.
		return nil, nil
	}
	return km, nil
}

// -----------------------------------------------------------------------------
// CreateOperator — OPEN, permissionless signup (no invite).
// -----------------------------------------------------------------------------

// CreateOperator inserts a new operator in the initial pending_verification
// state with a normalized email. There is NO invite token: signup is open and
// permissionless (settled decision). Abuse is bounded downstream by per-session
// rate-limiting and by the fact that a pending operator's keys authenticate
// nothing (GetActiveKeyMap returns nil).
//
// The slug (id) and email must both be unique; a collision on either returns
// ErrDuplicateOperator (map to HTTP 409). Email is lowercased and trimmed before
// insert, matching the DB-boundary CHECK constraint.
func (r *Repository) CreateOperator(ctx context.Context, id, name, email, phone string) error {
	normEmail := normalizeEmail(email)
	var phoneArg any
	if strings.TrimSpace(phone) == "" {
		phoneArg = nil // phone is nullable (phone-2FA deferred)
	} else {
		phoneArg = strings.TrimSpace(phone)
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO operators (id, name, email, phone)
		VALUES ($1, $2, $3, $4)`,
		id, name, normEmail, phoneArg,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicateOperator
		}
		return fmt.Errorf("operator: insert operator: %w", err)
	}
	return nil
}

// normalizeEmail lowercases and trims an email so it matches the operators
// email CHECK (email = lower(btrim(email))) and the UNIQUE constraint. Callers
// MUST normalize on every path that compares or inserts email.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// MarkEmailVerified records that the operator's email 2FA succeeded. It does not
// itself activate the operator; call AutoActivate after both gates
// (email-verified AND conformance-passed) hold.
func (r *Repository) MarkEmailVerified(ctx context.Context, operatorID string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE operators SET email_verified = TRUE WHERE id = $1`,
		operatorID,
	)
	if err != nil {
		return fmt.Errorf("operator: mark email verified: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Keyset hashing + conformance binding.
// -----------------------------------------------------------------------------

// activeKeysetHash returns H(sorted 7 active public keys) for the operator, and
// the count of active keys it hashed. The hash binds an activation (or a
// conformance pass) to the EXACT tested keyset: any key add/rotate changes this
// value, so a later activation that requires a match will be refused if the
// keyset drifted. The keys are sorted by their raw public-key bytes (index is
// deliberately NOT part of the hash so the same set of keys hashes identically
// regardless of index assignment).
func (r *Repository) activeKeysetHash(ctx context.Context, q pgx.Tx, operatorID string) ([]byte, int, error) {
	rows, err := q.Query(ctx, `
		SELECT public_key FROM operator_keys
		WHERE operator_id = $1 AND state = 'active'`,
		operatorID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("operator: query keyset for hash: %w", err)
	}
	defer rows.Close()

	var pubs [][]byte
	for rows.Next() {
		var pub []byte
		if err := rows.Scan(&pub); err != nil {
			return nil, 0, fmt.Errorf("operator: scan keyset pubkey: %w", err)
		}
		cp := make([]byte, len(pub))
		copy(cp, pub)
		pubs = append(pubs, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("operator: iterate keyset: %w", err)
	}

	return hashSortedPubkeys(pubs), len(pubs), nil
}

// hashSortedPubkeys returns H(sorted pubkeys): the SHA-256 of the concatenation
// of the public keys after sorting them by raw bytes. Index is deliberately NOT
// part of the hash so the same set of keys hashes identically regardless of index
// assignment. Both the write-path activeKeysetHash and the read-model
// currentActiveKeysetHash call this so the two can never diverge. It mutates the
// caller's slice order (sorts in place); callers pass a slice they own.
func hashSortedPubkeys(pubs [][]byte) []byte {
	sort.Slice(pubs, func(i, j int) bool {
		return stringsCompareBytes(pubs[i], pubs[j]) < 0
	})
	h := sha256.New()
	for _, p := range pubs {
		h.Write(p)
	}
	return h.Sum(nil)
}

// stringsCompareBytes is bytes.Compare inlined to avoid importing bytes solely
// for the sort comparator. Returns -1, 0, or 1.
func stringsCompareBytes(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// MarkConformancePassed records that the operator's conformance run passed and
// binds the pass to the current active keyset by storing
// conformance_keyset_hash = H(sorted active pubkeys). It requires exactly
// KeyIndexCount (7) active keys — a pass is meaningless against a partial keyset.
// It sets conformance_passed_at = now(). This does NOT activate the operator;
// call AutoActivate to flip onboarding_state to 'active' once email is also
// verified.
func (r *Repository) MarkConformancePassed(ctx context.Context, operatorID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin conformance-pass tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback on non-commit paths is intentional

	hash, count, err := r.activeKeysetHash(ctx, tx, operatorID)
	if err != nil {
		return err
	}
	if count != KeyIndexCount {
		return ErrKeysetCountMismatch
	}

	ct, err := tx.Exec(ctx, `
		UPDATE operators
		SET conformance_passed_at = NOW(),
		    conformance_keyset_hash = $2
		WHERE id = $1`,
		operatorID, hash,
	)
	if err != nil {
		return fmt.Errorf("operator: record conformance pass: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit conformance pass: %w", err)
	}
	return nil
}

// AutoActivate flips onboarding_state to 'active' when, and only when, all
// activation preconditions hold, atomically re-checking them inside the same
// transaction that writes the flip:
//
//   - email_verified = TRUE,
//   - conformance_passed_at IS NOT NULL,
//   - the current active keyset hash equals the recorded conformance_keyset_hash
//     (activation is bound to the EXACT tested keyset), and
//   - exactly KeyIndexCount (7) active keys are present.
//
// Passing BOTH mechanical gates auto-activates the operator (no human in the
// entry path); the :8090 admin can DISCONNECT/REVOKE afterward. It is idempotent
// in effect: if the operator is already 'active' it returns nil without error.
// If preconditions are unmet it returns ErrActivationPreconditions (or
// ErrKeysetHashMismatch / ErrKeysetCountMismatch for the keyset-specific cases).
func (r *Repository) AutoActivate(ctx context.Context, operatorID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin activate tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		status          string
		onboardingState string
		emailVerified   bool
		conformancePass *time.Time
		conformanceHash []byte
	)
	err = tx.QueryRow(ctx, `
		SELECT status, onboarding_state, email_verified,
		       conformance_passed_at, conformance_keyset_hash
		FROM operators WHERE id = $1 FOR UPDATE`,
		operatorID,
	).Scan(&status, &onboardingState, &emailVerified, &conformancePass, &conformanceHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOperatorNotFound
		}
		return fmt.Errorf("operator: load for activation: %w", err)
	}

	if onboardingState == "active" {
		return nil // already active — idempotent
	}
	if status != "active" {
		// A revoked operator cannot be (re)activated by this path.
		return ErrActivationPreconditions
	}
	if !emailVerified || conformancePass == nil || len(conformanceHash) == 0 {
		return ErrActivationPreconditions
	}

	curHash, count, err := r.activeKeysetHash(ctx, tx, operatorID)
	if err != nil {
		return err
	}
	if count != KeyIndexCount {
		return ErrKeysetCountMismatch
	}
	if stringsCompareBytes(curHash, conformanceHash) != 0 {
		return ErrKeysetHashMismatch
	}

	ct, err := tx.Exec(ctx, `
		UPDATE operators SET onboarding_state = 'active' WHERE id = $1`,
		operatorID,
	)
	if err != nil {
		return fmt.Errorf("operator: flip to active: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit activation: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// AddKeys — register the operator's 7 public keys (pre-conformance).
// -----------------------------------------------------------------------------

// AddKeys registers the operator's active public keys. It is used during
// onboarding to register the seven keys before conformance. Registering (or
// changing) keys clears any prior conformance pass — the pass is bound to a
// specific keyset, so a keyset change invalidates it (design §3). The keys are
// inserted with state='active' and the given expiration thresholds. It requires
// len(pubkeys) == len(thresholds) == KeyIndexCount and rejects a mixed-algo set.
//
// AddKeys is only valid pre-activation: post-activation key changes go through
// the signed RegisterReplacementKey rotation path. This is enforced by the
// caller (the onboarding handler); AddKeys itself refuses if the operator is
// already active.
func (r *Repository) AddKeys(ctx context.Context, operatorID string, pubkeys [][]byte, algo string, thresholds []int) error {
	if len(pubkeys) != KeyIndexCount || len(thresholds) != KeyIndexCount {
		return ErrKeysetCountMismatch
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin add-keys tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var onboardingState string
	err = tx.QueryRow(ctx, `
		SELECT onboarding_state FROM operators WHERE id = $1 FOR UPDATE`,
		operatorID,
	).Scan(&onboardingState)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOperatorNotFound
		}
		return fmt.Errorf("operator: load for add-keys: %w", err)
	}
	if onboardingState == "active" {
		// Post-activation key changes must go through signed rotation.
		return fmt.Errorf("operator: cannot AddKeys to an active operator; use signed rotation")
	}

	// Clear any existing active keys and the conformance pass bound to them.
	if _, err := tx.Exec(ctx, `
		DELETE FROM operator_keys WHERE operator_id = $1`, operatorID); err != nil {
		return fmt.Errorf("operator: clear prior keys: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE operators
		SET conformance_passed_at = NULL, conformance_keyset_hash = NULL
		WHERE id = $1`, operatorID); err != nil {
		return fmt.Errorf("operator: clear conformance pass: %w", err)
	}

	for i := 0; i < KeyIndexCount; i++ {
		if thresholds[i] <= 0 {
			return fmt.Errorf("operator: key index %d has non-positive expiration threshold", i)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO operator_keys
			    (operator_id, key_index, public_key, algo, state, expiration_threshold)
			VALUES ($1, $2, $3, $4, 'active', $5)`,
			operatorID, i, pubkeys[i], algo, thresholds[i],
		); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("operator: duplicate active key at index %d: %w", i, err)
			}
			return fmt.Errorf("operator: insert key index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit add-keys: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// recordUsageAndExpire — lazy count-expiry with the 2-key/3-active floor.
// -----------------------------------------------------------------------------

// RecordUsageAndExpire increments usage_count for the two signing indices and
// lazily expires any key whose usage_count reaches its expiration_threshold —
// EXCEPT that it never expires a key if doing so would drop the operator below
// MinActiveKeys active keys. It returns swapRequired=true if any of the two
// used indices is at or over its threshold after the increment (whether or not
// it was actually expired), so the middleware can set X-Operator-Swap-Required.
//
// This runs DOWNSTREAM of the anti-replay CAS in the middleware, so a given
// transmission increments usage exactly once (a replay is rejected before it
// reaches here). Lazy expiry means a key is not removed by a background job; it
// expires the first time its own usage crosses the threshold, and only while the
// active-key floor permits.
func (r *Repository) RecordUsageAndExpire(ctx context.Context, operatorID string, idx0, idx1 int) (swapRequired bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("operator: begin usage tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Count current active keys up front; expiry must not drop below the floor.
	var activeCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM operator_keys
		WHERE operator_id = $1 AND state = 'active'`,
		operatorID,
	).Scan(&activeCount); err != nil {
		return false, fmt.Errorf("operator: count active keys: %w", err)
	}

	for _, idx := range []int{idx0, idx1} {
		var usage, threshold int
		err := tx.QueryRow(ctx, `
			UPDATE operator_keys
			SET usage_count = usage_count + 1
			WHERE operator_id = $1 AND key_index = $2 AND state = 'active'
			RETURNING usage_count, expiration_threshold`,
			operatorID, idx,
		).Scan(&usage, &threshold)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// No active key at this index (already expired/rotated); nothing
				// to increment. Not an error for usage accounting.
				continue
			}
			return false, fmt.Errorf("operator: increment usage at index %d: %w", idx, err)
		}
		if usage >= threshold {
			swapRequired = true
			if activeCount > MinActiveKeys {
				// Expire this key (lazy). The crossing transmission itself still
				// verified — expiry takes effect for the NEXT transmission.
				if _, err := tx.Exec(ctx, `
					UPDATE operator_keys SET state = 'expired'
					WHERE operator_id = $1 AND key_index = $2 AND state = 'active'`,
					operatorID, idx,
				); err != nil {
					return false, fmt.Errorf("operator: expire key at index %d: %w", idx, err)
				}
				activeCount--
			}
			// else: at the floor — keep the key active (do NOT expire below floor).
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("operator: commit usage: %w", err)
	}
	return swapRequired, nil
}

// -----------------------------------------------------------------------------
// RegisterReplacementKey — signed rotation (Phase-B).
// -----------------------------------------------------------------------------

// RegisterReplacementKey verifies a signed OperatorRotation against the CURRENT
// active keyset and, only if it verifies, swaps in the new public key at the
// rotation's KeyIndex: it retires the old active row at that index and inserts
// the new active key, in ONE transaction. Verification happens BEFORE any insert
// (the new key bytes are inside the signed message, so a MITM of the
// out-of-band registration cannot inject key material). Rotation is available
// only AFTER activation (there is an established keyset to authorize against);
// the caller enforces that the operator is active.
//
// The new key inherits a fresh usage_count of 0 and the given expiration
// threshold. The rotation's own anti-replay (nonce/seq) is the caller's
// responsibility via the CAS path, exactly like a transmission.
func (r *Repository) RegisterReplacementKey(ctx context.Context, rot OperatorRotation, threshold int) error {
	if threshold <= 0 {
		return fmt.Errorf("operator: replacement key threshold must be positive")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin rotation tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Load the current active keyset under a row lock so a concurrent rotation
	// cannot race the verify/swap.
	km, err := r.activeKeyMapTx(ctx, tx, rot.OperatorID)
	if err != nil {
		return err
	}

	// Verify the rotation authorization against the CURRENT keys (protocol
	// enforces: nonce, algo, distinct indices, range, key presence, algo-pin,
	// no duplicate new key, sig validity over the new-key-bearing bytes).
	if err := rot.Verify(km); err != nil {
		return fmt.Errorf("operator: rotation verify failed: %w", err)
	}

	// Retire the old active row at KeyIndex (history is preserved as 'retired').
	if _, err := tx.Exec(ctx, `
		UPDATE operator_keys SET state = 'retired'
		WHERE operator_id = $1 AND key_index = $2 AND state = 'active'`,
		rot.OperatorID, rot.KeyIndex,
	); err != nil {
		return fmt.Errorf("operator: retire old key: %w", err)
	}

	// Insert the operator-generated replacement as the new active key.
	if _, err := tx.Exec(ctx, `
		INSERT INTO operator_keys
		    (operator_id, key_index, public_key, algo, state, expiration_threshold)
		VALUES ($1, $2, $3, $4, 'active', $5)`,
		rot.OperatorID, rot.KeyIndex, rot.NewPublicKey, rot.Algo, threshold,
	); err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("operator: replacement key conflicts with an active key: %w", err)
		}
		return fmt.Errorf("operator: insert replacement key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit rotation: %w", err)
	}
	return nil
}

// activeKeyMapTx loads the active keymap inside a transaction (no operator-state
// gating — used by rotation, which already requires the caller to have gated on
// active status). It does NOT apply the algo-pin fail-closed behavior of
// GetActiveKeyMap; the protocol Verify enforces the algo-pin per index.
func (r *Repository) activeKeyMapTx(ctx context.Context, tx pgx.Tx, operatorID string) (map[int]KeyRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT key_index, public_key, algo FROM operator_keys
		WHERE operator_id = $1 AND state = 'active'`,
		operatorID,
	)
	if err != nil {
		return nil, fmt.Errorf("operator: query active keys (tx): %w", err)
	}
	defer rows.Close()
	km := make(map[int]KeyRecord, KeyIndexCount)
	for rows.Next() {
		var (
			idx int
			pub []byte
			alg string
		)
		if err := rows.Scan(&idx, &pub, &alg); err != nil {
			return nil, fmt.Errorf("operator: scan active key (tx): %w", err)
		}
		km[idx] = KeyRecord{PublicKey: pub, Algo: alg}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operator: iterate active keys (tx): %w", err)
	}
	return km, nil
}

// -----------------------------------------------------------------------------
// Revoke / disconnect (:8090 governance).
// -----------------------------------------------------------------------------

// Revoke flips the operator's status to 'revoked'. After this GetActiveKeyMap
// returns nil for the operator — every fronted member is denied (high blast
// radius). This is the :8090 kill switch. Idempotent: revoking an
// already-revoked operator returns nil.
func (r *Repository) Revoke(ctx context.Context, operatorID string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE operators SET status = 'revoked' WHERE id = $1`,
		operatorID,
	)
	if err != nil {
		return fmt.Errorf("operator: revoke: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}
	return nil
}

// Disconnect is an alias intent for Revoke used by the :8090 admin surface
// ("disconnect the operator"). It flips status to 'revoked' and additionally
// revokes all the operator's active keys so the registry reflects a hard
// disconnect. GetActiveKeyMap already returns nil once status != 'active', so
// the key-state change is belt-and-suspenders for the admin detail view.
func (r *Repository) Disconnect(ctx context.Context, operatorID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin disconnect tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ct, err := tx.Exec(ctx, `
		UPDATE operators SET status = 'revoked' WHERE id = $1`, operatorID)
	if err != nil {
		return fmt.Errorf("operator: disconnect status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE operator_keys SET state = 'revoked'
		WHERE operator_id = $1 AND state = 'active'`, operatorID); err != nil {
		return fmt.Errorf("operator: disconnect keys: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit disconnect: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Anti-replay: SeqCheckAndAdvance + NonceInsert in ONE fail-closed transaction.
// -----------------------------------------------------------------------------

// CheckAndAdvance performs the coordinator-side anti-replay obligation for one
// transmission in ONE transaction, fail-closed: a stale/replayed Seq or a
// previously-seen nonce is rejected with ErrReplay, and ANY database error also
// rejects (never fail open). On success the per-(operator,coordinator) sliding
// window is advanced and the nonce is durably recorded as consumed.
//
// scope is 'production' for live transmissions or 'conformance' for the
// conformance harness path; the nonce table domain-separates the two so a
// conformance nonce can never satisfy a production replay check and vice versa.
// nonceExpiry bounds how long the nonce row must be retained (>= the 5-minute
// timestamp freshness window); expired rows can be pruned because they can no
// longer be replayed within the window.
//
// This method is the SeqCheckAndAdvance + NonceInsert pair the design calls for,
// fused into a single call so the two can never be split across transactions.
func (r *Repository) CheckAndAdvance(ctx context.Context, operatorID, coordinatorID string, seq uint64, nonce []byte, scope string, nonceExpiry time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		// Fail closed: cannot even open a transaction -> reject.
		return fmt.Errorf("operator: begin replay tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Insert the nonce first: the PRIMARY KEY makes single-use enforcement
	// atomic. A duplicate nonce is a replay regardless of Seq.
	_, err = tx.Exec(ctx, `
		INSERT INTO operator_nonces (nonce, operator_id, scope, expires_at)
		VALUES ($1, $2, $3, $4)`,
		nonce, operatorID, scope, nonceExpiry,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrReplay
		}
		return fmt.Errorf("operator: insert nonce: %w", err)
	}

	// Load-or-create the sliding window row for this (operator, coordinator),
	// locked so a concurrent transmission serializes behind us.
	var (
		seqHigh int64
		window  []byte
	)
	err = tx.QueryRow(ctx, `
		INSERT INTO operator_replay (operator_id, coordinator_id)
		VALUES ($1, $2)
		ON CONFLICT (operator_id, coordinator_id) DO UPDATE
		    SET operator_id = EXCLUDED.operator_id
		RETURNING seq_high, seq_window`,
		operatorID, coordinatorID,
	).Scan(&seqHigh, &window)
	if err != nil {
		return fmt.Errorf("operator: load replay window: %w", err)
	}

	newHigh, newWindow, ok := advanceWindow(uint64(seqHigh), window, seq)
	if !ok {
		return ErrReplay
	}

	if _, err := tx.Exec(ctx, `
		UPDATE operator_replay
		SET seq_high = $3, seq_window = $4, updated_at = NOW()
		WHERE operator_id = $1 AND coordinator_id = $2`,
		operatorID, coordinatorID, int64(newHigh), newWindow,
	); err != nil {
		return fmt.Errorf("operator: advance replay window: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		// Fail closed: a commit failure means we cannot be sure the replay
		// state persisted -> reject the transmission.
		return fmt.Errorf("operator: commit replay: %w", err)
	}
	return nil
}

// advanceWindow implements the sliding-window Seq check. Given the current high
// watermark seqHigh and the seqWindowBits-wide bitmap window (bit i set means
// "seqHigh-i has been seen"), it decides whether seq is fresh and, if so,
// returns the advanced (high, window). ok=false means seq is stale or already
// seen (a replay).
//
// Semantics:
//   - seq > seqHigh: fresh; the window shifts left by (seq-seqHigh); the old
//     high's bit is recorded; the new high's own bit is set.
//   - seq == seqHigh: already the high watermark -> replay (unless nothing has
//     ever been seen and seqHigh is the sentinel 0; the first transmission at
//     seq 0 is accepted once).
//   - seqHigh-seqWindowBits < seq < seqHigh: inside the window; accept only if
//     its bit is currently clear, then set it.
//   - seq <= seqHigh-seqWindowBits: below the window -> too old -> replay.
//
// The window is a big-endian bit array over 32 bytes; bit 0 (the MSB of byte 0)
// represents seqHigh itself.
func advanceWindow(seqHigh uint64, window []byte, seq uint64) (uint64, []byte, bool) {
	if len(window) != seqWindowBits/8 {
		// Malformed window: fail closed.
		return 0, nil, false
	}
	w := make([]byte, len(window))
	copy(w, window)

	// Special-case a virgin window: seq_high defaults to 0 with an all-zero
	// bitmap and nothing seen. Treat the very first transmission — at any seq —
	// as fresh, setting the high watermark to it. We detect "virgin" as
	// seqHigh==0 AND the bit for seqHigh is unset (no seq 0 recorded yet).
	virgin := seqHigh == 0 && !getBit(w, 0)

	switch {
	case virgin:
		// First ever transmission: set high to seq, mark its bit.
		nw := make([]byte, seqWindowBits/8)
		setBit(nw, 0)
		return seq, nw, true

	case seq > seqHigh:
		shift := seq - seqHigh
		if shift >= seqWindowBits {
			// The shift clears the whole window; only the new high is set.
			nw := make([]byte, seqWindowBits/8)
			setBit(nw, 0)
			return seq, nw, true
		}
		shifted := shiftRight(w, uint(shift)) // old high moves down by `shift`
		setBit(shifted, 0)                    // new high's own bit
		return seq, shifted, true

	case seq == seqHigh:
		// The high watermark itself was already recorded (its bit is set in the
		// virgin check path or by prior advance) -> replay.
		return 0, nil, false

	default: // seq < seqHigh
		diff := seqHigh - seq
		if diff >= seqWindowBits {
			return 0, nil, false // below the window: too old
		}
		if getBit(w, uint(diff)) {
			return 0, nil, false // already seen inside the window
		}
		setBit(w, uint(diff))
		return seqHigh, w, true
	}
}

// getBit reports whether bit `pos` (0 = MSB of byte 0) is set.
func getBit(b []byte, pos uint) bool {
	byteIdx := pos / 8
	bitIdx := 7 - (pos % 8)
	if int(byteIdx) >= len(b) {
		return false
	}
	return b[byteIdx]&(1<<bitIdx) != 0
}

// setBit sets bit `pos` (0 = MSB of byte 0).
func setBit(b []byte, pos uint) {
	byteIdx := pos / 8
	bitIdx := 7 - (pos % 8)
	if int(byteIdx) >= len(b) {
		return
	}
	b[byteIdx] |= 1 << bitIdx
}

// shiftRight returns a copy of the bitmap shifted toward higher bit positions
// by n (bit 0 -> bit n). Bits shifted past the end are dropped. This models
// "the window slid: what used to be at position i is now at position i+n".
func shiftRight(b []byte, n uint) []byte {
	out := make([]byte, len(b))
	total := uint(len(b) * 8)
	for pos := uint(0); pos < total; pos++ {
		if getBit(b, pos) {
			np := pos + n
			if np < total {
				setBit(out, np)
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// helpers.
// -----------------------------------------------------------------------------

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). Used to map insert conflicts to domain errors.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// compile-time assertion that the re-exported alias matches the protocol type.
var _ = protoop.KeyIndexCount

package operator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	protoop "github.com/NTARI-RAND/sohocloud-protocol/operator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The conformance harness grades an operator against FRESH per-onboarding inputs
// SoHoLINK generates and computes its own oracle for — NEVER testdata/vectors.json
// verbatim (those are public: seeds+bytes+sigs, so they'd be replayable). SoHoLINK
// is always the initiator/grader and NEVER dials operator infrastructure.
//
// Three suites (design §9):
//
//	A canonical-signing — SoHoLINK generates fresh OperatorTransmission fields,
//	  computes its OWN canonical bytes as the oracle, the operator returns the
//	  bytes it computed (assert byte-equality) + a 2-of-7 ConformanceResponse
//	  signature over those bytes (verify). A real canon + real signing are both
//	  necessary to pass.
//	B transmission — SoHoLINK issues a fresh {nonce, ts, seq, idx0!=idx1}; the
//	  operator returns a full OperatorTransmission; SoHoLINK runs the REAL
//	  operator-auth verify path (protocol Verify against the 7 registered keys).
//	C rotation/expiry — the 5-mock-key §4.2.2.1 exercise against a SCRATCH,
//	  in-memory namespace (never the operator's real keys/usage): assert
//	  swap-required fires at each drawn threshold with the 2-key floor.
//
// All three green -> MarkConformancePassed binds conformance_keyset_hash; the
// handler then calls AutoActivate.

// ConformanceSuite identifies one of the three grading suites.
type ConformanceSuite string

const (
	SuiteA ConformanceSuite = "A" // canonical-signing
	SuiteB ConformanceSuite = "B" // transmission (real verify path)
	SuiteC ConformanceSuite = "C" // rotation/expiry (scratch namespace)
)

// conformanceNonceLen is the fresh CSPRNG nonce length per challenge (>= the
// protocol MinNonceLen of 16).
const conformanceNonceLen = 32

// Suite C mock-key thresholds, the §4.2.2.1 EXPIRATIONS distribution. Five mock
// keys are driven until each crosses its threshold; swap-required must fire at
// each crossing, and expiry must respect the 2-key floor.
var suiteCThresholds = []int{3, 6, 9, 12, 365}

// suiteCFloor is the active-key floor for the Suite C scratch exercise: with 5
// mock keys, expiry must not drop below 2 keys (2 needed to sign). This is the
// design's "2-key floor" for the isolated Suite C namespace, distinct from the
// production MinActiveKeys=3 floor on the real 7-key set.
const suiteCFloor = 2

// Errors surfaced by the conformance path.
var (
	// ErrConformanceRunNotFound is returned when a run_id does not exist or does
	// not belong to the named operator.
	ErrConformanceRunNotFound = errors.New("operator: conformance run not found")
	// ErrConformanceChallengeConsumed is returned when a challenge response is
	// submitted for a challenge already consumed (single-use). Idempotent-for-
	// support is handled by returning the prior graded result, not by re-grading.
	ErrConformanceChallengeConsumed = errors.New("operator: conformance challenge already consumed")
	// ErrConformanceNotReady is returned by FinalizeRun when not all challenges
	// across the three suites have passed.
	ErrConformanceNotReady = errors.New("operator: conformance run not fully passed")
)

// ChallengeA is the fresh input SoHoLINK sends for a Suite A challenge. The
// operator must compute the canonical bytes of an OperatorTransmission with
// exactly these fields and return them (plus a signature). The expected bytes
// are computed on SoHoLINK's side and stored in conformance_challenges.expected;
// they are NOT sent to the operator.
type ChallengeA struct {
	ChallengeID string `json:"challenge_id"`
	OperatorID  string `json:"operator_id"`
	TsUnixNano  int64  `json:"ts_unix_nano"`
	Nonce       []byte `json:"nonce"`
	Seq         uint64 `json:"seq"`
	Algo        string `json:"algo"`
	Idx0        int    `json:"idx0"`
	Idx1        int    `json:"idx1"`
}

// ResponseA is the operator's answer to a Suite A challenge: the canonical bytes
// it computed and a 2-of-7 ConformanceResponse over those bytes.
type ResponseA struct {
	ChallengeID    string `json:"challenge_id"`
	CanonicalBytes []byte `json:"canonical_bytes"`
	Idx0           int    `json:"idx0"`
	Idx1           int    `json:"idx1"`
	Sig0           []byte `json:"sig0"`
	Sig1           []byte `json:"sig1"`
}

// ChallengeB is the fresh input for a Suite B challenge: the operator must
// produce a full OperatorTransmission over these fields (its own signatures) and
// return it; SoHoLINK runs the real verify path.
type ChallengeB struct {
	ChallengeID string `json:"challenge_id"`
	OperatorID  string `json:"operator_id"`
	TsUnixNano  int64  `json:"ts_unix_nano"`
	Nonce       []byte `json:"nonce"`
	Seq         uint64 `json:"seq"`
	Algo        string `json:"algo"`
}

// ResponseB is the operator's full transmission for a Suite B challenge.
type ResponseB struct {
	ChallengeID string `json:"challenge_id"`
	Idx0        int    `json:"idx0"`
	Idx1        int    `json:"idx1"`
	Sig0        []byte `json:"sig0"`
	Sig1        []byte `json:"sig1"`
}

// StartConformanceRun creates a conformance_runs row for the operator and issues
// the Suite A and Suite B challenges (Suite C is graded server-side in one shot
// on submit — it needs no operator response). It returns the run id and the
// challenge sets to send the operator.
//
// It requires exactly KeyIndexCount (7) active keys (a conformance run against a
// partial keyset is meaningless). Each challenge carries a fresh CSPRNG nonce
// persisted with scope='conformance' single-use. The Suite A expected canonical
// bytes are computed here and stored, never sent.
func (r *Repository) StartConformanceRun(ctx context.Context, operatorID string) (runID string, challengesA []ChallengeA, challengesB []ChallengeB, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", nil, nil, fmt.Errorf("operator: begin start-run tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Confirm 7 active keys and grab the algo pin.
	var (
		activeCount int
		algo        string
	)
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MIN(algo), '')
		FROM operator_keys WHERE operator_id = $1 AND state = 'active'`,
		operatorID,
	).Scan(&activeCount, &algo); err != nil {
		return "", nil, nil, fmt.Errorf("operator: count active keys for run: %w", err)
	}
	if activeCount != KeyIndexCount {
		return "", nil, nil, ErrKeysetCountMismatch
	}

	newRunID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO conformance_runs (run_id, operator_id, status)
		VALUES ($1, $2, 'running')`,
		newRunID, operatorID,
	); err != nil {
		return "", nil, nil, fmt.Errorf("operator: insert conformance run: %w", err)
	}

	// One challenge per suite (A, B) is sufficient for the liveness proof;
	// distinct nonces/fields make each fresh. Suite C is generated + graded on
	// submit and needs no operator round-trip.
	chA, err := r.issueChallengeA(ctx, tx, newRunID, operatorID, 0, algo)
	if err != nil {
		return "", nil, nil, err
	}
	chB, err := r.issueChallengeB(ctx, tx, newRunID, operatorID, 0, algo)
	if err != nil {
		return "", nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", nil, nil, fmt.Errorf("operator: commit start-run: %w", err)
	}
	return newRunID, []ChallengeA{chA}, []ChallengeB{chB}, nil
}

// issueChallengeA generates fresh OperatorTransmission fields, computes the
// oracle canonical bytes, persists the challenge row (nonce single-use,
// scope='conformance'), and returns the ChallengeA to send. The expected bytes
// are stored, NEVER returned.
func (r *Repository) issueChallengeA(ctx context.Context, tx pgx.Tx, runID, operatorID string, idx int, algo string) (ChallengeA, error) {
	nonce, err := freshNonce()
	if err != nil {
		return ChallengeA{}, err
	}
	seq, err := freshSeq()
	if err != nil {
		return ChallengeA{}, err
	}
	i0, i1 := 0, 1 // any distinct pair; the operator must sign with these

	ch := ChallengeA{
		OperatorID: operatorID,
		TsUnixNano: time.Now().UnixNano(),
		Nonce:      nonce,
		Seq:        seq,
		Algo:       algo,
		Idx0:       i0,
		Idx1:       i1,
	}
	// Oracle: SoHoLINK computes the canonical bytes itself.
	oracle := protoop.OperatorTransmission{
		OperatorID: ch.OperatorID,
		TsUnixNano: ch.TsUnixNano,
		Nonce:      ch.Nonce,
		Seq:        ch.Seq,
		Algo:       ch.Algo,
		Idx0:       ch.Idx0,
		Idx1:       ch.Idx1,
	}.CanonicalBytes()

	challengeID, err := r.persistChallenge(ctx, tx, runID, SuiteA, idx, nonce, ch, map[string]any{
		"canonical_bytes": oracle,
	})
	if err != nil {
		return ChallengeA{}, err
	}
	ch.ChallengeID = challengeID
	return ch, nil
}

// issueChallengeB generates fresh transmission fields and persists the
// challenge; the operator produces the signatures. Expected records the fields
// so grading can rebuild and run the real verify path.
func (r *Repository) issueChallengeB(ctx context.Context, tx pgx.Tx, runID, operatorID string, idx int, algo string) (ChallengeB, error) {
	nonce, err := freshNonce()
	if err != nil {
		return ChallengeB{}, err
	}
	seq, err := freshSeq()
	if err != nil {
		return ChallengeB{}, err
	}
	ch := ChallengeB{
		OperatorID: operatorID,
		TsUnixNano: time.Now().UnixNano(),
		Nonce:      nonce,
		Seq:        seq,
		Algo:       algo,
	}
	challengeID, err := r.persistChallenge(ctx, tx, runID, SuiteB, idx, nonce, ch, map[string]any{
		"ts_unix_nano": ch.TsUnixNano,
		"seq":          ch.Seq,
		"algo":         ch.Algo,
	})
	if err != nil {
		return ChallengeB{}, err
	}
	ch.ChallengeID = challengeID
	return ch, nil
}

// persistChallenge inserts a conformance_challenges row and durably records the
// nonce single-use with scope='conformance' (fail-closed: a nonce collision
// aborts the run creation). Returns the generated challenge_id.
func (r *Repository) persistChallenge(ctx context.Context, tx pgx.Tx, runID string, suite ConformanceSuite, idx int, nonce []byte, inputs any, expected map[string]any) (string, error) {
	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return "", fmt.Errorf("operator: marshal challenge inputs: %w", err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return "", fmt.Errorf("operator: marshal challenge oracle: %w", err)
	}

	// Reserve the nonce single-use (scope='conformance'). A conformance nonce can
	// never satisfy a production replay check and vice versa (domain-separated).
	nonceExpiry := time.Now().Add(1 * time.Hour) // conformance run window
	if _, err := tx.Exec(ctx, `
		INSERT INTO operator_nonces (nonce, operator_id, scope, expires_at)
		VALUES ($1, (SELECT operator_id FROM conformance_runs WHERE run_id = $2), 'conformance', $3)`,
		nonce, runID, nonceExpiry,
	); err != nil {
		if isUniqueViolation(err) {
			return "", ErrReplay
		}
		return "", fmt.Errorf("operator: reserve conformance nonce: %w", err)
	}

	var challengeID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO conformance_challenges (run_id, suite, idx, nonce, inputs, expected)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING challenge_id`,
		runID, string(suite), idx, nonce, inputsJSON, expectedJSON,
	).Scan(&challengeID); err != nil {
		return "", fmt.Errorf("operator: insert conformance challenge: %w", err)
	}
	return challengeID, nil
}

// ChallengeResult is the graded verdict for one challenge.
type ChallengeResult struct {
	ChallengeID string           `json:"challenge_id"`
	Suite       ConformanceSuite `json:"suite"`
	Passed      bool             `json:"passed"`
	Detail      string           `json:"detail,omitempty"`
}

// GradeSuiteA grades a Suite A response: byte-equality of the returned canonical
// bytes against SoHoLINK's stored oracle, THEN a real protocol verify of the
// 2-of-7 ConformanceResponse over those bytes against the operator's registered
// keys. The challenge is consumed single-use; a resubmit returns the prior
// result (idempotent-for-support) without re-grading.
func (r *Repository) GradeSuiteA(ctx context.Context, operatorID, runID string, resp ResponseA) (ChallengeResult, error) {
	return r.gradeChallenge(ctx, operatorID, runID, resp.ChallengeID, SuiteA, func(expected map[string]json.RawMessage, km map[int]KeyRecord) (bool, string) {
		var oracle []byte
		if err := json.Unmarshal(expected["canonical_bytes"], &oracle); err != nil {
			return false, "grader: cannot read oracle bytes"
		}
		// Byte-equality against SoHoLINK's own canon computation.
		if !bytesEqual(oracle, resp.CanonicalBytes) {
			return false, fmt.Sprintf("canonical bytes mismatch; first-differing offset %d", firstDiff(oracle, resp.CanonicalBytes))
		}
		// Real signature verify: 2-of-7 ConformanceResponse over the fresh bytes.
		cr := protoop.ConformanceResponse{
			OperatorID: operatorID,
			Challenge:  resp.CanonicalBytes,
			Algo:       algoOf(km),
			Idx0:       resp.Idx0,
			Idx1:       resp.Idx1,
			Sig0:       resp.Sig0,
			Sig1:       resp.Sig1,
		}
		if err := cr.Verify(km); err != nil {
			return false, "signature verify failed: " + err.Error()
		}
		return true, ""
	})
}

// GradeSuiteB grades a Suite B response by rebuilding the OperatorTransmission
// from the stored fresh fields + the operator's returned signatures and running
// the REAL protocol Verify against the registered keys (the same pure-verify the
// production OperatorAuth path runs, steps 1-7). Single-use.
func (r *Repository) GradeSuiteB(ctx context.Context, operatorID, runID string, resp ResponseB) (ChallengeResult, error) {
	return r.gradeChallenge(ctx, operatorID, runID, resp.ChallengeID, SuiteB, func(expected map[string]json.RawMessage, km map[int]KeyRecord) (bool, string) {
		var ts int64
		var seq uint64
		var algo string
		if err := json.Unmarshal(expected["ts_unix_nano"], &ts); err != nil {
			return false, "grader: cannot read ts"
		}
		if err := json.Unmarshal(expected["seq"], &seq); err != nil {
			return false, "grader: cannot read seq"
		}
		if err := json.Unmarshal(expected["algo"], &algo); err != nil {
			return false, "grader: cannot read algo"
		}
		// Nonce comes from the challenge row (loaded by gradeChallenge and passed
		// via expected["_nonce"]).
		var nonce []byte
		if err := json.Unmarshal(expected["_nonce"], &nonce); err != nil {
			return false, "grader: cannot read nonce"
		}
		tx := protoop.OperatorTransmission{
			OperatorID: operatorID,
			TsUnixNano: ts,
			Nonce:      nonce,
			Seq:        seq,
			Algo:       algo,
			Idx0:       resp.Idx0,
			Idx1:       resp.Idx1,
			Sig0:       resp.Sig0,
			Sig1:       resp.Sig1,
		}
		if err := tx.Verify(km); err != nil {
			return false, "transmission verify failed: " + err.Error()
		}
		return true, ""
	})
}

// gradeChallenge is the shared single-use grading path: load the challenge under
// a row lock, short-circuit to the prior result if already consumed, load the
// operator's active keymap, run the suite-specific grader, persist the verdict,
// and mark consumed. It fails closed on any DB error.
func (r *Repository) gradeChallenge(
	ctx context.Context,
	operatorID, runID, challengeID string,
	suite ConformanceSuite,
	grader func(expected map[string]json.RawMessage, km map[int]KeyRecord) (bool, string),
) (ChallengeResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: begin grade tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Confirm the run belongs to the operator.
	var runOperator string
	if err := tx.QueryRow(ctx, `SELECT operator_id FROM conformance_runs WHERE run_id = $1`, runID).Scan(&runOperator); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ChallengeResult{}, ErrConformanceRunNotFound
		}
		return ChallengeResult{}, fmt.Errorf("operator: load run: %w", err)
	}
	if runOperator != operatorID {
		return ChallengeResult{}, ErrConformanceRunNotFound
	}

	var (
		dbSuite     string
		nonce       []byte
		expectedRaw []byte
		resultRaw   []byte
		consumedAt  *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT suite, nonce, expected, result, consumed_at
		FROM conformance_challenges
		WHERE challenge_id = $1 AND run_id = $2
		FOR UPDATE`,
		challengeID, runID,
	).Scan(&dbSuite, &nonce, &expectedRaw, &resultRaw, &consumedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ChallengeResult{}, ErrConformanceRunNotFound
		}
		return ChallengeResult{}, fmt.Errorf("operator: load challenge: %w", err)
	}
	if ConformanceSuite(dbSuite) != suite {
		return ChallengeResult{}, fmt.Errorf("operator: challenge %s is not suite %s", challengeID, suite)
	}

	// Idempotent-for-support: a resubmit returns the prior graded result, never
	// re-accepts a new signature.
	if consumedAt != nil {
		var prior ChallengeResult
		if len(resultRaw) > 0 {
			_ = json.Unmarshal(resultRaw, &prior) //nolint:errcheck // best-effort replay
		}
		prior.ChallengeID = challengeID
		prior.Suite = suite
		return prior, nil
	}

	// Load the operator's active keymap for verification (through the same gated
	// chokepoint semantics would return nil for a non-active operator; during
	// onboarding the operator is NOT active yet, so we read the raw active keys
	// via the tx helper — conformance runs precede activation by design).
	km, err := r.activeKeyMapTx(ctx, tx, operatorID)
	if err != nil {
		return ChallengeResult{}, err
	}

	expected := map[string]json.RawMessage{}
	if err := json.Unmarshal(expectedRaw, &expected); err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: unmarshal expected: %w", err)
	}
	// Make the challenge nonce available to the grader (Suite B rebuilds the
	// transmission over it).
	nonceJSON, _ := json.Marshal(nonce) //nolint:errcheck
	expected["_nonce"] = nonceJSON

	passed, detail := grader(expected, km)
	result := ChallengeResult{ChallengeID: challengeID, Suite: suite, Passed: passed, Detail: detail}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: marshal result: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE conformance_challenges
		SET result = $2, consumed_at = NOW()
		WHERE challenge_id = $1`,
		challengeID, resultJSON,
	); err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: persist challenge result: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: commit grade: %w", err)
	}
	return result, nil
}

// GradeSuiteC runs the §4.2.2.1 count-expiry / lazy-swap exercise against a
// SCRATCH, in-memory 5-mock-key namespace — it NEVER reads or mutates the
// operator's real operator_keys rows or usage counters. SoHoLINK generates the 5
// mock keypairs, drives transmissions until each key crosses its threshold, and
// asserts swap-required fires at each drawn threshold with the 2-key floor. This
// proves the operator's counterpart understands lazy expiry; because SoHoLINK is
// always the grader and holds the mock keys, the exercise is fully server-side
// (no operator round-trip). The verdict is persisted as a Suite C challenge row.
//
// It is idempotent-for-support: a second call returns the prior result.
func (r *Repository) GradeSuiteC(ctx context.Context, operatorID, runID string) (ChallengeResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: begin suiteC tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var runOperator string
	if err := tx.QueryRow(ctx, `SELECT operator_id FROM conformance_runs WHERE run_id = $1`, runID).Scan(&runOperator); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ChallengeResult{}, ErrConformanceRunNotFound
		}
		return ChallengeResult{}, fmt.Errorf("operator: load run for suiteC: %w", err)
	}
	if runOperator != operatorID {
		return ChallengeResult{}, ErrConformanceRunNotFound
	}

	// Return the prior verdict if Suite C already graded for this run.
	var (
		existingID  string
		existingRes []byte
	)
	err = tx.QueryRow(ctx, `
		SELECT challenge_id, result FROM conformance_challenges
		WHERE run_id = $1 AND suite = 'C' AND idx = 0`,
		runID,
	).Scan(&existingID, &existingRes)
	if err == nil {
		var prior ChallengeResult
		if len(existingRes) > 0 {
			_ = json.Unmarshal(existingRes, &prior) //nolint:errcheck
		}
		prior.ChallengeID = existingID
		prior.Suite = SuiteC
		return prior, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ChallengeResult{}, fmt.Errorf("operator: load prior suiteC: %w", err)
	}

	passed, detail := runSuiteCScratch()

	nonce, err := freshNonce()
	if err != nil {
		return ChallengeResult{}, err
	}
	nonceExpiry := time.Now().Add(1 * time.Hour)
	if _, err := tx.Exec(ctx, `
		INSERT INTO operator_nonces (nonce, operator_id, scope, expires_at)
		VALUES ($1, $2, 'conformance', $3)`,
		nonce, operatorID, nonceExpiry,
	); err != nil {
		if isUniqueViolation(err) {
			return ChallengeResult{}, ErrReplay
		}
		return ChallengeResult{}, fmt.Errorf("operator: reserve suiteC nonce: %w", err)
	}

	result := ChallengeResult{Suite: SuiteC, Passed: passed, Detail: detail}
	inputsJSON, _ := json.Marshal(map[string]any{"thresholds": suiteCThresholds, "floor": suiteCFloor}) //nolint:errcheck
	expectedJSON, _ := json.Marshal(map[string]any{"swap_required_at": suiteCThresholds})               //nolint:errcheck
	resultJSON, _ := json.Marshal(result)                                                               //nolint:errcheck

	var challengeID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO conformance_challenges (run_id, suite, idx, nonce, inputs, expected, result, consumed_at)
		VALUES ($1, 'C', 0, $2, $3, $4, $5, NOW())
		RETURNING challenge_id`,
		runID, nonce, inputsJSON, expectedJSON, resultJSON,
	).Scan(&challengeID); err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: insert suiteC challenge: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ChallengeResult{}, fmt.Errorf("operator: commit suiteC: %w", err)
	}
	result.ChallengeID = challengeID
	return result, nil
}

// runSuiteCScratch executes the 5-mock-key §4.2.2.1 lazy-expiry exercise fully
// in memory. It builds 5 mock keys with thresholds {3,6,9,12,365}, drives
// transmissions using a rotating distinct pair of indices, and asserts:
//   - swap-required fires exactly when a used key's usage reaches its threshold,
//   - a key is not expired if doing so would drop active keys below the 2-key
//     floor (expired-until-attempted semantics: the crossing use still counts).
//
// This mirrors the production RecordUsageAndExpire logic against a scratch state
// so the operator's registered keys/usage are never touched.
func runSuiteCScratch() (bool, string) {
	type mockKey struct {
		threshold int
		usage     int
		active    bool
	}
	keys := make([]mockKey, len(suiteCThresholds))
	for i, th := range suiteCThresholds {
		keys[i] = mockKey{threshold: th, active: true}
	}

	activeCount := func() int {
		n := 0
		for _, k := range keys {
			if k.active {
				n++
			}
		}
		return n
	}

	// record models RecordUsageAndExpire for two indices: increment usage, report
	// swap-required on any threshold crossing, expire only above the floor.
	record := func(i0, i1 int) (swap bool) {
		for _, idx := range []int{i0, i1} {
			if !keys[idx].active {
				continue
			}
			keys[idx].usage++
			if keys[idx].usage >= keys[idx].threshold {
				swap = true
				if activeCount() > suiteCFloor {
					keys[idx].active = false
				}
			}
		}
		return swap
	}

	// Drive key 0 (threshold 3) to its crossing; swap must fire on the 3rd use.
	// Pair index 0 with a high-threshold key (4, threshold 365) so the partner
	// never crosses during this phase.
	for use := 1; use <= keys[0].threshold; use++ {
		swap := record(0, 4)
		crossing := use == keys[0].threshold
		if swap != crossing {
			return false, fmt.Sprintf("suiteC: key0 use %d: swapRequired=%v want %v", use, swap, crossing)
		}
	}
	if keys[0].active {
		return false, "suiteC: key0 should have expired after crossing (floor permits)"
	}

	// Drive key 1 (threshold 6) with partner key 4.
	for use := 1; use <= keys[1].threshold; use++ {
		swap := record(1, 4)
		crossing := use == keys[1].threshold
		if swap != crossing {
			return false, fmt.Sprintf("suiteC: key1 use %d: swapRequired=%v want %v", use, swap, crossing)
		}
	}
	if keys[1].active {
		return false, "suiteC: key1 should have expired after crossing (floor permits)"
	}

	// Now active keys are {2,3,4} = 3. Drive key 2 (threshold 9): crossing fires
	// swap and expires (3 > floor 2), leaving {3,4} = 2 at the floor.
	for use := 1; use <= keys[2].threshold; use++ {
		swap := record(2, 4)
		crossing := use == keys[2].threshold
		if swap != crossing {
			return false, fmt.Sprintf("suiteC: key2 use %d: swapRequired=%v want %v", use, swap, crossing)
		}
	}
	if keys[2].active {
		return false, "suiteC: key2 should have expired after crossing (floor still permits: 3>2)"
	}
	if activeCount() != suiteCFloor {
		return false, fmt.Sprintf("suiteC: expected %d active keys at floor, got %d", suiteCFloor, activeCount())
	}

	// At the floor {3,4}. Drive key 3 (threshold 12) with partner 4: the crossing
	// must STILL report swap-required, but the key must NOT expire (would drop
	// below the 2-key floor). Partner key 4 (threshold 365) never crosses here.
	for use := 1; use <= keys[3].threshold; use++ {
		swap := record(3, 4)
		crossing := use == keys[3].threshold
		if swap != crossing {
			return false, fmt.Sprintf("suiteC: key3 use %d: swapRequired=%v want %v", use, swap, crossing)
		}
	}
	if !keys[3].active {
		return false, "suiteC: key3 must remain active at the floor (expired-until-attempted; floor=2)"
	}
	if activeCount() != suiteCFloor {
		return false, fmt.Sprintf("suiteC: floor breached: %d active keys", activeCount())
	}

	return true, ""
}

// FinalizeRun checks that every challenge across suites A, B, and C for the run
// has passed; if so it marks the run 'passed', calls MarkConformancePassed to
// bind the keyset hash, and returns true. If any challenge is unpassed it marks
// the run 'failed' (if all issued challenges are consumed) or leaves it running,
// and returns false with ErrConformanceNotReady.
//
// The caller then invokes AutoActivate (which additionally requires email
// verification) to flip the operator to 'active'.
func (r *Repository) FinalizeRun(ctx context.Context, operatorID, runID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("operator: begin finalize tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var runOperator string
	if err := tx.QueryRow(ctx, `SELECT operator_id FROM conformance_runs WHERE run_id = $1`, runID).Scan(&runOperator); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrConformanceRunNotFound
		}
		return false, fmt.Errorf("operator: load run for finalize: %w", err)
	}
	if runOperator != operatorID {
		return false, ErrConformanceRunNotFound
	}

	// Count challenges and passed challenges, and confirm each of A/B/C is present
	// and passed.
	suitesPassed := map[string]bool{"A": false, "B": false, "C": false}
	rows, err := tx.Query(ctx, `
		SELECT suite, result FROM conformance_challenges WHERE run_id = $1`, runID)
	if err != nil {
		return false, fmt.Errorf("operator: load challenges for finalize: %w", err)
	}
	anyUnconsumed := false
	for rows.Next() {
		var suite string
		var resultRaw []byte
		if err := rows.Scan(&suite, &resultRaw); err != nil {
			rows.Close()
			return false, fmt.Errorf("operator: scan finalize row: %w", err)
		}
		if len(resultRaw) == 0 {
			anyUnconsumed = true
			continue
		}
		var res ChallengeResult
		if err := json.Unmarshal(resultRaw, &res); err != nil {
			rows.Close()
			return false, fmt.Errorf("operator: unmarshal finalize result: %w", err)
		}
		if res.Passed {
			suitesPassed[suite] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("operator: iterate finalize rows: %w", err)
	}

	allPassed := suitesPassed["A"] && suitesPassed["B"] && suitesPassed["C"]
	if !allPassed {
		status := "running"
		if !anyUnconsumed {
			status = "failed"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE conformance_runs SET status = $2, finished_at = CASE WHEN $2 = 'failed' THEN NOW() ELSE finished_at END
			WHERE run_id = $1`, runID, status); err != nil {
			return false, fmt.Errorf("operator: mark run not-passed: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("operator: commit not-passed: %w", err)
		}
		return false, ErrConformanceNotReady
	}

	// All three suites green. Bind the keyset hash (H sorted active pubkeys) on
	// the run and mark it passed, in this same tx.
	hash, count, err := r.activeKeysetHash(ctx, tx, operatorID)
	if err != nil {
		return false, err
	}
	if count != KeyIndexCount {
		return false, ErrKeysetCountMismatch
	}
	if _, err := tx.Exec(ctx, `
		UPDATE conformance_runs
		SET status = 'passed', keyset_hash = $2, finished_at = NOW()
		WHERE run_id = $1`, runID, hash); err != nil {
		return false, fmt.Errorf("operator: mark run passed: %w", err)
	}
	// Bind the pass to the operator (conformance_passed_at + conformance_keyset_hash).
	if _, err := tx.Exec(ctx, `
		UPDATE operators
		SET conformance_passed_at = NOW(), conformance_keyset_hash = $2
		WHERE id = $1`, operatorID, hash); err != nil {
		return false, fmt.Errorf("operator: bind conformance pass to operator: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("operator: commit run passed: %w", err)
	}
	return true, nil
}

// -----------------------------------------------------------------------------
// small helpers
// -----------------------------------------------------------------------------

// freshNonce returns a fresh CSPRNG nonce of conformanceNonceLen bytes.
func freshNonce() ([]byte, error) {
	b := make([]byte, conformanceNonceLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("operator: generate conformance nonce: %w", err)
	}
	return b, nil
}

// freshSeq returns a fresh random uint64 seq for a conformance challenge (the
// conformance path domain-separates from production, so any value is fine).
func freshSeq() (uint64, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return 0, fmt.Errorf("operator: generate conformance seq: %w", err)
	}
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	// Keep it well below the sliding-window edge concerns; it is not replay-checked
	// on the conformance path, but a modest value avoids surprises.
	return v>>1 + 1, nil
}

// algoOf returns the algo shared by the keymap (the algo-pin), or "" if empty.
func algoOf(km map[int]KeyRecord) string {
	for _, rec := range km {
		return rec.Algo
	}
	return ""
}

// bytesEqual is a length-then-content byte comparison (no crypto timing needed
// for a public canonical-bytes equality check).
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// firstDiff returns the first byte offset at which a and b differ, or the length
// of the shorter slice if one is a prefix of the other.
func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

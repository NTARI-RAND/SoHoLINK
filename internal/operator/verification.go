package operator

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
)

// Email-2FA parameters (settled decisions: zero-cost EMAIL 2FA via net/smtp;
// codes bound to the applicant session; rate-limit keyed to the session, not the
// target operator, so a third party cannot grief a victim's lockout).
const (
	// VerificationChannelEmail is the only 2FA channel in v0 (phone deferred).
	// It matches the operator_verifications.channel CHECK constraint.
	VerificationChannelEmail = "email"

	// VerificationCodeTTL is how long an issued 2FA code remains valid.
	VerificationCodeTTL = 10 * time.Minute

	// VerificationMaxAttempts is the per-code attempt cap. On the (cap+1)th wrong
	// check the code is dead and the applicant must request a new one.
	VerificationMaxAttempts = 5

	// VerificationResendCooldown is the minimum interval between issuing codes for
	// the same applicant session — the session-keyed rate limit. A start request
	// inside the cooldown is rejected with ErrVerificationRateLimited.
	VerificationResendCooldown = 30 * time.Second

	// verificationCodeDigits is the length of the numeric 2FA code.
	verificationCodeDigits = 6
)

// Errors surfaced by the 2FA path. Distinguishable so handlers map each to the
// right HTTP status (429 for rate-limited, 400/401 for bad/expired code, etc.).
var (
	// ErrVerificationRateLimited is returned by IssueEmailCode when a code was
	// issued for this session within VerificationResendCooldown. Map to HTTP 429.
	ErrVerificationRateLimited = errors.New("operator: verification code requested too soon; wait before retrying")
	// ErrVerificationSessionActive is returned by IssueEmailCode when a live
	// (unexpired) verification code is already bound to a DIFFERENT applicant
	// session. A third party cannot overwrite or take over another session's
	// in-flight verification; the bound session must let its code expire (or
	// consume it) before a new session may issue one. Map to HTTP 409.
	ErrVerificationSessionActive = errors.New("operator: a verification code is already in flight for another session")
	// ErrVerificationNotFound is returned by CheckEmailCode when no live code
	// exists for the operator (never issued, or already consumed/expired-cleared).
	ErrVerificationNotFound = errors.New("operator: no active verification code")
	// ErrVerificationExpired is returned when the code exists but its TTL passed.
	ErrVerificationExpired = errors.New("operator: verification code expired")
	// ErrVerificationSessionMismatch is returned when the check presents a
	// session_id that does not match the one the code was bound to. A code issued
	// to one applicant session can never be consumed by another.
	ErrVerificationSessionMismatch = errors.New("operator: verification session mismatch")
	// ErrVerificationTooManyAttempts is returned when the attempt cap is reached.
	// The code is dead; the applicant must request a new one.
	ErrVerificationTooManyAttempts = errors.New("operator: too many verification attempts")
	// ErrVerificationCodeMismatch is returned when the submitted code is wrong
	// (and attempts remain). Map to HTTP 401.
	ErrVerificationCodeMismatch = errors.New("operator: incorrect verification code")
)

// generateCode returns a fresh CSPRNG numeric code of verificationCodeDigits
// digits, zero-padded. crypto/rand is used (never math/rand) so the code is not
// predictable.
func generateCode() (string, error) {
	max := big.NewInt(1)
	for i := 0; i < verificationCodeDigits; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("operator: generate 2FA code: %w", err)
	}
	return fmt.Sprintf("%0*d", verificationCodeDigits, n), nil
}

// IssueEmailCode generates a fresh session-bound email 2FA code for the operator
// and upserts it into operator_verifications. It returns the code AND the
// operator's REGISTERED email address so the caller sends the code only to the
// address on file — never to a caller-supplied destination. This is what makes
// the gate prove "the operator controls its registered inbox," not merely "the
// caller controls SOME inbox it named."
//
// Session scoping (genuine, not target-scoped): the resend cooldown and the
// session binding both key off the applicant session, so a third party that
// knows the (public, guessable) slug cannot grief a legitimate applicant.
//
//   - No live row (never issued, or the prior row expired): issue a fresh code
//     bound to sessionID. An expired row is cleared and taken over — a new
//     applicant can start its own onboarding.
//   - Live row bound to THIS session, within the cooldown: ErrVerificationRateLimited.
//   - Live row bound to THIS session, past the cooldown: reissue (rotate the
//     code, refresh the anchor) for the SAME session.
//   - Live row bound to a DIFFERENT session: ErrVerificationSessionActive. The
//     in-flight verification is NOT overwritten and the bound session is NOT
//     rebound; a third party can neither clobber the victim's code nor steal the
//     session. The bound session's code must expire (or be consumed) first.
//
// The issued code is NOT logged or returned anywhere except to the direct
// caller (the handler, which hands it to the Notifier). It never lands in a
// response body.
func (r *Repository) IssueEmailCode(ctx context.Context, operatorID, sessionID string) (code string, email string, err error) {
	if sessionID == "" {
		return "", "", fmt.Errorf("operator: issue code: empty session id")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("operator: begin issue-code tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Load the operator's REGISTERED email — the ONLY destination the code is
	// ever sent to. A caller-supplied address is never honored.
	var registeredEmail string
	if err := tx.QueryRow(ctx, `SELECT email FROM operators WHERE id = $1`, operatorID).Scan(&registeredEmail); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrOperatorNotFound
		}
		return "", "", fmt.Errorf("operator: lookup for issue-code: %w", err)
	}

	// Inspect any existing verification row: its bound session, when it was last
	// issued (rate-limit anchor), and whether it is still live.
	var (
		boundSess   string
		lastCreated time.Time
		expiresAt   time.Time
		haveRow     bool
	)
	err = tx.QueryRow(ctx, `
		SELECT session_id, created_at, expires_at FROM operator_verifications
		WHERE operator_id = $1 AND channel = $2`,
		operatorID, VerificationChannelEmail,
	).Scan(&boundSess, &lastCreated, &expiresAt)
	switch {
	case err == nil:
		haveRow = true
	case errors.Is(err, pgx.ErrNoRows):
		haveRow = false
	default:
		return "", "", fmt.Errorf("operator: load prior verification: %w", err)
	}

	if haveRow {
		live := time.Now().Before(expiresAt)
		if live && boundSess != sessionID {
			// A different session owns the live code. Do NOT overwrite it and do
			// NOT rebind the session — this is the anti-griefing / anti-takeover
			// guard the session scoping promises.
			return "", "", ErrVerificationSessionActive
		}
		if live && boundSess == sessionID && time.Since(lastCreated) < VerificationResendCooldown {
			// Same session, still cooling down: rate-limited.
			return "", "", ErrVerificationRateLimited
		}
		// Same session past the cooldown, or an expired row (from any session):
		// fall through to reissue below.
	}

	newCode, err := generateCode()
	if err != nil {
		return "", "", err
	}
	newExpiry := time.Now().Add(VerificationCodeTTL)

	// Upsert: a fresh code resets attempts to 0, binds this session, and refreshes
	// created_at (the rate-limit anchor) and expires_at. The DIFFERENT-live-session
	// case is already rejected above, so this only ever (re)binds sessionID to a
	// row that has no live owner or that this same session owns.
	if _, err := tx.Exec(ctx, `
		INSERT INTO operator_verifications
		    (operator_id, channel, code, attempts, session_id, expires_at, created_at)
		VALUES ($1, $2, $3, 0, $4, $5, NOW())
		ON CONFLICT (operator_id, channel) DO UPDATE
		    SET code = EXCLUDED.code,
		        attempts = 0,
		        session_id = EXCLUDED.session_id,
		        expires_at = EXCLUDED.expires_at,
		        created_at = NOW()`,
		operatorID, VerificationChannelEmail, newCode, sessionID, newExpiry,
	); err != nil {
		return "", "", fmt.Errorf("operator: upsert verification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("operator: commit issue-code: %w", err)
	}
	return newCode, registeredEmail, nil
}

// CheckEmailCode verifies a submitted 2FA code against the live session-bound
// row and, on success, marks the operator's email verified (and deletes the
// consumed code). It is fail-closed on every branch:
//
//   - no live row                          -> ErrVerificationNotFound
//   - expired                              -> ErrVerificationExpired (row cleared)
//   - session_id mismatch                  -> ErrVerificationSessionMismatch
//     (a code issued to one applicant session cannot be consumed by another;
//     the attempt is NOT counted so it cannot burn a victim's attempts)
//   - attempts already at cap              -> ErrVerificationTooManyAttempts
//   - wrong code (attempts remain)         -> attempts++ then ErrVerificationCodeMismatch
//   - correct code                         -> email_verified = TRUE, row deleted, nil
//
// The comparison is a constant-time-ish direct string compare on the numeric
// code; codes are single-use CSPRNG values with a low attempt cap and short TTL,
// so timing is not the threat model here (the cap and TTL are).
func (r *Repository) CheckEmailCode(ctx context.Context, operatorID, sessionID, code string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operator: begin check-code tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		storedCode string
		attempts   int
		boundSess  string
		expiresAt  time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT code, attempts, session_id, expires_at
		FROM operator_verifications
		WHERE operator_id = $1 AND channel = $2
		FOR UPDATE`,
		operatorID, VerificationChannelEmail,
	).Scan(&storedCode, &attempts, &boundSess, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVerificationNotFound
		}
		return fmt.Errorf("operator: load verification: %w", err)
	}

	if time.Now().After(expiresAt) {
		// Expired: clear the dead row so a fresh IssueEmailCode is not rate-blocked
		// by a stale created_at, and report expiry.
		if _, err := tx.Exec(ctx, `
			DELETE FROM operator_verifications WHERE operator_id = $1 AND channel = $2`,
			operatorID, VerificationChannelEmail); err != nil {
			return fmt.Errorf("operator: clear expired code: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("operator: commit expiry clear: %w", err)
		}
		return ErrVerificationExpired
	}

	// Session binding: a mismatched session does NOT burn an attempt (so a third
	// party cannot exhaust a victim's cap by guessing under a wrong session).
	if boundSess != sessionID {
		return ErrVerificationSessionMismatch
	}

	if attempts >= VerificationMaxAttempts {
		return ErrVerificationTooManyAttempts
	}

	if storedCode != code {
		if _, err := tx.Exec(ctx, `
			UPDATE operator_verifications SET attempts = attempts + 1
			WHERE operator_id = $1 AND channel = $2`,
			operatorID, VerificationChannelEmail); err != nil {
			return fmt.Errorf("operator: increment attempts: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("operator: commit attempt increment: %w", err)
		}
		return ErrVerificationCodeMismatch
	}

	// Correct code: mark email verified and consume the code (single-use).
	if _, err := tx.Exec(ctx, `
		UPDATE operators SET email_verified = TRUE WHERE id = $1`, operatorID); err != nil {
		return fmt.Errorf("operator: set email_verified: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM operator_verifications WHERE operator_id = $1 AND channel = $2`,
		operatorID, VerificationChannelEmail); err != nil {
		return fmt.Errorf("operator: consume verification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operator: commit email verification: %w", err)
	}
	return nil
}

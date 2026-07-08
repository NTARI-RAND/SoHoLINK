package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/notify"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// operatorSlugPattern constrains the operator slug (id) to a known-safe charset
// at intake. The slug is echoed into JS-string-literal-inside-HTML-attribute
// contexts on the governance console (confirm() strings); html/template escapes
// those correctly, so this is defense-in-depth, not the primary XSS control.
// Rejecting anything outside [a-z0-9-] keeps the id to a charset that cannot
// carry a template-context breakout regardless of downstream sink.
var operatorSlugPattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// This file implements the PUBLIC (soholink.org) operator-onboarding backend:
// OPEN application, email-2FA issue+verify, and the conformance harness submit
// path. Templates are Stage 3; these are JSON handlers. Privileged lifecycle
// (activate/revoke) is NOT here — it lives on :8090 (governance separation).
//
// OPEN signup (settled): no invite token. Anyone applies; email-2FA + the
// automated conformance test are the mechanical gates, and passing BOTH
// auto-activates the operator (no human in the entry path). A pending operator's
// keys authenticate nothing (GetActiveKeyMap returns nil unless active).

// operatorRepo is the subset of *operator.Repository the onboarding handlers
// need. Defined as an interface so handlers can be tested without a database.
type operatorRepo interface {
	CreateOperator(ctx context.Context, id, name, email, phone string) error
	AddKeys(ctx context.Context, operatorID string, pubkeys [][]byte, algo string, thresholds []int) error
	IssueEmailCode(ctx context.Context, operatorID, sessionID string) (code string, email string, err error)
	CheckEmailCode(ctx context.Context, operatorID, sessionID, code string) error
	StartConformanceRun(ctx context.Context, operatorID string) (string, []operator.ChallengeA, []operator.ChallengeB, error)
	GradeSuiteA(ctx context.Context, operatorID, runID string, resp operator.ResponseA) (operator.ChallengeResult, error)
	GradeSuiteB(ctx context.Context, operatorID, runID string, resp operator.ResponseB) (operator.ChallengeResult, error)
	GradeSuiteC(ctx context.Context, operatorID, runID string) (operator.ChallengeResult, error)
	FinalizeRun(ctx context.Context, operatorID, runID string) (bool, error)
	AutoActivate(ctx context.Context, operatorID string) error
}

// OnboardingServer holds the dependencies for the public onboarding handlers:
// the operator repository, the Notifier used to deliver 2FA codes, and the
// default per-key expiration thresholds drawn at key registration.
type OnboardingServer struct {
	repo       operatorRepo
	notifier   notify.Notifier
	thresholds []int // len == operator.KeyIndexCount; drawn at AddKeys time

	// Console (Stage-4 step 2) GET-page fields, populated by ConfigureConsole.
	// Zero-valued on a POST-only server: the GET routes then render a 500
	// "template not found", the same failure mode as the portal with a missing
	// template. See operator_console.go.
	consoleRepo   consoleRepo
	templatePaths []string
	coordinatorID string
}

// NewOnboardingServer constructs an OnboardingServer. thresholds MUST have
// exactly operator.KeyIndexCount entries (the §4.2.2.1 EXPIRATIONS drawn per
// key); if nil, a uniform default is used.
func NewOnboardingServer(repo operatorRepo, notifier notify.Notifier, thresholds []int) *OnboardingServer {
	if len(thresholds) != operator.KeyIndexCount {
		thresholds = defaultThresholds()
	}
	return &OnboardingServer{repo: repo, notifier: notifier, thresholds: thresholds}
}

// defaultThresholds is the fallback per-key expiration distribution. The design
// leaves the exact distribution open (design §13 item 4); a uniform value keeps
// all seven keys well above the MinActiveKeys floor for the pilot.
func defaultThresholds() []int {
	th := make([]int, operator.KeyIndexCount)
	for i := range th {
		th[i] = 365
	}
	return th
}

// RegisterRoutes wires the public onboarding routes onto mux. All are POST JSON
// except where noted. These belong on the PUBLIC portal mux, never on :8090.
func (s *OnboardingServer) RegisterRoutes(mux *http.ServeMux) {
	// Public JSON routes (Stage 2).
	mux.HandleFunc("POST /operators/apply", s.handleApply)
	mux.HandleFunc("POST /operators/{id}/keys", s.handleRegisterKeys)
	mux.HandleFunc("POST /operators/{id}/verify/start", s.handleVerifyStart)
	mux.HandleFunc("POST /operators/{id}/verify/check", s.handleVerifyCheck)
	mux.HandleFunc("POST /operators/{id}/conformance/start", s.handleConformanceStart)
	mux.HandleFunc("POST /operators/{id}/conformance/{run}/submit", s.handleConformanceSubmit)

	// Public console GET pages (Stage 4 step 2). These render the operator_*.html
	// templates against the read models; they are pure reads and carry no
	// privileged action (activation/revocation stay on :8090). A POST-only server
	// (ConfigureConsole not called) still registers these — they render a 500
	// until templates are wired, which is harmless.
	mux.HandleFunc("GET /{$}", s.handleLanding)
	mux.HandleFunc("GET /fees", s.handleFeesPage)
	mux.HandleFunc("GET /operators/apply", s.handleApplyPage)
	mux.HandleFunc("GET /operators/{id}/keys", s.handleKeysPage)
	mux.HandleFunc("GET /operators/{id}/verify", s.handleVerifyPage)
	mux.HandleFunc("GET /operators/{id}/conformance", s.handleConformancePage)
	mux.HandleFunc("GET /operators/{id}/dashboard", s.handleDashboardPage)
}

// PublicOperatorHandler returns the public operator surface (onboarding routes
// plus any additional operator-fronted routes registered on `extra`) wrapped with
// the per-IP 401 rate limiter. This is how the PUBLIC portal should mount the
// operator endpoints: the limiter is a SOFT throttle keyed to authentication
// FAILURES (429 + Retry-After after an IP's 401 budget is spent, always
// refilling) and is NEVER an IP-scoped lockout — Cloudy's fleet shares one egress
// IP, so a hard block would deny the whole fleet (task constraint / design §13).
//
// burst is the number of 401s an IP may incur before throttling; refillPerSec is
// the recovery rate. Pass 0/0 for the pilot defaults (burst 10, refill 0.2/s).
// `extra` may be nil; if non-nil, its routes (e.g. OperatorAuth-fronted handlers)
// are mounted alongside the onboarding routes on the same limited mux so they
// share the 401 budget.
func (s *OnboardingServer) PublicOperatorHandler(burst, refillPerSec float64, extra func(mux *http.ServeMux)) http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	if extra != nil {
		extra(mux)
	}
	limiter := newAuthFailureLimiter(burst, refillPerSec)
	return limiter.Wrap(mux)
}

// -----------------------------------------------------------------------------
// T1 — OPEN application (submit platform metadata + register 7 keys later).
// -----------------------------------------------------------------------------

type applyRequest struct {
	Slug  string `json:"slug"`  // stable operator id, e.g. "cloudy"
	Name  string `json:"name"`  // platform display name
	Email string `json:"email"` // normalized server-side
	Phone string `json:"phone"` // optional; phone-2FA deferred
}

type applyResponse struct {
	OperatorID string `json:"operator_id"`
	State      string `json:"onboarding_state"`
}

// handleApply creates a pending operator. OPEN: no invite token. Duplicate slug
// or email -> 409.
func (s *OnboardingServer) handleApply(w http.ResponseWriter, r *http.Request) {
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Name = strings.TrimSpace(req.Name)
	if req.Slug == "" || req.Name == "" || strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "slug, name, and email are required")
		return
	}
	if !operatorSlugPattern.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must be 1-64 characters of lowercase letters, digits, or hyphens")
		return
	}

	err := s.repo.CreateOperator(r.Context(), req.Slug, req.Name, req.Email, req.Phone)
	if errors.Is(err, operator.ErrDuplicateOperator) {
		writeError(w, http.StatusConflict, "an operator with that slug or email already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create operator")
		return
	}
	writeJSON(w, http.StatusCreated, applyResponse{OperatorID: req.Slug, State: "pending_verification"})
}

// -----------------------------------------------------------------------------
// T1b — register exactly 7 raw-32B public keys (base64).
// -----------------------------------------------------------------------------

type registerKeysRequest struct {
	Algo       string   `json:"algo"`        // "ed25519" (v0)
	PublicKeys []string `json:"public_keys"` // exactly 7, base64 std of raw 32-byte keys, index order
}

// handleRegisterKeys registers the operator's 7 public keys. Registering (or
// re-registering) clears any prior conformance pass (bound to a keyset). Refuses
// unless exactly 7 valid keys are supplied.
func (s *OnboardingServer) handleRegisterKeys(w http.ResponseWriter, r *http.Request) {
	operatorID := r.PathValue("id")
	var req registerKeysRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	algo := req.Algo
	if algo == "" {
		algo = operator.AlgoEd25519
	}
	if algo != operator.AlgoEd25519 {
		writeError(w, http.StatusBadRequest, "unsupported algo (v0 permits ed25519 only)")
		return
	}
	if len(req.PublicKeys) != operator.KeyIndexCount {
		writeError(w, http.StatusBadRequest, "exactly seven public keys are required")
		return
	}
	pubs := make([][]byte, operator.KeyIndexCount)
	for i, b64 := range req.PublicKeys {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			writeError(w, http.StatusBadRequest, "public key is not valid base64")
			return
		}
		if len(raw) != 32 {
			writeError(w, http.StatusBadRequest, "each ed25519 public key must be raw 32 bytes")
			return
		}
		pubs[i] = raw
	}

	err := s.repo.AddKeys(r.Context(), operatorID, pubs, algo, s.thresholds)
	if errors.Is(err, operator.ErrOperatorNotFound) {
		writeError(w, http.StatusNotFound, "operator not found")
		return
	}
	if errors.Is(err, operator.ErrKeysetCountMismatch) {
		writeError(w, http.StatusBadRequest, "exactly seven public keys are required")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "could not register keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registered": operator.KeyIndexCount})
}

// -----------------------------------------------------------------------------
// T2 — email-2FA issue + verify (session-bound, session-keyed rate-limit).
// -----------------------------------------------------------------------------

type verifyStartRequest struct {
	Channel   string `json:"channel"`    // "email" only in v0
	SessionID string `json:"session_id"` // applicant session; the code is bound to it
}

// handleVerifyStart issues a fresh session-bound 2FA code and sends it — ONLY to
// the operator's REGISTERED email (read server-side by IssueEmailCode), never to
// a caller-supplied address. This is what makes the gate prove the operator
// controls its registered inbox. The code is NEVER returned in the response body.
// Rate-limited per session (429); an in-flight code owned by a different session
// is protected (409), not clobbered.
func (s *OnboardingServer) handleVerifyStart(w http.ResponseWriter, r *http.Request) {
	operatorID := r.PathValue("id")
	var req verifyStartRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if req.Channel != "" && req.Channel != operator.VerificationChannelEmail {
		writeError(w, http.StatusBadRequest, "unsupported channel (email only in v0)")
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	code, email, err := s.repo.IssueEmailCode(r.Context(), operatorID, req.SessionID)
	if errors.Is(err, operator.ErrOperatorNotFound) {
		writeError(w, http.StatusNotFound, "operator not found")
		return
	}
	if errors.Is(err, operator.ErrVerificationRateLimited) {
		writeError(w, http.StatusTooManyRequests, "verification code requested too soon; wait before retrying")
		return
	}
	if errors.Is(err, operator.ErrVerificationSessionActive) {
		writeError(w, http.StatusConflict, "a verification code is already in flight for another session; wait for it to expire")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue verification code")
		return
	}

	// Send via the Notifier to the REGISTERED address only. A send failure is
	// reported so the applicant can retry (the code is already stored; the
	// cooldown still applies).
	if err := s.notifier.Send(notify.Message{
		To:      email,
		Subject: "Your SoHoLINK operator verification code",
		Body:    "Your verification code is: " + code + "\n\nIt expires in 10 minutes.",
	}); err != nil {
		writeError(w, http.StatusBadGateway, "could not send verification code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

type verifyCheckRequest struct {
	Channel   string `json:"channel"`
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
}

// handleVerifyCheck verifies a submitted 2FA code. On success the operator's
// email is marked verified; it then opportunistically attempts AutoActivate (in
// case conformance already passed). Distinguishable errors map to 401/410/429.
func (s *OnboardingServer) handleVerifyCheck(w http.ResponseWriter, r *http.Request) {
	operatorID := r.PathValue("id")
	var req verifyCheckRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "session_id and code are required")
		return
	}

	err := s.repo.CheckEmailCode(r.Context(), operatorID, req.SessionID, req.Code)
	switch {
	case err == nil:
		// Email verified; try to auto-activate (no-op unless conformance also passed).
		s.tryAutoActivate(r.Context(), operatorID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
	case errors.Is(err, operator.ErrVerificationNotFound):
		writeError(w, http.StatusBadRequest, "no active verification code")
	case errors.Is(err, operator.ErrVerificationExpired):
		writeError(w, http.StatusGone, "verification code expired")
	case errors.Is(err, operator.ErrVerificationSessionMismatch):
		writeError(w, http.StatusUnauthorized, "verification session mismatch")
	case errors.Is(err, operator.ErrVerificationTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too many verification attempts")
	case errors.Is(err, operator.ErrVerificationCodeMismatch):
		writeError(w, http.StatusUnauthorized, "incorrect verification code")
	default:
		writeError(w, http.StatusInternalServerError, "could not verify code")
	}
}

// -----------------------------------------------------------------------------
// T3 — conformance harness start + submit.
// -----------------------------------------------------------------------------

type conformanceStartResponse struct {
	RunID       string                `json:"run_id"`
	ChallengesA []operator.ChallengeA `json:"challenges_a"`
	ChallengesB []operator.ChallengeB `json:"challenges_b"`
}

// handleConformanceStart creates a run and returns the Suite A + Suite B
// challenges (fresh CSPRNG-nonced, SoHoLINK-computed oracles). Suite C needs no
// operator round-trip (graded server-side on submit).
func (s *OnboardingServer) handleConformanceStart(w http.ResponseWriter, r *http.Request) {
	operatorID := r.PathValue("id")
	runID, chA, chB, err := s.repo.StartConformanceRun(r.Context(), operatorID)
	if errors.Is(err, operator.ErrKeysetCountMismatch) {
		writeError(w, http.StatusBadRequest, "register all seven keys before starting conformance")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start conformance run")
		return
	}
	writeJSON(w, http.StatusOK, conformanceStartResponse{RunID: runID, ChallengesA: chA, ChallengesB: chB})
}

type conformanceSubmitRequest struct {
	SuiteA []operator.ResponseA `json:"suite_a"`
	SuiteB []operator.ResponseB `json:"suite_b"`
}

type conformanceSubmitResponse struct {
	Results   []operator.ChallengeResult `json:"results"`
	Passed    bool                       `json:"passed"`
	Activated bool                       `json:"activated"`
}

// handleConformanceSubmit grades the operator's Suite A + Suite B responses,
// runs Suite C server-side, then attempts to finalize the run. On a full pass it
// binds the keyset hash (in FinalizeRun) and calls AutoActivate; the response
// reports whether the operator became active (requires email verification too).
func (s *OnboardingServer) handleConformanceSubmit(w http.ResponseWriter, r *http.Request) {
	operatorID := r.PathValue("id")
	runID := r.PathValue("run")
	var req conformanceSubmitRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	var results []operator.ChallengeResult
	for _, ra := range req.SuiteA {
		res, err := s.repo.GradeSuiteA(r.Context(), operatorID, runID, ra)
		if errors.Is(err, operator.ErrConformanceRunNotFound) {
			writeError(w, http.StatusNotFound, "conformance run or challenge not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not grade suite A")
			return
		}
		results = append(results, res)
	}
	for _, rb := range req.SuiteB {
		res, err := s.repo.GradeSuiteB(r.Context(), operatorID, runID, rb)
		if errors.Is(err, operator.ErrConformanceRunNotFound) {
			writeError(w, http.StatusNotFound, "conformance run or challenge not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not grade suite B")
			return
		}
		results = append(results, res)
	}
	// Suite C is graded server-side (scratch namespace; no operator response).
	resC, err := s.repo.GradeSuiteC(r.Context(), operatorID, runID)
	if errors.Is(err, operator.ErrConformanceRunNotFound) {
		writeError(w, http.StatusNotFound, "conformance run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not grade suite C")
		return
	}
	results = append(results, resC)

	passed, err := s.repo.FinalizeRun(r.Context(), operatorID, runID)
	if err != nil && !errors.Is(err, operator.ErrConformanceNotReady) {
		writeError(w, http.StatusInternalServerError, "could not finalize run")
		return
	}

	activated := false
	if passed {
		// On email-verified AND all suites passed -> auto-activate (binds keyset-hash
		// was done in FinalizeRun; AutoActivate re-checks all preconditions).
		if actErr := s.repo.AutoActivate(r.Context(), operatorID); actErr == nil {
			activated = true
		}
	}
	writeJSON(w, http.StatusOK, conformanceSubmitResponse{Results: results, Passed: passed, Activated: activated})
}

// tryAutoActivate attempts activation and swallows the "preconditions not met"
// family of errors (they are expected until BOTH gates pass). It is called on
// email verification so the operator activates the moment the second gate closes,
// regardless of which gate closes last.
func (s *OnboardingServer) tryAutoActivate(ctx context.Context, operatorID string) {
	_ = s.repo.AutoActivate(ctx, operatorID) //nolint:errcheck // best-effort; errors are expected pre-both-gates
}

// -----------------------------------------------------------------------------
// JSON helpers (local to the onboarding handlers).
// -----------------------------------------------------------------------------

// decodeJSON decodes the request body into v, writing a 400 and returning an
// error on failure. Callers return immediately on a non-nil error.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)) // 1 MiB cap
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return err
	}
	return nil
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// compile-time assertion that *operator.Repository satisfies operatorRepo.
var _ operatorRepo = (*operator.Repository)(nil)

//go:build integration

package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	protoop "github.com/NTARI-RAND/sohocloud-protocol/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/api"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/notify"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// connectTestDB mirrors the operator package's harness: TEST_DATABASE_URL ONLY
// (never DATABASE_URL), refuses any database whose name lacks "test", runs
// migrations, and wipes the operator tables. Dev XXIV data-loss guard: the
// destructive guard fires before any TRUNCATE.
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
	for _, stmt := range []string{
		`TRUNCATE operators CASCADE`,
		`TRUNCATE operator_replay`,
		`TRUNCATE operator_nonces`,
	} {
		if _, err := db.Pool.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("wipe (%s): %v", stmt, err)
		}
	}
	return db
}

// testOperator is a fully honest operator client: it holds 7 real keypairs and
// computes canon + signs exactly as a conformant Cloudy would. Used to prove the
// harness passes a REAL implementation end-to-end.
type testOperator struct {
	id    string
	privs []ed25519.PrivateKey
	pubs  [][]byte
}

func newTestOperator(t *testing.T, id string) *testOperator {
	t.Helper()
	op := &testOperator{id: id}
	for i := 0; i < operator.KeyIndexCount; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("gen key %d: %v", i, err)
		}
		op.privs = append(op.privs, priv)
		op.pubs = append(op.pubs, pub)
	}
	return op
}

func (o *testOperator) pubsB64() []string {
	out := make([]string, len(o.pubs))
	for i, p := range o.pubs {
		out[i] = base64.StdEncoding.EncodeToString(p)
	}
	return out
}

// TestOnboarding_EndToEnd_HappyPath drives the full public onboarding flow
// through the real HTTP handlers against a real Postgres, with a real operator
// signing real canon: apply -> keys -> email 2FA -> conformance (A/B/C) ->
// auto-activate. Then confirms GetActiveKeyMap returns the 7 keys (the operator
// can now authenticate).
func TestOnboarding_EndToEnd_HappyPath(t *testing.T) {
	db := connectTestDB(t)
	repo := operator.NewRepository(db.Pool)
	log := notify.NewLogNotifier()
	srv := api.NewOnboardingServer(repo, log, nil)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx := context.Background()
	op := newTestOperator(t, "cloudy")
	session := "sess-abc-123"

	// T1: apply.
	do(t, ts, "POST", "/operators/apply", map[string]any{
		"slug": op.id, "name": "Cloudy", "email": "ops@cloudy.io",
	}, http.StatusCreated)

	// T1b: register 7 keys.
	do(t, ts, "POST", "/operators/"+op.id+"/keys", map[string]any{
		"algo": "ed25519", "public_keys": op.pubsB64(),
	}, http.StatusOK)

	// T2: email 2FA start -> code delivered via LogNotifier to the REGISTERED
	// address (server-side; the caller no longer names a destination).
	do(t, ts, "POST", "/operators/"+op.id+"/verify/start", map[string]any{
		"channel": "email", "session_id": session,
	}, http.StatusOK)
	last, ok := log.Last()
	if !ok {
		t.Fatal("no verification code delivered")
	}
	code := extractCode(t, last.Body)

	// T2: verify check with the delivered code.
	do(t, ts, "POST", "/operators/"+op.id+"/verify/check", map[string]any{
		"session_id": session, "code": code,
	}, http.StatusOK)

	// T3: start a conformance run.
	var startResp struct {
		RunID       string                `json:"run_id"`
		ChallengesA []operator.ChallengeA `json:"challenges_a"`
		ChallengesB []operator.ChallengeB `json:"challenges_b"`
	}
	doInto(t, ts, "POST", "/operators/"+op.id+"/conformance/start", nil, http.StatusOK, &startResp)
	if len(startResp.ChallengesA) != 1 || len(startResp.ChallengesB) != 1 {
		t.Fatalf("expected 1 A and 1 B challenge, got %d/%d", len(startResp.ChallengesA), len(startResp.ChallengesB))
	}

	// Suite A: compute the SAME canon bytes and sign a ConformanceResponse.
	chA := startResp.ChallengesA[0]
	canonA := protoop.OperatorTransmission{
		OperatorID: chA.OperatorID,
		TsUnixNano: chA.TsUnixNano,
		Nonce:      chA.Nonce,
		Seq:        chA.Seq,
		Algo:       chA.Algo,
		Idx0:       chA.Idx0,
		Idx1:       chA.Idx1,
	}.CanonicalBytes()
	crA := protoop.ConformanceResponse{
		OperatorID: op.id, Challenge: canonA, Algo: chA.Algo,
	}
	crA.Sign(op.privs[chA.Idx0], op.privs[chA.Idx1], chA.Idx0, chA.Idx1)
	respA := operator.ResponseA{
		ChallengeID: chA.ChallengeID, CanonicalBytes: canonA,
		Idx0: chA.Idx0, Idx1: chA.Idx1, Sig0: crA.Sig0, Sig1: crA.Sig1,
	}

	// Suite B: produce a full transmission over the fresh fields.
	chB := startResp.ChallengesB[0]
	txB := protoop.OperatorTransmission{
		OperatorID: op.id, TsUnixNano: chB.TsUnixNano, Nonce: chB.Nonce,
		Seq: chB.Seq, Algo: chB.Algo,
	}
	txB.Sign(op.privs[2], op.privs[5], 2, 5)
	respB := operator.ResponseB{
		ChallengeID: chB.ChallengeID, Idx0: 2, Idx1: 5, Sig0: txB.Sig0, Sig1: txB.Sig1,
	}

	// Submit.
	var submitResp struct {
		Results   []operator.ChallengeResult `json:"results"`
		Passed    bool                       `json:"passed"`
		Activated bool                       `json:"activated"`
	}
	doInto(t, ts, "POST", "/operators/"+op.id+"/conformance/"+startResp.RunID+"/submit", map[string]any{
		"suite_a": []operator.ResponseA{respA},
		"suite_b": []operator.ResponseB{respB},
	}, http.StatusOK, &submitResp)

	for _, res := range submitResp.Results {
		if !res.Passed {
			t.Errorf("suite %s challenge %s FAILED: %s", res.Suite, res.ChallengeID, res.Detail)
		}
	}
	if !submitResp.Passed {
		t.Fatal("conformance run did not pass")
	}
	if !submitResp.Activated {
		t.Fatal("operator did not auto-activate after both gates passed")
	}

	// The operator can now authenticate: GetActiveKeyMap returns the 7 keys.
	km, err := repo.GetActiveKeyMap(ctx, op.id)
	if err != nil {
		t.Fatalf("GetActiveKeyMap: %v", err)
	}
	if len(km) != operator.KeyIndexCount {
		t.Fatalf("active key map has %d keys, want %d", len(km), operator.KeyIndexCount)
	}
}

// TestOnboarding_SuiteA_WrongBytesFails confirms Suite A grading rejects an
// operator that returns bytes not byte-equal to SoHoLINK's oracle.
func TestOnboarding_SuiteA_WrongBytesFails(t *testing.T) {
	db := connectTestDB(t)
	repo := operator.NewRepository(db.Pool)
	srv := api.NewOnboardingServer(repo, notify.NewLogNotifier(), nil)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	op := newTestOperator(t, "badcanon")
	do(t, ts, "POST", "/operators/apply", map[string]any{
		"slug": op.id, "name": "Bad", "email": "bad@x.io",
	}, http.StatusCreated)
	do(t, ts, "POST", "/operators/"+op.id+"/keys", map[string]any{
		"algo": "ed25519", "public_keys": op.pubsB64(),
	}, http.StatusOK)

	var startResp struct {
		RunID       string                `json:"run_id"`
		ChallengesA []operator.ChallengeA `json:"challenges_a"`
	}
	doInto(t, ts, "POST", "/operators/"+op.id+"/conformance/start", nil, http.StatusOK, &startResp)
	chA := startResp.ChallengesA[0]

	// Return WRONG bytes (truncated) but a valid-looking signature over them.
	wrong := []byte("not the canonical bytes")
	crA := protoop.ConformanceResponse{OperatorID: op.id, Challenge: wrong, Algo: chA.Algo}
	crA.Sign(op.privs[chA.Idx0], op.privs[chA.Idx1], chA.Idx0, chA.Idx1)
	respA := operator.ResponseA{
		ChallengeID: chA.ChallengeID, CanonicalBytes: wrong,
		Idx0: chA.Idx0, Idx1: chA.Idx1, Sig0: crA.Sig0, Sig1: crA.Sig1,
	}

	var submitResp struct {
		Results []operator.ChallengeResult `json:"results"`
		Passed  bool                       `json:"passed"`
	}
	doInto(t, ts, "POST", "/operators/"+op.id+"/conformance/"+startResp.RunID+"/submit", map[string]any{
		"suite_a": []operator.ResponseA{respA},
	}, http.StatusOK, &submitResp)

	var suiteA *operator.ChallengeResult
	for i := range submitResp.Results {
		if submitResp.Results[i].Suite == operator.SuiteA {
			suiteA = &submitResp.Results[i]
		}
	}
	if suiteA == nil {
		t.Fatal("no Suite A result")
	}
	if suiteA.Passed {
		t.Fatal("Suite A should FAIL on wrong canonical bytes")
	}
	if !strings.Contains(suiteA.Detail, "canonical bytes mismatch") {
		t.Errorf("expected byte-mismatch detail, got %q", suiteA.Detail)
	}
	if submitResp.Passed {
		t.Fatal("run should not pass with a failed Suite A")
	}
}

// TestOnboarding_RateLimit confirms a second verify/start inside the cooldown is
// rejected with 429.
func TestOnboarding_RateLimit(t *testing.T) {
	db := connectTestDB(t)
	repo := operator.NewRepository(db.Pool)
	srv := api.NewOnboardingServer(repo, notify.NewLogNotifier(), nil)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	do(t, ts, "POST", "/operators/apply", map[string]any{
		"slug": "rl", "name": "RL", "email": "rl@x.io",
	}, http.StatusCreated)
	do(t, ts, "POST", "/operators/rl/verify/start", map[string]any{
		"channel": "email", "session_id": "s1",
	}, http.StatusOK)
	// Immediate resend, SAME session -> rate limited.
	do(t, ts, "POST", "/operators/rl/verify/start", map[string]any{
		"channel": "email", "session_id": "s1",
	}, http.StatusTooManyRequests)
	// A DIFFERENT session cannot clobber or take over the in-flight code -> 409.
	do(t, ts, "POST", "/operators/rl/verify/start", map[string]any{
		"channel": "email", "session_id": "s2",
	}, http.StatusConflict)
}

// TestOnboarding_DuplicateApply confirms a duplicate slug returns 409.
func TestOnboarding_DuplicateApply(t *testing.T) {
	db := connectTestDB(t)
	repo := operator.NewRepository(db.Pool)
	srv := api.NewOnboardingServer(repo, notify.NewLogNotifier(), nil)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	do(t, ts, "POST", "/operators/apply", map[string]any{
		"slug": "dup", "name": "Dup", "email": "dup@x.io",
	}, http.StatusCreated)
	do(t, ts, "POST", "/operators/apply", map[string]any{
		"slug": "dup", "name": "Dup2", "email": "other@x.io",
	}, http.StatusConflict)
}

// -----------------------------------------------------------------------------
// test HTTP helpers
// -----------------------------------------------------------------------------

func do(t *testing.T, ts *httptest.Server, method, path string, body any, wantStatus int) {
	t.Helper()
	doInto(t, ts, method, path, body, wantStatus, nil)
}

func doInto(t *testing.T, ts *httptest.Server, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, resp.StatusCode, wantStatus, buf.String())
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
}

// extractCode pulls the 6-digit code out of the delivered email body.
func extractCode(t *testing.T, body string) string {
	t.Helper()
	// Body form: "Your verification code is: NNNNNN\n\n..."
	const marker = "code is: "
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no code marker in body: %q", body)
	}
	rest := body[i+len(marker):]
	code := strings.TrimSpace(rest)
	if nl := strings.IndexByte(code, '\n'); nl >= 0 {
		code = code[:nl]
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		t.Fatalf("extracted code %q not 6 digits", code)
	}
	return code
}

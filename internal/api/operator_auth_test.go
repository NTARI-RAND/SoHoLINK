package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// fakeVerifier is an in-memory operatorVerifier for middleware unit tests: no
// database. It captures the key map, forces replay/usage outcomes, and records
// call counts so tests can assert exactly-once accounting and fail-closed order.
type fakeVerifier struct {
	km              map[int]operator.KeyRecord
	kmErr           error
	replayErr       error
	usageSwap       bool
	usageErr        error
	getKeyMapCalls  int
	checkAdvCalls   int
	recordUsecalls  int
	lastScope       string
	lastCoordinator string
}

func (f *fakeVerifier) GetActiveKeyMap(_ context.Context, _ string) (map[int]operator.KeyRecord, error) {
	f.getKeyMapCalls++
	return f.km, f.kmErr
}

func (f *fakeVerifier) CheckAndAdvance(_ context.Context, _, coordinatorID string, _ uint64, _ []byte, scope string, _ time.Time) error {
	f.checkAdvCalls++
	f.lastScope = scope
	f.lastCoordinator = coordinatorID
	return f.replayErr
}

func (f *fakeVerifier) RecordUsageAndExpire(_ context.Context, _ string, _, _ int) (bool, error) {
	f.recordUsecalls++
	return f.usageSwap, f.usageErr
}

// testKeyset returns 7 fresh Ed25519 keypairs and the corresponding protocol
// KeyRecord map (all ed25519), for signing and verifying transmissions.
func testKeyset(t *testing.T) ([]ed25519.PrivateKey, map[int]operator.KeyRecord) {
	t.Helper()
	privs := make([]ed25519.PrivateKey, operator.KeyIndexCount)
	km := make(map[int]operator.KeyRecord, operator.KeyIndexCount)
	for i := 0; i < operator.KeyIndexCount; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate key %d: %v", i, err)
		}
		privs[i] = priv
		km[i] = operator.KeyRecord{PublicKey: pub, Algo: operator.AlgoEd25519}
	}
	return privs, km
}

// signedTransmission builds and signs a fresh valid transmission with indices
// idx0/idx1 over a random 16-byte nonce and the current timestamp.
func signedTransmission(t *testing.T, privs []ed25519.PrivateKey, operatorID string, seq uint64, idx0, idx1 int) operator.OperatorTransmission {
	t.Helper()
	nonce := make([]byte, operator.MinNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	tx := operator.OperatorTransmission{
		OperatorID: operatorID,
		TsUnixNano: time.Now().UnixNano(),
		Nonce:      nonce,
		Seq:        seq,
		Algo:       operator.AlgoEd25519,
	}
	tx.Sign(privs[idx0], privs[idx1], idx0, idx1)
	return tx
}

// okHandler is the protected handler; it records that it ran and echoes the
// verified operator id from context.
func okHandler(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*ran = true
		if v, ok := OperatorFromContext(r.Context()); ok {
			w.Header().Set("X-Test-Operator", v.OperatorID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func doRequest(t *testing.T, h http.Handler, header string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/nodes/heartbeat", nil)
	if header != "" {
		req.Header.Set(OperatorHeader, header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOperatorAuth_ValidTransmission_Passes(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	tx := signedTransmission(t, privs, "cloudy", 1, 0, 3)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !ran {
		t.Fatal("protected handler did not run")
	}
	if got := rec.Header().Get("X-Test-Operator"); got != "cloudy" {
		t.Errorf("context operator id = %q, want cloudy", got)
	}
	if fv.checkAdvCalls != 1 || fv.recordUsecalls != 1 {
		t.Errorf("expected exactly-once CAS+usage, got cas=%d usage=%d", fv.checkAdvCalls, fv.recordUsecalls)
	}
	if fv.lastScope != "production" {
		t.Errorf("CAS scope = %q, want production", fv.lastScope)
	}
	if fv.lastCoordinator != "soholink" {
		t.Errorf("coordinator id = %q, want soholink", fv.lastCoordinator)
	}
}

func TestOperatorAuth_MissingHeader_401_Required(t *testing.T) {
	fv := &fakeVerifier{km: map[int]operator.KeyRecord{}}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	rec := doRequest(t, h, "") // no header: REQUIRED, not additive

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for absent token, got %d", rec.Code)
	}
	if ran {
		t.Fatal("handler ran despite missing operator token (should be mutually exclusive, not fall-through)")
	}
	if fv.getKeyMapCalls != 0 {
		t.Errorf("no verifier work should happen without a token, got getKeyMap=%d", fv.getKeyMapCalls)
	}
}

func TestOperatorAuth_NonActiveOperator_NilKeymap_FailsClosed(t *testing.T) {
	privs, _ := testKeyset(t)
	// GetActiveKeyMap returns nil (operator pending/verified/revoked) -> Verify
	// against nil keymap must fail closed.
	fv := &fakeVerifier{km: nil}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	tx := signedTransmission(t, privs, "cloudy", 1, 0, 3)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for non-active operator, got %d", rec.Code)
	}
	if ran {
		t.Fatal("handler ran for a non-active operator (nil keymap)")
	}
	if fv.checkAdvCalls != 0 {
		t.Error("replay CAS ran despite verify failure — order violation")
	}
}

func TestOperatorAuth_TamperedSignature_Rejected(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	tx := signedTransmission(t, privs, "cloudy", 1, 0, 3)
	tx.Sig0[0] ^= 0xFF // flip a bit
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered sig, got %d", rec.Code)
	}
	if ran || fv.checkAdvCalls != 0 {
		t.Error("tampered transmission reached CAS or handler")
	}
}

func TestOperatorAuth_StaleTimestamp_Rejected(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	nonce := make([]byte, operator.MinNonceLen)
	_, _ = rand.Read(nonce)
	tx := operator.OperatorTransmission{
		OperatorID: "cloudy",
		TsUnixNano: time.Now().Add(-10 * time.Minute).UnixNano(), // outside 5min window
		Nonce:      nonce,
		Seq:        1,
		Algo:       operator.AlgoEd25519,
	}
	tx.Sign(privs[0], privs[3], 0, 3)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for stale timestamp, got %d", rec.Code)
	}
	if fv.getKeyMapCalls != 0 {
		t.Error("stale timestamp should be rejected before any DB work")
	}
}

func TestOperatorAuth_ReplayRejected(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km, replayErr: operator.ErrReplay}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	tx := signedTransmission(t, privs, "cloudy", 1, 0, 3)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for replay, got %d", rec.Code)
	}
	if ran {
		t.Fatal("handler ran despite replay rejection")
	}
	if fv.recordUsecalls != 0 {
		t.Error("usage accounting ran despite replay rejection — must be downstream of CAS")
	}
}

func TestOperatorAuth_SwapRequiredHeaderSet(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km, usageSwap: true}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	tx := signedTransmission(t, privs, "cloudy", 1, 0, 3)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get(SwapRequiredHeader) != "true" {
		t.Error("expected X-Operator-Swap-Required: true when a key hit threshold")
	}
}

func TestOperatorAuth_SameIndexRejected(t *testing.T) {
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	// Sign with the same index twice: protocol Verify must reject (ErrSameIndex).
	tx := signedTransmission(t, privs, "cloudy", 1, 2, 2)
	rec := doRequest(t, h, EncodeOperatorHeader(tx))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for same-index, got %d", rec.Code)
	}
	if fv.checkAdvCalls != 0 {
		t.Error("same-index transmission reached CAS")
	}
}

func TestOperatorAuth_MalformedHeader_Rejected(t *testing.T) {
	fv := &fakeVerifier{km: map[int]operator.KeyRecord{}}
	var ran bool
	h := OperatorAuth(fv, "soholink", okHandler(&ran))

	rec := doRequest(t, h, "!!!not-base64!!!")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for malformed header, got %d", rec.Code)
	}
	if fv.getKeyMapCalls != 0 || ran {
		t.Error("malformed header should reject before any verifier work")
	}
}

func TestEncodeDecodeOperatorHeader_RoundTrip(t *testing.T) {
	privs, _ := testKeyset(t)
	tx := signedTransmission(t, privs, "cloudy", 42, 1, 5)

	got, err := decodeOperatorHeader(EncodeOperatorHeader(tx))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OperatorID != tx.OperatorID || got.Seq != tx.Seq ||
		got.TsUnixNano != tx.TsUnixNano || got.Idx0 != tx.Idx0 || got.Idx1 != tx.Idx1 ||
		got.Algo != tx.Algo {
		t.Errorf("scalar round-trip mismatch: %+v vs %+v", got, tx)
	}
	if string(got.Nonce) != string(tx.Nonce) ||
		string(got.Sig0) != string(tx.Sig0) || string(got.Sig1) != string(tx.Sig1) {
		t.Error("byte-field round-trip mismatch")
	}
}

package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// asOperator puts a verified operator in the request context, exactly as
// OperatorAuth does after a transmission verifies.
func asOperator(r *http.Request, operatorID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), operatorContextKey{}, VerifiedOperator{OperatorID: operatorID, Idx0: 0, Idx1: 1}))
}

func operatorNodeBody(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newNodeKeyB64(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func postOperatorNode(t *testing.T, h http.Handler, operatorID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/operators/nodes", bytes.NewReader(body))
	if operatorID != "" {
		req = asOperator(req, operatorID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// seedAPIOperator inserts an operators row (nodes reference it) and removes
// it, with its nodes, after the test.
func seedAPIOperator(t *testing.T, db *store.DB, id string) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO operators (id, name, email) VALUES ($1, $1, $1 || '@test.internal')`, id,
	); err != nil {
		t.Fatalf("seed operator %s: %v", id, err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DELETE FROM nodes WHERE operator_id = $1`, id) //nolint:errcheck
		db.Pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id)      //nolint:errcheck
	})
}

// Without a verified operator in context the handler refuses on its own, so
// a mis-wiring fails closed. No database is touched (db is nil).
func TestRegisterOperatorNode_NoOperator_401(t *testing.T) {
	h := handleRegisterOperatorNode(nil)
	rec := postOperatorNode(t, h, "", operatorNodeBody(t, map[string]any{
		"node_id": uuid.NewString(), "public_key": newNodeKeyB64(t),
	}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body)
	}
}

// Bad input is refused before any database work (db is nil, so reaching the
// store would panic). In particular a person-derived field is refused.
func TestRegisterOperatorNode_BadInput_400(t *testing.T) {
	h := handleRegisterOperatorNode(nil)
	id := uuid.NewString()
	key := newNodeKeyB64(t)
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))
	cases := map[string]map[string]any{
		"hostname attached":      {"node_id": id, "public_key": key, "hostname": "alice-laptop"},
		"country attached":       {"node_id": id, "public_key": key, "country_code": "US"},
		"participant attached":   {"node_id": id, "public_key": key, "participant_id": uuid.NewString()},
		"not a uuid":             {"node_id": "node-1", "public_key": key},
		"uppercase uuid":         {"node_id": "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11", "public_key": key},
		"missing node_id":        {"public_key": key},
		"key not base64":         {"node_id": id, "public_key": "not base64!"},
		"key wrong length":       {"node_id": id, "public_key": short},
		"missing key":            {"node_id": id},
		"unsupported algo":       {"node_id": id, "public_key": key, "algo": "rsa"},
		"key as raw bytes array": {"node_id": id, "public_key": []int{1, 2, 3}},
	}
	for name, body := range cases {
		rec := postOperatorNode(t, h, "op-any", operatorNodeBody(t, body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rec.Code, rec.Body)
		}
	}
}

func TestRegisterOperatorNode_CreatedThenIdempotentThenConflicts(t *testing.T) {
	db := connectAPITestDB(t)
	seedAPIOperator(t, db, "op-api-a")
	seedAPIOperator(t, db, "op-api-b")
	h := handleRegisterOperatorNode(db)
	id := uuid.NewString()
	key := newNodeKeyB64(t)
	body := operatorNodeBody(t, map[string]any{"node_id": id, "public_key": key, "algo": "ed25519"})

	if rec := postOperatorNode(t, h, "op-api-a", body); rec.Code != http.StatusCreated {
		t.Fatalf("first registration = %d, want 201: %s", rec.Code, rec.Body)
	}
	rec := postOperatorNode(t, h, "op-api-a", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat registration = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["node_id"] != id {
		t.Errorf("response = %s (%v), want node_id %s", rec.Body, err, id)
	}

	otherKey := operatorNodeBody(t, map[string]any{"node_id": id, "public_key": newNodeKeyB64(t)})
	if rec := postOperatorNode(t, h, "op-api-a", otherKey); rec.Code != http.StatusConflict {
		t.Errorf("same node, different key = %d, want 409", rec.Code)
	}
	if rec := postOperatorNode(t, h, "op-api-b", body); rec.Code != http.StatusConflict {
		t.Errorf("another operator's node = %d, want 409", rec.Code)
	}
	owner, err := store.NodeOperator(context.Background(), db, id)
	if err != nil || owner != "op-api-a" {
		t.Errorf("owner after refused writes = %q, %v; want op-api-a", owner, err)
	}
}

// Through the real api.New wiring: the route requires an operator
// transmission. No header is a 401 and nothing is written; a valid signed
// transmission reaches the handler and registers the node to that operator.
func TestRegisterOperatorNode_ThroughServerWiring(t *testing.T) {
	db := connectAPITestDB(t)
	seedAPIOperator(t, db, "op-wired")
	privs, km := testKeyset(t)
	fv := &fakeVerifier{km: km}
	s := New(db, orchestrator.NewNodeRegistry(), nil, "", "", "", nil, fv, "soholink-test")
	h := s.srv.Handler

	id := uuid.NewString()
	body := operatorNodeBody(t, map[string]any{"node_id": id, "public_key": newNodeKeyB64(t)})

	req := httptest.NewRequest(http.MethodPost, "/operators/nodes", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no operator header = %d, want 401: %s", rec.Code, rec.Body)
	}
	if _, err := store.NodeOperator(context.Background(), db, id); err == nil {
		t.Fatal("a node was written without an operator transmission")
	}

	tx := signedTransmission(t, privs, "op-wired", 1, 0, 1)
	req = httptest.NewRequest(http.MethodPost, "/operators/nodes", bytes.NewReader(body))
	req.Header.Set(OperatorHeader, EncodeOperatorHeader(tx))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signed registration = %d, want 201: %s", rec.Code, rec.Body)
	}
	if owner, err := store.NodeOperator(context.Background(), db, id); err != nil || owner != "op-wired" {
		t.Errorf("owner = %q, %v; want op-wired", owner, err)
	}
}

// With no operator verifier configured the route is not mounted: the request
// falls through to the SPIFFE subtree (degraded here), never to the handler.
func TestRegisterOperatorNode_NotMountedWithoutVerifier(t *testing.T) {
	s := New(nil, orchestrator.NewNodeRegistry(), nil, "", "", "", nil, nil, "soholink-test")
	body := operatorNodeBody(t, map[string]any{"node_id": uuid.NewString(), "public_key": newNodeKeyB64(t)})
	req := httptest.NewRequest(http.MethodPost, "/operators/nodes", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
		t.Fatalf("route served without an operator verifier: %d", rec.Code)
	}
}

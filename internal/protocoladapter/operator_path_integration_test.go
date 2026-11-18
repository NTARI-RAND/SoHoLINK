//go:build integration

package protocoladapter

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	protoidentity "github.com/NTARI-RAND/sohocloud-protocol/identity"
	"github.com/NTARI-RAND/sohocloud-protocol/listing"
	"github.com/NTARI-RAND/sohocloud-protocol/liveness"
	"github.com/NTARI-RAND/sohocloud-protocol/transport/httpjson"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/api"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// seedAdapterOperator inserts an operators row and removes it, with its
// nodes, after the test.
func seedAdapterOperator(t *testing.T, db *store.DB, id string) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO operators (id, name, email) VALUES ($1, $1, $1 || '@test.internal')`, id,
	); err != nil {
		t.Fatalf("seed operator %s: %v", id, err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DELETE FROM jobs WHERE node_id IN (SELECT id FROM nodes WHERE operator_id = $1)`, id) //nolint:errcheck
		db.Pool.Exec(context.Background(), `DELETE FROM nodes WHERE operator_id = $1`, id)                                        //nolint:errcheck
		db.Pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id)                                             //nolint:errcheck
	})
}

// operatorNode registers a person-free node for operatorID and returns its id
// and private key.
func operatorNode(t *testing.T, db *store.DB, operatorID string) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv := testKeypair(t)
	var id string
	if err := db.Pool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterOperatorNode(context.Background(), db, operatorID, id, pub); err != nil {
		t.Fatalf("RegisterOperatorNode: %v", err)
	}
	return id, priv
}

// The adapter's bind accepts a verified operator only for nodes that operator
// registered; the SPIFFE path is unchanged.
func TestAdapter_Bind_OperatorPath(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	seedAdapterOperator(t, f.db, "op-bind")
	seedAdapterOperator(t, f.db, "op-bind-other")
	opNode, _ := operatorNode(t, f.db, "op-bind")
	asOp := func(id string) context.Context { return identity.WithOperatorID(context.Background(), id) }

	if err := f.adapter.bind(asOp("op-bind"), protoidentity.NodeID(opNode)); err != nil {
		t.Errorf("registering operator: got %v, want nil", err)
	}
	if err := f.adapter.bind(asOp("op-bind-other"), protoidentity.NodeID(opNode)); !errors.Is(err, ErrIdentityMismatch) {
		t.Errorf("another operator: got %v, want ErrIdentityMismatch", err)
	}
	if err := f.adapter.bind(asOp("op-bind"), protoidentity.NodeID(f.nodeID)); !errors.Is(err, ErrIdentityMismatch) {
		t.Errorf("operator against a participant's node: got %v, want ErrIdentityMismatch", err)
	}
	if err := f.adapter.bind(asOp("op-bind"), "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrIdentityMismatch) {
		t.Errorf("operator against an unknown node: got %v, want ErrIdentityMismatch", err)
	}
	if err := f.adapter.bind(context.Background(), protoidentity.NodeID(opNode)); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("no identity: got %v, want ErrNoIdentity", err)
	}
	// A node's own SVID still binds to its own node — and only its own.
	if err := f.adapter.bind(f.ctx, protoidentity.NodeID(f.nodeID)); err != nil {
		t.Errorf("SPIFFE path, own node: got %v, want nil", err)
	}
	if err := f.adapter.bind(f.ctx, protoidentity.NodeID(opNode)); !errors.Is(err, ErrIdentityMismatch) {
		t.Errorf("SPIFFE path, operator's node: got %v, want ErrIdentityMismatch", err)
	}
}

// e2eVerifier stands in for operator.Repository: it holds each operator's 7
// active keys and accepts every non-replayed transmission. Signature checks
// are the real protocol Verify, run by api.OperatorAuth.
type e2eVerifier struct {
	keys map[string]map[int]operator.KeyRecord
}

func (v e2eVerifier) GetActiveKeyMap(_ context.Context, id string) (map[int]operator.KeyRecord, error) {
	return v.keys[id], nil
}
func (v e2eVerifier) CheckAndAdvance(context.Context, string, string, uint64, []byte, string, time.Time) error {
	return nil
}
func (v e2eVerifier) RecordUsageAndExpire(context.Context, string, int, int) (bool, error) {
	return false, nil
}

func operatorKeyset(t *testing.T) ([]ed25519.PrivateKey, map[int]operator.KeyRecord) {
	t.Helper()
	privs := make([]ed25519.PrivateKey, operator.KeyIndexCount)
	km := make(map[int]operator.KeyRecord, operator.KeyIndexCount)
	for i := range privs {
		pub, priv := testKeypair(t)
		privs[i] = priv
		km[i] = operator.KeyRecord{PublicKey: pub, Algo: operator.AlgoEd25519}
	}
	return privs, km
}

// operatorTransport signs a fresh operator transmission onto every request,
// as Cloudy's relay does.
type operatorTransport struct {
	t          *testing.T
	operatorID string
	privs      []ed25519.PrivateKey
	mu         sync.Mutex
	seq        uint64
}

func (o *operatorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	o.mu.Lock()
	o.seq++
	seq := o.seq
	o.mu.Unlock()
	nonce := make([]byte, operator.MinNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	tx := operator.OperatorTransmission{
		OperatorID: o.operatorID,
		TsUnixNano: time.Now().UnixNano(),
		Nonce:      nonce,
		Seq:        seq,
		Algo:       operator.AlgoEd25519,
	}
	tx.Sign(o.privs[0], o.privs[1], 0, 1)
	r = r.Clone(r.Context())
	r.Header.Set(api.OperatorHeader, api.EncodeOperatorHeader(tx))
	return http.DefaultTransport.RoundTrip(r)
}

// On the wire, through the same gate cmd/orchestrator mounts: an operator
// relays its prosumer's signed listing, heartbeat and job poll for a node it
// registered, and every call succeeds. Another operator's credential, and
// the operator's credential against a participant's node, are refused and
// change nothing.
func TestV0_OperatorPath_EndToEnd(t *testing.T) {
	f := setupAdapterFixture(t, stubFees{decl: testFeeDecl()})
	seedAdapterOperator(t, f.db, "op-wire")
	seedAdapterOperator(t, f.db, "op-wire-other")
	opNode, opNodePriv := operatorNode(t, f.db, "op-wire")

	privs, km := operatorKeyset(t)
	otherPrivs, otherKM := operatorKeyset(t)
	v := e2eVerifier{keys: map[string]map[int]operator.KeyRecord{"op-wire": km, "op-wire-other": otherKM}}
	gate := func(bare, spiffeGated http.Handler) http.Handler {
		return api.OperatorOrSPIFFE(v, "soholink", spiffeGated, bare)
	}
	srv := httptest.NewServer(NewHandler(f.adapter, nil, false, gate))
	t.Cleanup(srv.Close)

	client := func(opID string, p []ed25519.PrivateKey) *httpjson.Client {
		return &httpjson.Client{BaseURL: srv.URL, HTTP: &http.Client{Transport: &operatorTransport{t: t, operatorID: opID, privs: p}}}
	}
	cloudy := client("op-wire", privs)
	intruder := client("op-wire-other", otherPrivs)
	ctx := context.Background()

	l := listing.CapabilityListing{
		NodeID:   protoidentity.NodeID(opNode),
		Class:    listing.ClassStandard,
		Capacity: listing.Capacity{VCPUs: 4, MemMB: 8192, DiskMB: 100 * 1024},
		OptIn:    listing.WorkloadOptIn{Compute: true},
		IssuedAt: time.Now().UTC(),
		Seq:      1,
	}
	l.Sign(opNodePriv)

	// Another operator relaying a validly signed listing is refused first.
	if err := intruder.SubmitListing(ctx, l); err == nil {
		t.Fatal("another operator's credential submitted a listing for op-wire's node")
	}
	if _, ok := f.registry.Get(opNode); ok {
		t.Fatal("refused listing reached the registry")
	}

	if err := cloudy.SubmitListing(ctx, l); err != nil {
		t.Fatalf("SubmitListing via operator: %v", err)
	}
	entry, ok := f.registry.Get(opNode)
	if !ok || entry.OperatorID != "op-wire" || entry.ParticipantID != "" {
		t.Fatalf("registry entry = %+v (present %v); want OperatorID op-wire and no participant", entry, ok)
	}

	hb := liveness.Heartbeat{NodeID: protoidentity.NodeID(opNode), SentAt: time.Now().UTC(), Seq: 1}
	hb.Sign(opNodePriv)
	if err := cloudy.Heartbeat(ctx, hb); err != nil {
		t.Fatalf("Heartbeat via operator: %v", err)
	}
	hb2 := liveness.Heartbeat{NodeID: protoidentity.NodeID(opNode), SentAt: time.Now().UTC(), Seq: 2}
	hb2.Sign(opNodePriv)
	if err := intruder.Heartbeat(ctx, hb2); err == nil {
		t.Error("another operator's credential sent a heartbeat for op-wire's node")
	}

	assignments, err := cloudy.PollJobs(ctx, protoidentity.NodeID(opNode))
	if err != nil {
		t.Fatalf("PollJobs via operator: %v", err)
	}
	if len(assignments) != 0 {
		t.Errorf("PollJobs = %d assignments, want 0 (no job was seeded)", len(assignments))
	}
	if _, err := intruder.PollJobs(ctx, protoidentity.NodeID(opNode)); err == nil {
		t.Error("another operator's credential polled op-wire's node")
	}

	// The operator's credential does not reach a participant's node.
	if _, err := cloudy.PollJobs(ctx, protoidentity.NodeID(f.nodeID)); err == nil {
		t.Error("operator credential polled a participant's node")
	}

	// The node row stayed person-free through the listing update.
	var hostname, country *string
	if err := f.db.Pool.QueryRow(ctx, `SELECT hostname, country_code FROM nodes WHERE id = $1`, opNode).Scan(&hostname, &country); err != nil {
		t.Fatal(err)
	}
	if hostname != nil || country != nil {
		t.Errorf("operator node gained hostname %v / country %v", hostname, country)
	}

	// No header at all falls to the SPIFFE path, which refuses a request
	// without a client certificate.
	resp, err := http.Post(srv.URL+"/v0/heartbeat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no credential = %d, want 401", resp.StatusCode)
	}
}

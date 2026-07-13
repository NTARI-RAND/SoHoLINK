package protocoladapter

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	protoidentity "github.com/NTARI-RAND/sohocloud-protocol/identity"
	"github.com/NTARI-RAND/sohocloud-protocol/listing"
	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// stubFees is a FeeSource returning a fixed declaration or error.
type stubFees struct {
	decl fees.FeeDeclaration
	err  error
}

func (s stubFees) CurrentFeeDeclaration(ctx context.Context, coordinatorID string) (fees.FeeDeclaration, error) {
	return s.decl, s.err
}

func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return pub, priv
}

// ── class mapping ─────────────────────────────────────────────────────────────

func TestNodeClassForComputeClass(t *testing.T) {
	cases := []struct {
		in      listing.ComputeClass
		want    string
		wantErr bool
	}{
		{listing.ClassServer, "A", false},
		{listing.ClassStandard, "B", false},
		{listing.ClassMicro, "C", false},
		{listing.ComputeClass("mainframe"), "", true},
		{listing.ComputeClass(""), "", true},
	}
	for _, tc := range cases {
		got, err := nodeClassForComputeClass(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("nodeClassForComputeClass(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("nodeClassForComputeClass(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("nodeClassForComputeClass(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── assignment mapping + coordinator signature ───────────────────────────────

func TestAssignmentForJob_WorkloadAndPrinterKindMapping(t *testing.T) {
	terms := fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500}
	now := time.Now().UTC()
	id := protoidentity.NodeID("node-1")

	cases := []struct {
		workloadType    string
		wantWorkload    string
		wantPrinterKind string
	}{
		{"app_hosting", "compute", ""},
		{"batch_compute", "compute", ""},
		{"ai_inference", "compute", ""},
		{"object_storage", "compute", ""},
		{"cdn_edge", "compute", ""},
		{"print_traditional", "print", "traditional"},
		{"print_3d", "print", "threed"},
	}
	for _, tc := range cases {
		asg := assignmentForJob(store.DispatchedJob{
			JobID:        "job-1",
			Image:        "soholink/worker@sha256:aaaa",
			WorkloadType: tc.workloadType,
		}, id, terms, now)
		if asg.Spec.Workload != tc.wantWorkload {
			t.Errorf("%s: Workload = %q, want %q", tc.workloadType, asg.Spec.Workload, tc.wantWorkload)
		}
		if asg.Spec.PrinterKind != tc.wantPrinterKind {
			t.Errorf("%s: PrinterKind = %q, want %q", tc.workloadType, asg.Spec.PrinterKind, tc.wantPrinterKind)
		}
		if asg.Fee != terms {
			t.Errorf("%s: Fee = %+v, want %+v", tc.workloadType, asg.Fee, terms)
		}
	}
}

func TestAssignment_SignatureVerifiesAgainstCoordinatorKey(t *testing.T) {
	pub, priv := testKeypair(t)
	asg := assignmentForJob(store.DispatchedJob{
		JobID:        "job-1",
		Image:        "soholink/worker@sha256:aaaa",
		WorkloadType: "batch_compute",
	}, protoidentity.NodeID("node-1"), fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500}, time.Now().UTC())
	asg.Sign(priv)
	if !asg.Verify(pub) {
		t.Fatal("assignment signature does not verify against coordinator public key")
	}
	otherPub, _ := testKeypair(t)
	if asg.Verify(otherPub) {
		t.Fatal("assignment signature verified against the WRONG public key")
	}
}

// ── adapter bind (defense-in-depth re-check, no DB needed) ────────────────────

func TestAdapter_Bind(t *testing.T) {
	a := New(nil, orchestrator.NewNodeRegistry(), stubFees{}, "soholink", nil, nil)

	// No identity in context → ErrNoIdentity.
	if err := a.bind(context.Background(), "node-1"); err != ErrNoIdentity {
		t.Errorf("no identity: got %v, want ErrNoIdentity", err)
	}

	// Mismatched identity → ErrIdentityMismatch.
	wrong := identity.WithSPIFFEID(context.Background(),
		spiffeid.RequireFromString("spiffe://soholink.org/node/other-node"))
	if err := a.bind(wrong, "node-1"); err != ErrIdentityMismatch {
		t.Errorf("mismatch: got %v, want ErrIdentityMismatch", err)
	}

	// Matching identity → nil.
	right := identity.WithSPIFFEID(context.Background(),
		spiffeid.RequireFromString("spiffe://soholink.org/node/node-1"))
	if err := a.bind(right, "node-1"); err != nil {
		t.Errorf("match: got %v, want nil", err)
	}
}

// ── handler: /v0/fees plain route ─────────────────────────────────────────────

func TestHandler_Fees_NotPublished404(t *testing.T) {
	a := New(nil, orchestrator.NewNodeRegistry(), stubFees{err: operator.ErrNoFeeDeclaration}, "soholink", nil, nil)
	h := NewHandler(a, nil, true, nil) // degraded: exercises only the plain /v0/fees route (no bundle needed)

	r := httptest.NewRequest(http.MethodGet, "/v0/fees", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 before any declaration is published, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_Fees_ServesSignedDeclaration(t *testing.T) {
	pub, priv := testKeypair(t)
	decl := fees.FeeDeclaration{
		CoordinatorID: "soholink",
		Terms:         fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500},
		EffectiveAt:   time.Now().UTC().Truncate(time.Nanosecond),
		Seq:           1,
	}
	decl.Sign(priv)

	a := New(nil, orchestrator.NewNodeRegistry(), stubFees{decl: decl}, "soholink", nil, nil)
	h := NewHandler(a, nil, true, nil) // degraded: exercises only the plain /v0/fees route (no bundle needed)

	r := httptest.NewRequest(http.MethodGet, "/v0/fees", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got fees.FeeDeclaration
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Verify(pub) {
		t.Fatal("re-served declaration does not round-trip signature verification")
	}
}

func TestHandler_Fees_ServedPlainInDegradedMode(t *testing.T) {
	a := New(nil, orchestrator.NewNodeRegistry(), stubFees{err: operator.ErrNoFeeDeclaration}, "soholink", nil, nil)
	h := NewHandler(a, nil, true, nil)

	// Fees stays reachable (404 = honest "nothing published", not 503)...
	r := httptest.NewRequest(http.MethodGet, "/v0/fees", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("fees in degraded mode: expected 404, got %d", w.Code)
	}

	// ...while the protected subtree serves 503.
	r = httptest.NewRequest(http.MethodPost, "/v0/heartbeat", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("protected route in degraded mode: expected 503, got %d", w.Code)
	}
}

// nonDegradedHandlerTestMarker documents that the handler below is built
// NON-degraded (a nil source is tolerated: the no-mTLS path 401s before the
// bundle is ever consulted).
func TestHandler_ProtectedRoutesRequireTLS(t *testing.T) {
	a := New(nil, orchestrator.NewNodeRegistry(), stubFees{}, "soholink", nil, nil)
	h := NewHandler(a, nil, false, nil) // NON-degraded: the protected subtree must return 401 (not 503) without mTLS

	// Plain HTTP request (no TLS peer certificate) → RequireSPIFFE 401.
	r := httptest.NewRequest(http.MethodPost, "/v0/heartbeat", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without mTLS, got %d", w.Code)
	}
}

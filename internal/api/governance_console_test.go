package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/notify"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// fakeConsoleGovRepo is an in-memory consoleGovRepo for the :8090 admin render
// smoke tests: no DB. It returns fixed queue/detail/fee-history shapes.
type fakeConsoleGovRepo struct {
	summaries []operator.OperatorSummary
	counts    operator.OperatorStatusCounts
	detail    operator.OperatorDetail
	detailErr error
	history   []operator.FeeDeclarationView
	nextSeq   uint64
}

func (f *fakeConsoleGovRepo) ListOperators(_ context.Context) ([]operator.OperatorSummary, operator.OperatorStatusCounts, error) {
	return f.summaries, f.counts, nil
}

func (f *fakeConsoleGovRepo) GetOperator(_ context.Context, _ string) (operator.OperatorDetail, error) {
	if f.detailErr != nil {
		return operator.OperatorDetail{}, f.detailErr
	}
	return f.detail, nil
}

func (f *fakeConsoleGovRepo) FeeHistory(_ context.Context, _ string) ([]operator.FeeDeclarationView, error) {
	return f.history, nil
}

func (f *fakeConsoleGovRepo) NextSeq(_ context.Context, _ string) (uint64, error) {
	return f.nextSeq, nil
}

// newConsoleGovServer builds a loopback-guarded :8090 governance handler wired to
// the real Stage-3 admin templates and the given fake read/action repos.
func newConsoleGovServer(t *testing.T, read consoleGovRepo, action governanceRepo) http.Handler {
	t.Helper()
	if action == nil {
		action = &fakeGovRepo{}
	}
	g := newTestGovServer(t, action, notify.NewLogNotifier())
	if err := g.ConfigureConsole(read, "../../web/templates"); err != nil {
		t.Fatalf("ConfigureConsole: %v", err)
	}
	return govMux(g)
}

// getGov issues a GET from a loopback source and returns the recorder.
func getGov(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:5555" // loopback source
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// sampleGovRead is a populated fake covering the queue, detail, and fee history.
func sampleGovRead() *fakeConsoleGovRepo {
	passed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return &fakeConsoleGovRepo{
		summaries: []operator.OperatorSummary{
			{
				ID: "cloudy", Name: "Cloudy", Email: "ops@cloudy.example",
				Status: "active", OnboardingState: "active",
				EmailVerified: true, ConformancePassed: true, ActiveKeyCount: 7,
			},
			{
				ID: "fruitful", Name: "Fruitful", Email: "ops@fruitful.example",
				Status: "active", OnboardingState: "pending_verification",
				EmailVerified: false, ConformancePassed: false, ActiveKeyCount: 7,
			},
		},
		counts: operator.OperatorStatusCounts{
			Pending: 1, Active: 1, Total: 2,
		},
		detail: operator.OperatorDetail{
			ID: "cloudy", Name: "Cloudy", Email: "ops@cloudy.example",
			Status: "active", OnboardingState: "active",
			EmailVerified: true, ConformancePassedAt: &passed,
			ActiveKeyCount: 7, KeysetHashMatches: true,
			Keys: []operator.KeyView{
				{KeyIndex: 0, PublicKeyB64Trunc: "AAAABBBBCCCC…", Algo: "ed25519", State: "active"},
			},
			LatestRun: &operator.ConformanceRunView{
				RunID: "run-1", Status: "passed", StartedAt: passed,
				Suites: []operator.SuiteVerdictView{
					{Suite: operator.SuiteA, Graded: true, Passed: true},
					{Suite: operator.SuiteB, Graded: true, Passed: false, Detail: "byte 3 differs"},
					{Suite: operator.SuiteC, Graded: false, Passed: false},
				},
			},
		},
		history: []operator.FeeDeclarationView{
			{CoordinatorID: "soholink", ContributorShareBps: 6500, PlatformFeeBps: 3500, Seq: 1, EffectiveAt: passed, IsCurrent: true},
		},
		nextSeq: 2,
	}
}

// TestAdminConsole_Pages_Render200 verifies the four admin GET pages render 200
// with their expected content, through the real admin templates and layout, from
// a loopback source.
func TestAdminConsole_Pages_Render200(t *testing.T) {
	read := sampleGovRead()
	// The fees page reads the current signed declaration via the action repo.
	action := &fakeGovRepo{
		current: &fees.FeeDeclaration{
			CoordinatorID: "soholink",
			Terms:         fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500},
			EffectiveAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			Seq:           1,
			Signature:     []byte("sig-bytes-here"),
		},
	}
	h := newConsoleGovServer(t, read, action)

	cases := []struct {
		path string
		want string
	}{
		{"/admin/operators", "Operators"},
		{"/admin/operators", "cloudy"},
		{"/admin/operators/cloudy", "Registered public keys"},
		{"/admin/fees", "Fee declaration"},
		{"/admin/fees", "Publish a new declaration"},
		{"/admin/messaging", "Messaging"},
	}
	for _, c := range cases {
		rec := getGov(h, c.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200; body=%q", c.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("GET %s: body missing %q", c.path, c.want)
		}
	}
}

// TestAdminConsole_UsesAdminLayout confirms the admin pages render with the
// governance nav (admin_layout.html) and NOT the public operator/member nav —
// the governance-separation invariant at the template layer.
func TestAdminConsole_UsesAdminLayout(t *testing.T) {
	h := newConsoleGovServer(t, sampleGovRead(), nil)
	body := getGov(h, "/admin/operators").Body.String()
	for _, want := range []string{
		"Local-only governance surface", // admin_layout banner
		`href="/admin/fees"`,            // governance nav link
		`href="/admin/messaging"`,       // governance nav link
		"loopback only",                 // admin_layout footer
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/operators body missing admin-layout marker %q", want)
		}
	}
	// Must NOT carry the public operator/member nav affordances.
	for _, notWant := range []string{"Become an operator", "Member portal"} {
		if strings.Contains(body, notWant) {
			t.Errorf("/admin/operators unexpectedly contains public-nav marker %q", notWant)
		}
	}
}

// TestAdminConsole_DetailAutoActivation confirms the detail page shows the
// auto-activation recap and carries NO activate form (activation is automatic).
func TestAdminConsole_DetailAutoActivation(t *testing.T) {
	h := newConsoleGovServer(t, sampleGovRead(), nil)
	body := getGov(h, "/admin/operators/cloudy").Body.String()
	if !strings.Contains(body, "Activation is automatic") {
		t.Errorf("detail page missing auto-activation recap")
	}
	if strings.Contains(body, `action="/admin/operators/cloudy/activate"`) {
		t.Errorf("detail page must not contain an activate form")
	}
	// The revoke danger-zone form (an existing POST action) should be present.
	if !strings.Contains(body, `action="/admin/operators/cloudy/revoke"`) {
		t.Errorf("detail page missing revoke form")
	}
}

// TestAdminConsole_DetailNotFound_404 maps ErrOperatorNotFound to a 404.
func TestAdminConsole_DetailNotFound_404(t *testing.T) {
	read := &fakeConsoleGovRepo{detailErr: operator.ErrOperatorNotFound}
	h := newConsoleGovServer(t, read, nil)
	rec := getGov(h, "/admin/operators/ghost")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /admin/operators/ghost: status %d, want 404", rec.Code)
	}
}

// TestAdminConsole_FeesEmptyState renders the empty-state card when no fee
// declaration has been published.
func TestAdminConsole_FeesEmptyState(t *testing.T) {
	// action repo with no current declaration -> ErrNoFeeDeclaration.
	h := newConsoleGovServer(t, &fakeConsoleGovRepo{}, &fakeGovRepo{})
	rec := getGov(h, "/admin/fees")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/fees: status %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No fee declaration has been published yet") {
		t.Errorf("/admin/fees empty state card not rendered")
	}
}

// TestAdminConsole_RejectsNonLoopbackSource confirms every admin GET page is
// refused 403 from a non-loopback source — the governance-separation / SSRF
// defense-in-depth guard applies to the console GET routes, not just the POSTs.
func TestAdminConsole_RejectsNonLoopbackSource(t *testing.T) {
	h := newConsoleGovServer(t, sampleGovRead(), nil)
	for _, path := range []string{
		"/admin/operators",
		"/admin/operators/cloudy",
		"/admin/fees",
		"/admin/messaging",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.9:4444" // NOT loopback
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s from non-loopback source: status %d, want 403", path, rec.Code)
		}
	}
}

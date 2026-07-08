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

// fakeConsoleRepo is an in-memory consoleRepo for the render smoke tests: no DB.
// It returns a fixed operator detail and (optionally) a current fee declaration.
type fakeConsoleRepo struct {
	detail     operator.OperatorDetail
	detailErr  error
	current    *fees.FeeDeclaration
	currentErr error
}

func (f *fakeConsoleRepo) GetOperator(_ context.Context, _ string) (operator.OperatorDetail, error) {
	if f.detailErr != nil {
		return operator.OperatorDetail{}, f.detailErr
	}
	return f.detail, nil
}

func (f *fakeConsoleRepo) CurrentFeeDeclaration(_ context.Context, _ string) (fees.FeeDeclaration, error) {
	if f.currentErr != nil {
		return fees.FeeDeclaration{}, f.currentErr
	}
	if f.current == nil {
		return fees.FeeDeclaration{}, operator.ErrNoFeeDeclaration
	}
	return *f.current, nil
}

// newConsoleTestServer builds an OnboardingServer with the real Stage-3 templates
// (../../web/templates) and the given fake read repo, ready to serve GET pages.
func newConsoleTestServer(t *testing.T, read consoleRepo) http.Handler {
	t.Helper()
	srv := NewOnboardingServer(&stubOperatorRepo{}, notify.NewLogNotifier(), nil)
	if err := srv.ConfigureConsole(read, "../../web/templates", "soholink"); err != nil {
		t.Fatalf("ConfigureConsole: %v", err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return mux
}

// getBody issues a GET and returns status + body, failing the test on a non-200.
func getBody(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200; body=%q", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestConsole_StaticPages_Render200 verifies the static funnel pages render 200
// with their expected content, through the real templates and layout.
func TestConsole_StaticPages_Render200(t *testing.T) {
	h := newConsoleTestServer(t, &fakeConsoleRepo{})

	cases := []struct {
		path string
		want string
	}{
		{"/", "Onboard your platform"},
		{"/operators/apply", "Platform details"},
		{"/operators/cloudy/keys", "Register keys"},
		{"/operators/cloudy/verify", "Verify contact"},
		{"/operators/cloudy/conformance", "Prove conformance"},
	}
	for _, c := range cases {
		body := getBody(t, h, c.path)
		if !strings.Contains(body, c.want) {
			t.Errorf("GET %s: body missing %q", c.path, c.want)
		}
		// Layout re-parent: operator nav present, member nav demoted to muted link.
		if !strings.Contains(body, "Become an operator") {
			t.Errorf("GET %s: operator nav 'Become an operator' missing", c.path)
		}
		if !strings.Contains(body, ">Member portal<") {
			t.Errorf("GET %s: muted 'Member portal' link missing", c.path)
		}
	}
}

// TestConsole_IDIsEscaped confirms operator ids render through html/template
// (auto-escaped) on the step pages.
func TestConsole_IDIsEscaped(t *testing.T) {
	h := newConsoleTestServer(t, &fakeConsoleRepo{})
	body := getBody(t, h, "/operators/a%22%3Cb/keys")
	if strings.Contains(body, `a"<b`) {
		t.Errorf("operator id rendered unescaped: %q", body)
	}
	if !strings.Contains(body, "&lt;") && !strings.Contains(body, "&#34;") {
		t.Errorf("expected escaped operator id in body")
	}
}

// TestConsole_Fees_Empty renders the no-declaration empty state.
func TestConsole_Fees_Empty(t *testing.T) {
	h := newConsoleTestServer(t, &fakeConsoleRepo{}) // current nil -> ErrNoFeeDeclaration
	body := getBody(t, h, "/fees")
	if !strings.Contains(body, "No fee declaration has been published") {
		t.Errorf("GET /fees empty: missing empty-state copy; body=%q", body)
	}
}

// TestConsole_Fees_Published renders the current signed declaration.
func TestConsole_Fees_Published(t *testing.T) {
	decl := fees.FeeDeclaration{
		CoordinatorID: "soholink",
		Terms:         fees.Terms{ContributorShareBps: 6500, PlatformFeeBps: 3500},
		EffectiveAt:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Seq:           3,
		Signature:     []byte("sig-bytes-placeholder"),
	}
	h := newConsoleTestServer(t, &fakeConsoleRepo{current: &decl})
	body := getBody(t, h, "/fees")
	for _, want := range []string{"6500", "3500", "Declaration detail"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /fees published: missing %q", want)
		}
	}
}

// TestConsole_Dashboard_PreActivation renders the awaiting-admission recap for a
// not-yet-active operator.
func TestConsole_Dashboard_PreActivation(t *testing.T) {
	h := newConsoleTestServer(t, &fakeConsoleRepo{
		detail: operator.OperatorDetail{
			ID:              "cloudy",
			Name:            "Cloudy",
			Status:          "active",
			OnboardingState: "verified",
			EmailVerified:   true,
			ActiveKeyCount:  7,
			Lifecycle:       operator.LifecycleView{CreatedAt: time.Now()},
			Keys:            []operator.KeyView{{KeyIndex: 0, State: "active"}},
		},
	})
	body := getBody(t, h, "/operators/cloudy/dashboard")
	if !strings.Contains(body, "Awaiting admission") {
		t.Errorf("dashboard pre-activation: missing awaiting-admission recap; body=%q", body)
	}
}

// TestConsole_Dashboard_Active renders the full dashboard for an active operator.
func TestConsole_Dashboard_Active(t *testing.T) {
	now := time.Now()
	h := newConsoleTestServer(t, &fakeConsoleRepo{
		detail: operator.OperatorDetail{
			ID:                   "cloudy",
			Name:                 "Cloudy",
			Status:               "active",
			OnboardingState:      "active",
			EmailVerified:        true,
			ConformancePassedAt:  &now,
			ActiveKeyCount:       7,
			TransmissionsLast24h: 42,
			Lifecycle:            operator.LifecycleView{CreatedAt: now, Activated: true},
			Keys: []operator.KeyView{
				{KeyIndex: 0, PublicKeyB64Trunc: "AAAA…", State: "active", UsageCount: 1, ExpirationThreshold: 365},
			},
		},
	})
	body := getBody(t, h, "/operators/cloudy/dashboard")
	for _, want := range []string{"Signing keys", "Transmissions / 24h", "42"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard active: missing %q", want)
		}
	}
}

// TestConsole_Dashboard_NotFound maps ErrOperatorNotFound to 404.
func TestConsole_Dashboard_NotFound(t *testing.T) {
	h := newConsoleTestServer(t, &fakeConsoleRepo{detailErr: operator.ErrOperatorNotFound})
	req := httptest.NewRequest(http.MethodGet, "/operators/ghost/dashboard", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET dashboard ghost: status %d, want 404", rec.Code)
	}
}

// stubOperatorRepo satisfies operatorRepo so NewOnboardingServer can be built for
// the GET-only render tests; none of its methods are exercised here.
type stubOperatorRepo struct{}

func (stubOperatorRepo) CreateOperator(context.Context, string, string, string, string) error {
	return nil
}
func (stubOperatorRepo) AddKeys(context.Context, string, [][]byte, string, []int) error { return nil }
func (stubOperatorRepo) IssueEmailCode(context.Context, string, string) (string, string, error) {
	return "", "", nil
}
func (stubOperatorRepo) CheckEmailCode(context.Context, string, string, string) error { return nil }
func (stubOperatorRepo) StartConformanceRun(context.Context, string) (string, []operator.ChallengeA, []operator.ChallengeB, error) {
	return "", nil, nil, nil
}
func (stubOperatorRepo) GradeSuiteA(context.Context, string, string, operator.ResponseA) (operator.ChallengeResult, error) {
	return operator.ChallengeResult{}, nil
}
func (stubOperatorRepo) GradeSuiteB(context.Context, string, string, operator.ResponseB) (operator.ChallengeResult, error) {
	return operator.ChallengeResult{}, nil
}
func (stubOperatorRepo) GradeSuiteC(context.Context, string, string) (operator.ChallengeResult, error) {
	return operator.ChallengeResult{}, nil
}
func (stubOperatorRepo) FinalizeRun(context.Context, string, string) (bool, error) { return false, nil }
func (stubOperatorRepo) AutoActivate(context.Context, string) error                { return nil }

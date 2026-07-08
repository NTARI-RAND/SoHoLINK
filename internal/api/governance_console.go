package api

import (
	"context"
	"encoding/base64"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// This file adds the LOCAL-ONLY :8090 GOVERNANCE console GET pages (Stage 4,
// step 3) to the GovernanceServer that already owns the step-4 POST handlers.
// It renders the Stage-3 admin_*/gov_*.html templates against the step-1 read
// models (internal/operator/readmodel.go) using the SAME per-request
// template.ParseFiles(adminLayout, page) pattern as internal/portal, EXCEPT the
// layout is admin_layout.html — never layout.html — so the admin surface renders
// with the governance nav (Operators / Fees / Messaging) and the loopback-only
// banner, and shares no template set with the public server.
//
// GOVERNANCE SEPARATION (load-bearing, CLAUDE.md / design §1): these GET pages
// are registered on the SAME loopback-bound, loopback-source-guarded mux as the
// POST handlers (registerRoutes + loopbackOnly). They are NEVER mounted on the
// public OnboardingServer. A non-loopback source is rejected 403 before any
// handler runs, exactly as for the POST routes. The admin renderTemplate lives
// here on the GovernanceServer, not on the public server, so the admin layout is
// physically unreachable from soholink.org.
//
// These handlers are READ-ONLY renders; the privileged mutations (revoke,
// disconnect, publish fees, send messages) stay on the existing POST routes,
// which the templates target with plain <form>/fetch. There is NO activate
// affordance: activation is automatic on passing both mechanical gates (design
// §3), so admin_operator_detail.html shows the auto-activation recap, not a
// button.

// consoleGovRepo is the READ-MODEL subset of *operator.Repository the :8090
// console GET pages need, layered on top of the write/action governanceRepo.
// Kept separate so a test can stub the read side independently and so the read
// surface (queue/detail/fee-history) is visibly distinct from the mutating
// action surface. Every method is a pure read (see readmodel.go).
type consoleGovRepo interface {
	ListOperators(ctx context.Context) ([]operator.OperatorSummary, operator.OperatorStatusCounts, error)
	GetOperator(ctx context.Context, operatorID string) (operator.OperatorDetail, error)
	FeeHistory(ctx context.Context, coordinatorID string) ([]operator.FeeDeclarationView, error)
	NextSeq(ctx context.Context, coordinatorID string) (uint64, error)
}

// compile-time assertion that *operator.Repository satisfies consoleGovRepo.
var _ consoleGovRepo = (*operator.Repository)(nil)

// ConfigureConsole enables the :8090 governance console GET pages on this
// server. It collects the .html template paths under templatesDir (recursively,
// like the portal) for the per-request ParseFiles render and records the
// read-model repository. It is separate from the constructor so the existing
// POST-only construction (and its tests) is unaffected; a server without this
// call registers the GET routes but renders a 500 "template not found" — the
// same failure mode as the portal with a missing template.
//
// The read repository is passed explicitly (rather than reusing the action
// governanceRepo) so the console read surface is a distinct dependency; in
// production both are the same *operator.Repository. Returns an error only if
// templatesDir cannot be walked.
func (g *GovernanceServer) ConfigureConsole(readRepo consoleGovRepo, templatesDir string) error {
	var paths []string
	if templatesDir != "" {
		if err := filepath.WalkDir(templatesDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && filepath.Ext(path) == ".html" {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	g.consoleRepo = readRepo
	g.templatePaths = paths
	return nil
}

// renderAdmin builds a fresh template set from admin_layout.html and the
// requested page file, then executes the named page template. This mirrors the
// portal's per-request ParseFiles pattern (every page defines
// {{define "content"}}, so a shared set would collide on the last parse) but is
// STRICTLY the ADMIN layout: it looks up admin_layout.html, never layout.html,
// so the governance pages cannot accidentally render with the public nav, and
// the public renderTemplate (operator_console.go) cannot reach admin_layout.
// The admin templates carry no transitional banner, so none is parsed here.
func (g *GovernanceServer) renderAdmin(w http.ResponseWriter, page string, data any) {
	var layoutPath, pagePath string
	for _, p := range g.templatePaths {
		switch filepath.Base(p) {
		case "admin_layout.html":
			layoutPath = p
		case page:
			pagePath = p
		}
	}
	if layoutPath == "" || pagePath == "" {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	tmpl, err := template.ParseFiles(layoutPath, pagePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// -----------------------------------------------------------------------------
// GET /admin/operators — operator overview queue (gov_operators.html).
// -----------------------------------------------------------------------------

// adminOperatorRow is one row of the operator queue table. It adapts
// operator.OperatorSummary field names to the names gov_operators.html reads
// (OperatorID / KeyCount rather than ID / ActiveKeyCount for the "Registered"
// column; ActiveKeyCount is surfaced separately for the "Keys" column).
type adminOperatorRow struct {
	OperatorID        string
	Name              string
	OnboardingState   string
	Status            string
	EmailVerified     bool
	ConformancePassed bool
	KeyCount          int // registered/active keys, for the "N / 7" cell
	ActiveKeyCount    int
}

// adminOperatorsData is the template data for gov_operators.html: the queue
// header counts plus the operator rows.
type adminOperatorsData struct {
	TotalCount   int
	ActiveCount  int
	PendingCount int
	ReadyCount   int // verified AND conformance passed (VerifiedPassed)
	RevokedCount int
	Operators    []adminOperatorRow
}

// handleAdminOperatorsPage renders the operator queue (gov_operators.html) from
// the ListOperators read model. Pure read; the disconnect/revoke affordances in
// the template POST to the existing action routes.
func (g *GovernanceServer) handleAdminOperatorsPage(w http.ResponseWriter, r *http.Request) {
	if g.consoleRepo == nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	summaries, counts, err := g.consoleRepo.ListOperators(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := adminOperatorsData{
		TotalCount:   counts.Total,
		ActiveCount:  counts.Active,
		PendingCount: counts.Pending,
		ReadyCount:   counts.VerifiedPassed,
		RevokedCount: counts.Revoked,
	}
	for _, s := range summaries {
		data.Operators = append(data.Operators, adminOperatorRow{
			OperatorID:        s.ID,
			Name:              s.Name,
			OnboardingState:   s.OnboardingState,
			Status:            s.Status,
			EmailVerified:     s.EmailVerified,
			ConformancePassed: s.ConformancePassed,
			KeyCount:          s.ActiveKeyCount,
			ActiveKeyCount:    s.ActiveKeyCount,
		})
	}
	g.renderAdmin(w, "gov_operators.html", data)
}

// -----------------------------------------------------------------------------
// GET /admin/operators/{id} — full operator detail (admin_operator_detail.html).
// -----------------------------------------------------------------------------

// adminKeyRow is one registered-key row for admin_operator_detail.html.
type adminKeyRow struct {
	Index       int
	PubKeyTrunc string
	State       string
}

// adminRunRow is one per-suite verdict row for the latest conformance run.
// Label is the suite letter (A/B/C); Passed drives the pass/fail badge; Detail
// carries the grader's failure reason (empty on pass). A suite issued but not
// yet graded renders as a fail with a "not yet graded" detail so the admin sees
// the incomplete state rather than a silent gap.
type adminRunRow struct {
	Label  string
	Passed bool
	Detail string
}

// adminOperatorDetailData is the template data for admin_operator_detail.html.
// It maps the OperatorDetail read model into the field names the template reads.
// CanActivate reflects the auto-activation precondition (verified + conformance
// passed + keyset-hash match); the template shows a recap sentence keyed on it,
// NOT an activate button — activation is automatic (design §3).
type adminOperatorDetailData struct {
	Name              string
	OperatorID        string
	OnboardingState   string
	Status            string
	EmailVerified     bool
	ConformancePassed bool
	KeysetMatch       bool
	KeyCount          int
	Keys              []adminKeyRow
	RunResults        []adminRunRow
	CanActivate       bool
}

// handleAdminOperatorDetailPage renders the full operator detail
// (admin_operator_detail.html): recap stat-grid, registered pubkeys, latest run
// verdicts, and the revoke danger zone (which POSTs to the existing revoke
// route). A missing operator id is a 404. Pure read.
func (g *GovernanceServer) handleAdminOperatorDetailPage(w http.ResponseWriter, r *http.Request) {
	if g.consoleRepo == nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	operatorID := r.PathValue("id")
	detail, err := g.consoleRepo.GetOperator(r.Context(), operatorID)
	if errors.Is(err, operator.ErrOperatorNotFound) {
		http.Error(w, "operator not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := adminOperatorDetailData{
		Name:              detail.Name,
		OperatorID:        detail.ID,
		OnboardingState:   detail.OnboardingState,
		Status:            detail.Status,
		EmailVerified:     detail.EmailVerified,
		ConformancePassed: detail.ConformancePassedAt != nil,
		KeysetMatch:       detail.KeysetHashMatches,
		KeyCount:          detail.ActiveKeyCount,
		// CanActivate == ReadyToActivate: verified + conformance passed + keyset
		// match (status still active). The template renders a recap, not a button.
		CanActivate: detail.ReadyToActivate,
	}
	for _, k := range detail.Keys {
		data.Keys = append(data.Keys, adminKeyRow{
			Index:       k.KeyIndex,
			PubKeyTrunc: k.PublicKeyB64Trunc,
			State:       k.State,
		})
	}
	if detail.LatestRun != nil {
		for _, s := range detail.LatestRun.Suites {
			row := adminRunRow{
				Label:  string(s.Suite),
				Passed: s.Graded && s.Passed,
			}
			switch {
			case !s.Graded:
				row.Detail = "not yet graded"
			case !s.Passed:
				row.Detail = s.Detail
			}
			data.RunResults = append(data.RunResults, row)
		}
	}
	g.renderAdmin(w, "admin_operator_detail.html", data)
}

// -----------------------------------------------------------------------------
// GET /admin/fees — current + history + compose/sign (gov_fees.html).
// -----------------------------------------------------------------------------

// adminFeeCurrent is the current signed declaration block for gov_fees.html. It
// carries the base64 signature so the admin can eyeball the signed artifact that
// is served publicly at /fees; the signing key itself never leaves this process.
type adminFeeCurrent struct {
	ContributorShareBps int
	PlatformFeeBps      int
	Seq                 uint64
	Balanced            bool
	EffectiveAt         string
	Signature           string
}

// adminFeeHistoryRow is one row of the declaration-history table.
type adminFeeHistoryRow struct {
	Seq                 uint64
	ContributorShareBps int
	PlatformFeeBps      int
	Balanced            bool
	EffectiveAt         string
}

// adminFeesData is the template data for gov_fees.html. Current is nil when no
// declaration has been published (the template renders the empty-state card).
// NextSeq pre-fills the compose form's Seq input with the next monotonic value.
type adminFeesData struct {
	CoordinatorID string
	Current       *adminFeeCurrent
	NextSeq       uint64
	History       []adminFeeHistoryRow
}

// handleAdminFeesPage renders the fee governance page (gov_fees.html): the
// current signed declaration, the full history, and the compose/sign form (which
// POSTs to the existing /admin/fees publish route). The current declaration is
// read via CurrentFeeDeclaration (the signable artifact, for the signature
// display); the history and next-seq come from the read model. Pure read.
func (g *GovernanceServer) handleAdminFeesPage(w http.ResponseWriter, r *http.Request) {
	if g.consoleRepo == nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	data := adminFeesData{CoordinatorID: g.coordinatorID}

	// Current signed declaration (for the signature display). Empty-state is not
	// an error: Current stays nil and the template shows the "no declaration" card.
	decl, err := g.repo.CurrentFeeDeclaration(r.Context(), g.coordinatorID)
	switch {
	case err == nil:
		data.Current = &adminFeeCurrent{
			ContributorShareBps: decl.Terms.ContributorShareBps,
			PlatformFeeBps:      decl.Terms.PlatformFeeBps,
			Seq:                 decl.Seq,
			Balanced:            decl.Terms.Balanced(),
			EffectiveAt:         decl.EffectiveAt.UTC().Format(time.RFC3339),
			Signature:           base64.StdEncoding.EncodeToString(decl.Signature),
		}
	case errors.Is(err, operator.ErrNoFeeDeclaration):
		// Current stays nil; empty-state card.
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	nextSeq, err := g.consoleRepo.NextSeq(r.Context(), g.coordinatorID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data.NextSeq = nextSeq

	history, err := g.consoleRepo.FeeHistory(r.Context(), g.coordinatorID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	for _, h := range history {
		data.History = append(data.History, adminFeeHistoryRow{
			Seq:                 h.Seq,
			ContributorShareBps: h.ContributorShareBps,
			PlatformFeeBps:      h.PlatformFeeBps,
			Balanced:            h.ContributorShareBps+h.PlatformFeeBps == 10000,
			EffectiveAt:         h.EffectiveAt.UTC().Format(time.RFC3339),
		})
	}
	g.renderAdmin(w, "gov_fees.html", data)
}

// -----------------------------------------------------------------------------
// GET /admin/messaging — compose to operators/members (gov_messaging.html).
// -----------------------------------------------------------------------------

// handleAdminMessagingPage renders the messaging composer (gov_messaging.html).
// The page is static — its form POSTs to the existing /admin/messages route,
// which resolves and deduplicates recipients server-side. No data is read here.
func (g *GovernanceServer) handleAdminMessagingPage(w http.ResponseWriter, r *http.Request) {
	g.renderAdmin(w, "gov_messaging.html", nil)
}

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

	"github.com/NTARI-RAND/sohocloud-protocol/fees"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// This file adds the PUBLIC operator-console GET pages (Stage 4, step 2) to the
// same OnboardingServer that owns the Stage-2 POST/JSON routes. It renders the
// Stage-3 operator_*.html templates against the step-1 read models
// (internal/operator/readmodel.go) using the SAME per-request
// template.ParseFiles(layout, page[, banner]) pattern as internal/portal so the
// two surfaces stay stylistically identical and the "content"-redefinition
// collision is avoided (each page file defines {{define "content"}}).
//
// These handlers are READ-ONLY. The privileged lifecycle (activate/revoke/fees
// publish) stays on the LOCAL-ONLY :8090 governance surface — nothing here can
// mutate state or reach the coordinator signing key (governance-separation
// invariant, CLAUDE.md / design §1). All operator-supplied fields (name, id,
// email) render through html/template, which auto-escapes.

// consoleRepo is the READ-MODEL subset of *operator.Repository the GET console
// pages need. It is deliberately separate from operatorRepo (the write/JSON
// interface) so the read surface cannot call a mutation and a test can stub the
// read side independently. Every method is a pure read (see readmodel.go).
type consoleRepo interface {
	GetOperator(ctx context.Context, operatorID string) (operator.OperatorDetail, error)
	CurrentFeeDeclaration(ctx context.Context, coordinatorID string) (fees.FeeDeclaration, error)
}

// compile-time assertion that *operator.Repository satisfies consoleRepo.
var _ consoleRepo = (*operator.Repository)(nil)

// ConfigureConsole enables the public operator-console GET pages on this server.
// It collects the .html template paths under templatesDir (recursively, like the
// portal) for the per-request ParseFiles render, and records the coordinatorID
// bound into the /fees page. It is separate from the constructor so the existing
// POST-only construction (and its tests) is unaffected; a server without this
// call registers the GET routes but renders a 500 "template not found" — the same
// failure mode as the portal with a missing template.
//
// Returns an error only if templatesDir cannot be walked. An empty templatesDir
// (no .html files) is allowed — the GET routes then 500 until templates exist.
func (s *OnboardingServer) ConfigureConsole(readRepo consoleRepo, templatesDir, coordinatorID string) error {
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
	s.consoleRepo = readRepo
	s.templatePaths = paths
	s.coordinatorID = coordinatorID
	return nil
}

// renderTemplate builds a fresh template set from layout.html, the requested page
// file, and (if present) transitional_banner.html, then executes the named page
// template. This mirrors internal/portal.PortalServer.renderTemplate verbatim:
// parsing per-request avoids the shared-set redefinition problem (every page
// defines {{define "content"}}), and the banner partial is parsed into every set
// so member pages can invoke it while operator pages simply do not.
func (s *OnboardingServer) renderTemplate(w http.ResponseWriter, page string, data any) {
	var layoutPath, pagePath, bannerPath string
	for _, p := range s.templatePaths {
		switch filepath.Base(p) {
		case "layout.html":
			layoutPath = p
		case "transitional_banner.html":
			bannerPath = p
		case page:
			pagePath = p
		}
	}
	if layoutPath == "" || pagePath == "" {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	files := []string{layoutPath, pagePath}
	if bannerPath != "" {
		files = append(files, bannerPath)
	}
	tmpl, err := template.ParseFiles(files...)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// -----------------------------------------------------------------------------
// GET / — operator console landing (reshaped coordinator identity).
// -----------------------------------------------------------------------------

// handleLanding renders the operator console landing page (operator_landing.html).
// The page is static coordinator identity; the only data the layout reads is
// IsAuthenticated (always false on the public operator surface — there is no
// member session here). The muted member-portal link keeps the transitional
// member entrypoint reachable without re-homing the member hero to /.
func (s *OnboardingServer) handleLanding(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "operator_landing.html", struct{ IsAuthenticated bool }{})
}

// -----------------------------------------------------------------------------
// GET /operators/apply — Step 1 application form.
// -----------------------------------------------------------------------------

// handleApplyPage renders the apply form (operator_apply.html). The form POSTs
// JSON to the existing handleApply route; this handler only serves the page.
func (s *OnboardingServer) handleApplyPage(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "operator_apply.html", struct{ IsAuthenticated bool }{})
}

// -----------------------------------------------------------------------------
// GET /fees — current signed coordinator FeeDeclaration.
// -----------------------------------------------------------------------------

// feesPageData is the template data for operator_fees.html. Signature is the
// base64 of the signed declaration's raw signature bytes, rendered inside a
// <code> so a client can independently verify against the published coordinator
// key. FeeSet is false (and the "no declaration" card shows) when none has been
// published yet.
type feesPageData struct {
	IsAuthenticated     bool
	CoordinatorID       string
	FeeSet              bool
	ContributorShareBps int
	PlatformFeeBps      int
	Balanced            bool
	EffectiveAt         string
	Seq                 uint64
	Signature           string
}

// handleFeesPage renders the public fee-terms page. It serves the CURRENT signed
// declaration read-only via the repository (the signing key lives only on :8090;
// this handler never touches it). No declaration yet -> FeeSet=false and the page
// shows the empty-state card rather than an error.
func (s *OnboardingServer) handleFeesPage(w http.ResponseWriter, r *http.Request) {
	data := feesPageData{CoordinatorID: s.coordinatorID}
	if s.consoleRepo != nil {
		decl, err := s.consoleRepo.CurrentFeeDeclaration(r.Context(), s.coordinatorID)
		switch {
		case err == nil:
			data.FeeSet = true
			data.CoordinatorID = decl.CoordinatorID
			data.ContributorShareBps = decl.Terms.ContributorShareBps
			data.PlatformFeeBps = decl.Terms.PlatformFeeBps
			data.Balanced = decl.Terms.Balanced()
			data.EffectiveAt = decl.EffectiveAt.UTC().Format(time.RFC3339)
			data.Seq = decl.Seq
			data.Signature = base64.StdEncoding.EncodeToString(decl.Signature)
		case errors.Is(err, operator.ErrNoFeeDeclaration):
			// FeeSet stays false; page renders the empty-state card.
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	s.renderTemplate(w, "operator_fees.html", data)
}

// -----------------------------------------------------------------------------
// GET /operators/{id}/keys|verify|conformance — per-step funnel pages.
// -----------------------------------------------------------------------------

// stepPageData is the minimal data the keys/verify/conformance step pages read:
// just the operator id (echoed into the page header and the page's fetch() calls
// to the POST routes). Rendered via html/template, so the id is auto-escaped.
type stepPageData struct {
	IsAuthenticated bool
	OperatorID      string
}

// handleKeysPage renders Step 2 (operator_keys.html) for the given operator.
func (s *OnboardingServer) handleKeysPage(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "operator_keys.html", stepPageData{OperatorID: r.PathValue("id")})
}

// handleVerifyPage renders Step 3 (operator_verify.html) for the given operator.
func (s *OnboardingServer) handleVerifyPage(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "operator_verify.html", stepPageData{OperatorID: r.PathValue("id")})
}

// handleConformancePage renders Step 4 (operator_conformance.html).
func (s *OnboardingServer) handleConformancePage(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "operator_conformance.html", stepPageData{OperatorID: r.PathValue("id")})
}

// -----------------------------------------------------------------------------
// GET /operators/{id}/dashboard — status-aware operator dashboard.
// -----------------------------------------------------------------------------

// dashboardKeyRow is one signing-keys row for operator_dashboard.html, adapting
// operator.KeyView field names to the template's expected names.
type dashboardKeyRow struct {
	Index         int
	PubKeyTrunc   string
	State         string
	UsageCount    int
	Threshold     int
	NearThreshold bool
}

// dashboardPageData is the template data for operator_dashboard.html. It merges
// the operator detail read model, the near-threshold key rows, the lifecycle
// timestamps as pre-formatted display strings, and the current fee terms preview.
// The template branches on OnboardingState: a not-yet-active operator sees the
// pre-activation recap; an active operator sees the full dashboard.
type dashboardPageData struct {
	IsAuthenticated bool

	Name              string
	OperatorID        string
	OnboardingState   string
	Status            string
	EmailVerified     bool
	ConformancePassed bool
	KeyCount          int
	ActiveKeyCount    int
	Transmissions24h  int

	Keys []dashboardKeyRow

	CreatedAt           string
	VerifiedAt          string
	ConformancePassedAt string
	ActivatedAt         string

	FeeSet              bool
	ContributorShareBps int
	PlatformFeeBps      int
	FeeEffectiveAt      string
	FeeSeq              uint64
}

// handleDashboardPage renders the status-aware operator dashboard
// (operator_dashboard.html). It reads the full operator detail plus the current
// fee terms (both pure reads). A missing operator id is a 404. The page itself
// decides what to show based on OnboardingState/Status; this handler only maps
// the read models into the template's field shape.
func (s *OnboardingServer) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	if s.consoleRepo == nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	operatorID := r.PathValue("id")
	detail, err := s.consoleRepo.GetOperator(r.Context(), operatorID)
	if errors.Is(err, operator.ErrOperatorNotFound) {
		http.Error(w, "operator not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := dashboardPageData{
		Name:              detail.Name,
		OperatorID:        detail.ID,
		OnboardingState:   detail.OnboardingState,
		Status:            detail.Status,
		EmailVerified:     detail.EmailVerified,
		ConformancePassed: detail.ConformancePassedAt != nil,
		KeyCount:          len(detail.Keys),
		ActiveKeyCount:    detail.ActiveKeyCount,
		Transmissions24h:  detail.TransmissionsLast24h,
		CreatedAt:         fmtTime(&detail.Lifecycle.CreatedAt),
		VerifiedAt:        boolStamp(detail.EmailVerified, "verified"),
		ActivatedAt:       boolStamp(detail.Lifecycle.Activated, "active"),
	}
	if detail.ConformancePassedAt != nil {
		data.ConformancePassedAt = fmtTime(detail.ConformancePassedAt)
	} else {
		data.ConformancePassedAt = "—"
	}

	for _, k := range detail.Keys {
		data.Keys = append(data.Keys, dashboardKeyRow{
			Index:         k.KeyIndex,
			PubKeyTrunc:   k.PublicKeyB64Trunc,
			State:         k.State,
			UsageCount:    k.UsageCount,
			Threshold:     k.ExpirationThreshold,
			NearThreshold: k.NearThreshold,
		})
	}

	// Current fee terms preview (empty state renders "No fee declaration").
	if decl, err := s.consoleRepo.CurrentFeeDeclaration(r.Context(), s.coordinatorID); err == nil {
		data.FeeSet = true
		data.ContributorShareBps = decl.Terms.ContributorShareBps
		data.PlatformFeeBps = decl.Terms.PlatformFeeBps
		data.FeeEffectiveAt = decl.EffectiveAt.UTC().Format(time.RFC3339)
		data.FeeSeq = decl.Seq
	} else if !errors.Is(err, operator.ErrNoFeeDeclaration) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "operator_dashboard.html", data)
}

// fmtTime renders a timestamp as a UTC RFC3339 string, or "—" when nil.
func fmtTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

// boolStamp renders a lifecycle milestone that the registry stores only as a
// boolean (no instant): the given label when reached, "—" otherwise.
func boolStamp(reached bool, label string) string {
	if reached {
		return label
	}
	return "—"
}

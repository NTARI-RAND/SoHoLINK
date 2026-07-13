package protocoladapter

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/NTARI-RAND/sohocloud-protocol/transport/httpjson"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
)

// NewHandler mounts the reference httpjson transport for the adapter behind
// SoHoLINK's SPIFFE middleware, per SPEC §7: "SPIFFE binding is applied by
// middleware in front of the handler; the reference handler wires routes and
// JSON only and is not the authenticator." The middleware chain is what gives
// 401/403 their correct statuses — the reference fail() can only 500.
//
// GET /v0/fees is registered PLAIN (SPEC §6: anyone → coord) as an exact
// pattern, which shadows the reference handler's own /v0/fees route so
// operator.ErrNoFeeDeclaration maps to an honest 404 ("nothing published
// yet") instead of a 500.
//
// degraded (SPIRE Workload API unreachable at startup, nil idSource) serves
// 503 on the protected subtree while fees stay reachable — mirroring the
// bespoke server's degraded posture.
func NewHandler(a *Adapter, idSource *identity.Source, degraded bool) http.Handler {
	top := http.NewServeMux()

	top.HandleFunc("GET /v0/fees", func(w http.ResponseWriter, r *http.Request) {
		decl, err := a.Fees(r.Context())
		if err != nil {
			if errors.Is(err, operator.ErrNoFeeDeclaration) {
				http.Error(w, "no fee declaration published", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(decl)
	})

	if degraded {
		top.Handle("/v0/", identity.UnavailableHandler())
		return top
	}
	// idSource is non-nil in production (non-degraded is only reached with a
	// live SPIRE source). Guard anyway: a nil source yields a nil bundle rather
	// than a panic, and RequireSPIFFE then fails every presented cert closed
	// while still 401-ing the no-cert case before it touches the bundle.
	var bundle x509bundle.Source
	if idSource != nil {
		bundle = idSource.BundleSource()
	}
	top.Handle("/v0/", identity.RequireSPIFFE(bundle, bindNodeID(httpjson.Handler(a))))
	return top
}

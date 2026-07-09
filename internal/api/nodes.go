package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/metrics"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

type nodePrinterInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type registerNodeRequest struct {
	NodeID      string `json:"node_id"`
	ProviderID  string `json:"provider_id"`
	NodeClass   string `json:"node_class"`
	CountryCode string `json:"country_code"`
	Region      string `json:"region"`
	HardwareProfile struct {
		CPUCores      int               `json:"cpu_cores"`
		RAMMB         int               `json:"ram_mb"`
		GPUPresent    bool              `json:"gpu_present"`
		StorageGB     int               `json:"storage_gb"`
		BandwidthMbps int               `json:"bandwidth_mbps"`
		Printers      []nodePrinterInfo `json:"printers"`
	} `json:"hardware_profile"`
}

type claimNodeRequest struct {
	Token       string `json:"token"`
	Hostname    string `json:"hostname"`
	CountryCode string `json:"country_code"`
	Region      string `json:"region"`
	HardwareProfile struct {
		CPUCores      int  `json:"cpu_cores"`
		RAMMB         int  `json:"ram_mb"`
		GPUPresent    bool `json:"gpu_present"`
		StorageGB     int  `json:"storage_gb"`
		BandwidthMbps int  `json:"bandwidth_mbps"`
	} `json:"hardware_profile"`
}

type heartbeatRequest struct {
	NodeID        string `json:"node_id"`
	OptOutVersion int    `json:"opt_out_version"`
	PrinterHash   string `json:"printer_hash"`

	// Transitional ADVISORY load fields (B2): consumed only by the
	// scheduler's soft idle-first scoring, never a hard gate. Absent fields
	// decode to zero values — backward compatible with old agents. The
	// signed protocol Heartbeat carries neither (no float in canon).
	OwnerActive bool    `json:"owner_active"`
	CPUPct      float64 `json:"cpu_pct"`
}

type heartbeatOptOut struct {
	Version         int             `json:"version"`
	ComputeEnabled  bool            `json:"compute_enabled"`
	StorageEnabled  bool            `json:"storage_enabled"`
	PrintingEnabled bool            `json:"printing_enabled"`
	EnabledPrinters map[string]bool `json:"enabled_printers"`
}

type heartbeatResponse struct {
	OK                   bool             `json:"ok"`
	OptOut               *heartbeatOptOut `json:"opt_out,omitempty"`
	RequestPrinterReport bool             `json:"request_printer_report,omitempty"`
}

type telemetryRequest struct {
	NodeID        string    `json:"node_id"`
	CPUPct        float64   `json:"cpu_pct"`
	RAMPct        float64   `json:"ram_pct"`
	BandwidthMbps int       `json:"bandwidth_mbps"`
	Timestamp     time.Time `json:"timestamp"`
}

type jobEntry struct {
	JobID     string `json:"job_id"`
	JobToken  string `json:"job_token"`
	Image     string `json:"container_image"`
	PrinterID string `json:"printer_id,omitempty"`
}

func registerNodeRoutes(mux *http.ServeMux, db *store.DB, registry *orchestrator.NodeRegistry) {
	mux.HandleFunc("POST /nodes/register", handleRegisterNode(db, registry))
	mux.HandleFunc("POST /nodes/heartbeat", handleHeartbeat(db, registry))
	mux.HandleFunc("POST /nodes/printers", handleReportPrinters(db))
	mux.HandleFunc("POST /nodes/pubkey", handleRegisterNodePubkey(db))
	mux.HandleFunc("GET /nodes/jobs", handleGetJobs(db))
	mux.HandleFunc("POST /jobs/{id}/started", handleStartedJob(db))
	mux.HandleFunc("POST /jobs/{id}/telemetry", handleTelemetry(db))
	mux.HandleFunc("POST /jobs/{id}/complete", handleCompleteJob(db, registry))
}

func handleRegisterNode(db *store.DB, registry *orchestrator.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Programmatic registration requires a pre-shared secret so arbitrary
		// callers cannot register nodes on behalf of any participant.
		// Set CONTROL_PLANE_REGISTER_SECRET to enable this endpoint; leave it
		// unset to disable it entirely (installer flow uses /nodes/claim instead).
		secret := os.Getenv("CONTROL_PLANE_REGISTER_SECRET")
		if secret == "" {
			writeError(w, http.StatusForbidden, "programmatic registration is disabled")
			return
		}
		if r.Header.Get("X-Register-Secret") != secret {
			writeError(w, http.StatusUnauthorized, "invalid register secret")
			return
		}

		var req registerNodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.NodeID == "" || req.ProviderID == "" {
			writeError(w, http.StatusBadRequest, "node_id and provider_id are required")
			return
		}

		registry.Register(orchestrator.NodeEntry{
			NodeID:        req.NodeID,
			ParticipantID: req.ProviderID,
			NodeClass:     req.NodeClass,
			CountryCode:   req.CountryCode,
			Region:        req.Region,
			Status:        "online",
			LastHeartbeat: time.Now(),
			HardwareProfile: orchestrator.HardwareProfile{
				CPUCores:      req.HardwareProfile.CPUCores,
				RAMMB:         req.HardwareProfile.RAMMB,
				GPUPresent:    req.HardwareProfile.GPUPresent,
				StorageGB:     req.HardwareProfile.StorageGB,
				BandwidthMbps: req.HardwareProfile.BandwidthMbps,
			},
		})

		hwJSON, err := json.Marshal(req.HardwareProfile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode hardware profile")
			return
		}

		var region *string
		if req.Region != "" {
			region = &req.Region
		}

		// hostname is NOT NULL in the schema; use node_id as the stable identifier
		// until the agent reports its own hostname in a later phase.
		_, err = db.Pool.Exec(r.Context(), `
			INSERT INTO nodes (id, participant_id, node_class, hostname, country_code, region, status, hardware_profile)
			VALUES ($1, $2, $3::node_class, $4, $5, $6, 'online'::node_status, $7)
			ON CONFLICT (id) DO UPDATE SET
				participant_id   = EXCLUDED.participant_id,
				node_class       = EXCLUDED.node_class,
				country_code     = EXCLUDED.country_code,
				region           = EXCLUDED.region,
				status           = 'online'::node_status,
				hardware_profile = EXCLUDED.hardware_profile,
				updated_at       = NOW()`,
			req.NodeID, req.ProviderID, req.NodeClass, req.NodeID,
			req.CountryCode, region, string(hwJSON),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Upsert printer rows. ON CONFLICT preserves the enabled flag set by the portal.
		for _, p := range req.HardwareProfile.Printers {
			_, err = db.Pool.Exec(r.Context(), `
				INSERT INTO node_printers (node_id, printer_id, printer_name)
				VALUES ($1, $2, $3)
				ON CONFLICT (node_id, printer_id) DO UPDATE SET
					printer_name = EXCLUDED.printer_name,
					detected_at  = NOW()`,
				req.NodeID, p.ID, p.Name,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"registered": true}) //nolint:errcheck
	}
}

// handleClaimNode is the installer-facing registration endpoint.
// The agent presents a single-use token generated by the participant on their
// dashboard. On success a new node record is created and the agent receives its
// node_id and a fresh HMAC secret for signing telemetry payloads.
func handleClaimNode(db *store.DB, registry *orchestrator.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req claimNodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Token == "" {
			writeError(w, http.StatusBadRequest, "token is required")
			return
		}

		// Validate token: must exist, unexpired, unused.
		var participantID string
		err := db.Pool.QueryRow(r.Context(), `
			SELECT participant_id FROM node_registration_tokens
			WHERE token = $1
			  AND expires_at > NOW()
			  AND used_at IS NULL`,
			req.Token,
		).Scan(&participantID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		hostname := req.Hostname
		if hostname == "" {
			hostname = "unknown"
		}

		hwJSON, err := json.Marshal(req.HardwareProfile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode hardware profile")
			return
		}

		var region *string
		if req.Region != "" {
			region = &req.Region
		}

		// Create the node record; DB generates the UUID.
		var nodeID string
		err = db.Pool.QueryRow(r.Context(), `
			INSERT INTO nodes (id, participant_id, node_class, hostname, country_code, region, status, hardware_profile)
			VALUES (gen_random_uuid(), $1, 'C'::node_class, $2, $3, $4, 'online'::node_status, $5)
			RETURNING id`,
			participantID, hostname, req.CountryCode, region, string(hwJSON),
		).Scan(&nodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Read SPIRE join token issued at portal token-generation time.
		// Needed for the workload entry registration call below, and
		// returned to the agent in the response so it can attest to
		// spire-agent. Empty if portal SPIRE token-gen failed upstream.
		var spireJoinToken string
		_ = db.Pool.QueryRow(r.Context(),
			`SELECT COALESCE(spire_join_token, '') FROM node_registration_tokens WHERE token = $1`,
			req.Token,
		).Scan(&spireJoinToken)

		// Register a SPIRE workload entry so the contributor's soholink-agent
		// can obtain a workload SVID for spiffe://soholink.org/node/<nodeID>
		// from its local spire-agent. Without this the agent has no identity
		// to present at mTLS time. If registration fails, roll back the node
		// row — the registration token stays unused so the participant can
		// retry with the same token.
		selector := os.Getenv("SPIRE_NODE_SELECTOR")
		if selector == "" {
			selector = `windows:user_name:NT AUTHORITY\SYSTEM`
		}
		if spireErr := registerNodeSpireEntry(r.Context(), nodeID, spireJoinToken, selector); spireErr != nil {
			slog.Error("spire workload entry registration failed; rolling back node row",
				"node_id", nodeID,
				"error", spireErr)
			if _, rollbackErr := db.Pool.Exec(r.Context(), `DELETE FROM nodes WHERE id = $1`, nodeID); rollbackErr != nil {
				slog.Error("compensating delete of node row failed; manual cleanup required",
					"node_id", nodeID,
					"rollback_error", rollbackErr)
			}
			writeError(w, http.StatusInternalServerError, "spire workload entry registration failed")
			return
		}

		// Mark token consumed.
		_, err = db.Pool.Exec(r.Context(), `
			UPDATE node_registration_tokens
			SET used_at = NOW(), node_id = $1
			WHERE token = $2`,
			nodeID, req.Token,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Register in in-memory registry so the node appears immediately.
		registry.Register(orchestrator.NodeEntry{
			NodeID:      nodeID,
			ParticipantID: participantID,
			NodeClass:   "C",
			CountryCode: req.CountryCode,
			Region:      req.Region,
			Status:      "online",
			LastHeartbeat: time.Now(),
			HardwareProfile: orchestrator.HardwareProfile{
				CPUCores:      req.HardwareProfile.CPUCores,
				RAMMB:         req.HardwareProfile.RAMMB,
				GPUPresent:    req.HardwareProfile.GPUPresent,
				StorageGB:     req.HardwareProfile.StorageGB,
				BandwidthMbps: req.HardwareProfile.BandwidthMbps,
			},
		})

		// Generate a per-node HMAC secret for telemetry signing. Returned once;
		// the agent persists it to agent.conf. Not stored server-side until the
		// telemetry verification layer is added.
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		resp := struct {
			NodeID         string `json:"node_id"`
			TokenSecret    string `json:"token_secret"`
			SpireJoinToken string `json:"spire_join_token,omitempty"`
		}{
			NodeID:         nodeID,
			TokenSecret:    hex.EncodeToString(secretBytes),
			SpireJoinToken: spireJoinToken,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// registerNodeSpireEntry calls `spire-server entry create` to register a
// workload entry that lets the contributor's soholink-agent process obtain
// a workload SVID for spiffe://soholink.org/node/<nodeID> from its local
// spire-agent. Without this entry the agent has no identity to present at
// mTLS time and cannot reach the orchestrator.
//
// If spireJoinToken is empty the SPIRE chain was already broken upstream
// (portal could not reach spire-server during token generation) — we
// preserve graceful degradation by skipping with a warning rather than
// failing the claim.
func registerNodeSpireEntry(ctx context.Context, nodeID, spireJoinToken, selector string) error {
	if spireJoinToken == "" {
		slog.Warn("spire workload entry skipped — spire_join_token empty",
			"node_id", nodeID,
			"reason", "portal SPIRE token generation likely failed upstream")
		return nil
	}
	parentID := "spiffe://soholink.org/spire/agent/join_token/" + spireJoinToken
	spiffeID := "spiffe://soholink.org/node/" + nodeID
	out, err := exec.CommandContext(ctx,
		"/opt/spire/bin/spire-server", "entry", "create",
		"-socketPath", "/run/spire-server/private/api.sock",
		"-parentID", parentID,
		"-spiffeID", spiffeID,
		"-selector", selector,
		"-ttl", "3600",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("spire-server entry create failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func handleHeartbeat(db *store.DB, registry *orchestrator.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req heartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.NodeID == "" {
			writeError(w, http.StatusBadRequest, "node_id is required")
			return
		}

		// See handleCompleteJob for the binding rationale.
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+req.NodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match node")
			return
		}

		if err := registry.Heartbeat(req.NodeID); err != nil {
			writeError(w, http.StatusNotFound, "node not found in registry")
			return
		}

		if err := store.RecordNodeHeartbeat(r.Context(), db, req.NodeID); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		metrics.HeartbeatsTotal.WithLabelValues(req.NodeID).Inc()

		// Read current opt-out state to determine whether to push an update,
		// and to refresh the in-memory registry's opt-out fields for FindMatch.
		var dbVersion int
		var computeEnabled, storageEnabled, printingEnabled bool
		var hasEnabledPrinter bool
		err := db.Pool.QueryRow(r.Context(), `
			SELECT opt_out_version, opt_out_compute, opt_out_storage, opt_out_printing,
			       EXISTS(SELECT 1 FROM node_printers WHERE node_id = $1 AND enabled = TRUE)
			FROM nodes WHERE id = $1`, req.NodeID,
		).Scan(&dbVersion, &computeEnabled, &storageEnabled, &printingEnabled, &hasEnabledPrinter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Refresh the in-memory registry's opt-out fields so FindMatch can filter
		// without DB access. The agent-side gate remains the canonical enforcement
		// layer; this is defense-in-depth at dispatch time.
		if err := registry.UpdateOptOut(req.NodeID, orchestrator.NodeOptOutState{
			OptOutCompute:     computeEnabled,
			OptOutStorage:     storageEnabled,
			OptOutPrinting:    printingEnabled,
			HasEnabledPrinter: hasEnabledPrinter,
		}); err != nil {
			// Race: node was evicted between Heartbeat() above and this call.
			// Heartbeat itself succeeded; log and continue.
			slog.Warn("registry update opt-out failed", "node_id", req.NodeID, "err", err)
		}

		// Refresh the advisory load sample (B2) for the scheduler's soft
		// idle-first scoring. Same warn-and-continue posture as UpdateOptOut:
		// a lost sample must never fail a heartbeat.
		if err := registry.UpdateLoad(req.NodeID, orchestrator.NodeLoadState{
			OwnerActive: req.OwnerActive,
			CPUUtilPct:  req.CPUPct,
			SampledAt:   time.Now(),
		}); err != nil {
			slog.Warn("registry update load failed", "node_id", req.NodeID, "err", err)
		}

		resp := heartbeatResponse{OK: true}

		// Push opt-out payload when the agent's version is stale.
		if req.OptOutVersion < dbVersion {
			rows, err := db.Pool.Query(r.Context(),
				`SELECT printer_id, enabled FROM node_printers WHERE node_id = $1`, req.NodeID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
			enabledPrinters := map[string]bool{}
			for rows.Next() {
				var pid string
				var enabled bool
				if err := rows.Scan(&pid, &enabled); err != nil {
					rows.Close()
					writeError(w, http.StatusInternalServerError, "database error")
					return
				}
				if enabled {
					enabledPrinters[pid] = true
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}

			resp.OptOut = &heartbeatOptOut{
				Version:         dbVersion,
				ComputeEnabled:  computeEnabled,
				StorageEnabled:  storageEnabled,
				PrintingEnabled: printingEnabled,
				EnabledPrinters: enabledPrinters,
			}
		}

		// Compare printer hash to detect hot-plug events.
		dbHash, err := serverPrinterHash(r.Context(), db, req.NodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if req.PrinterHash != dbHash {
			resp.RequestPrinterReport = true
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// serverPrinterHash computes the SHA-256 of sorted printer IDs for a node,
// matching the agent-side PrinterHash algorithm. Returns "" when the node has
// no printers.
func serverPrinterHash(ctx context.Context, db *store.DB, nodeID string) (string, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT printer_id FROM node_printers WHERE node_id = $1 ORDER BY printer_id`, nodeID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", nil
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func handleReportPrinters(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			NodeID   string            `json:"node_id"`
			Printers []nodePrinterInfo `json:"printers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.NodeID == "" {
			writeError(w, http.StatusBadRequest, "node_id is required")
			return
		}
		// See handleCompleteJob for the binding rationale.
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+req.NodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match node")
			return
		}

		for _, p := range req.Printers {
			_, err := db.Pool.Exec(r.Context(), `
				INSERT INTO node_printers (node_id, printer_id, printer_name)
				VALUES ($1, $2, $3)
				ON CONFLICT (node_id, printer_id) DO UPDATE SET
					printer_name = EXCLUDED.printer_name,
					detected_at  = NOW()`,
				req.NodeID, p.ID, p.Name,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true}) //nolint:errcheck
	}
}

func handleGetJobs(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		if nodeID == "" {
			writeError(w, http.StatusBadRequest, "node_id query parameter is required")
			return
		}

		// See handleCompleteJob for the binding rationale.
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+nodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match node")
			return
		}

		dispatched, err := store.PollScheduledJobs(r.Context(), db, nodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		jobs := make([]jobEntry, 0, len(dispatched))
		for _, d := range dispatched {
			jobs = append(jobs, jobEntry{
				JobID:     d.JobID,
				JobToken:  d.JobToken,
				Image:     d.Image,
				PrinterID: d.PrinterID,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobs) //nolint:errcheck
	}
}

// completeJobRequest is the JSON body the agent POSTs to /jobs/{id}/complete.
// ExitCode is a pointer so the handler distinguishes "not sent" (old agent —
// persisted as NULL) from "sent zero" (success — persisted as 0). C4 uses this
// distinction to decide whether to fire metering.
type completeJobRequest struct {
	ExitCode       *int   `json:"exit_code,omitempty"`
	FailureCause   string `json:"failure_cause,omitempty"`
	TmpfsExhausted bool   `json:"tmpfs_exhausted,omitempty"`
}

func handleCompleteJob(db *store.DB, registry *orchestrator.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "job ID required")
			return
		}

		var nodeID string
		err := db.Pool.QueryRow(r.Context(), `
			SELECT j.node_id::text
			FROM jobs j
			WHERE j.id = $1`,
			jobID,
		).Scan(&nodeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		// Bind the peer's SPIFFE identity to the node that owns this job.
		// Node SPIFFE IDs follow a deterministic format
		// (spiffe://soholink.org/node/<nodeID>) enforced at registration in
		// registerNodeSpireEntry, so we compare the path component without
		// needing a per-node lookup. 401 is reserved for missing identity
		// (router misconfiguration or degraded mode); 403 for mismatch.
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+nodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match job owner")
			return
		}

		var req completeJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		newStatus, err := store.CompleteJob(r.Context(), db, jobID, req.ExitCode, req.FailureCause, req.TmpfsExhausted)
		if err != nil {
			if errors.Is(err, store.ErrJobNotRunning) {
				writeError(w, http.StatusConflict, "job is not in running state")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Advisory in-flight accounting (B4): all three statuses CompleteJob
		// can produce are terminal FOR PLACEMENT — completed, failed, and
		// awaiting_pickup (container work is done; only physical handoff
		// remains) — so the node's slot is released.
		registry.AddInFlight(nodeID, -1)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": newStatus}) //nolint:errcheck
	}
}

// nodePubkeyRequest is the JSON body for POST /nodes/pubkey (A1): out-of-band
// enrollment of a node's sohocloud-protocol Ed25519 verification key.
type nodePubkeyRequest struct {
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"` // standard base64 of the raw 32-byte ed25519 public key
	Algo      string `json:"algo"`       // optional; only "ed25519" accepted in v0
}

// handleRegisterNodePubkey enrolls a node's protocol verification key into
// node_protocol_keys. SPIFFE-bound with the canonical TODO-35 guard.
// FIRST-WRITE-WINS: re-enrolling the identical key is an idempotent 200;
// presenting a different key returns 409 — rotation is an operator action
// (direct DB update with sign-off), never a self-service overwrite, so a
// compromised node SVID cannot silently swap the verification key.
func handleRegisterNodePubkey(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req nodePubkeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.NodeID == "" {
			writeError(w, http.StatusBadRequest, "node_id is required")
			return
		}
		// Canonical SPIFFE binding guard (TODO 34/35 form).
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+req.NodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match node")
			return
		}

		algo := req.Algo
		if algo == "" {
			algo = "ed25519"
		}
		if algo != "ed25519" {
			writeError(w, http.StatusBadRequest, "unsupported algo (v0 permits exactly ed25519)")
			return
		}
		key, err := base64.StdEncoding.DecodeString(req.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			writeError(w, http.StatusBadRequest, "public_key must be standard base64 of a 32-byte ed25519 public key")
			return
		}

		tag, err := db.Pool.Exec(r.Context(),
			`INSERT INTO node_protocol_keys (node_id, public_key, algo)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (node_id) DO NOTHING`,
			req.NodeID, key, algo,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
				writeError(w, http.StatusNotFound, "node not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		if tag.RowsAffected() == 0 {
			// A key already exists: idempotent success only when byte-identical.
			var existingKey []byte
			var existingAlgo string
			if err := db.Pool.QueryRow(r.Context(),
				`SELECT public_key, algo FROM node_protocol_keys WHERE node_id = $1`,
				req.NodeID,
			).Scan(&existingKey, &existingAlgo); err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
			if !bytes.Equal(existingKey, key) || existingAlgo != algo {
				writeError(w, http.StatusConflict, "a different protocol key is already enrolled for this node (rotation is an operator action)")
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"enrolled": true}) //nolint:errcheck
	}
}

func handleStartedJob(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "job ID required")
			return
		}

		var nodeID string
		err := db.Pool.QueryRow(r.Context(), `
			SELECT j.node_id::text
			FROM jobs j
			WHERE j.id = $1`,
			jobID,
		).Scan(&nodeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		// See handleCompleteJob for the binding rationale.
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+nodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match job owner")
			return
		}

		tag, err := db.Pool.Exec(r.Context(),
			`UPDATE jobs SET started_at = NOW(), status = 'running'::job_status, updated_at = NOW()
			 WHERE id = $1 AND status = 'dispatched'::job_status`,
			jobID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusConflict, "job is not in dispatched state")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"started": true}) //nolint:errcheck
	}
}

func handleTelemetry(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "job ID required")
			return
		}

		var nodeID string
		err := db.Pool.QueryRow(r.Context(), `
			SELECT j.node_id::text
			FROM jobs j
			WHERE j.id = $1`,
			jobID,
		).Scan(&nodeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		// See handleCompleteJob for the binding rationale.
		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}
		if spiffeID.Path() != "/node/"+nodeID {
			writeError(w, http.StatusForbidden, "SPIFFE identity does not match job owner")
			return
		}

		var req telemetryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Metering table is added in Phase 2 Step 4 — log for now.
		log.Printf("telemetry job=%s node=%s cpu=%.1f%% ram=%.1f%% bw=%dMbps ts=%s spiffe=%s",
			jobID, req.NodeID, req.CPUPct, req.RAMPct, req.BandwidthMbps,
			req.Timestamp.Format(time.RFC3339), spiffeID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true}) //nolint:errcheck
	}
}

// ── APIServer method wrappers (used by tests and future handler composition) ─

func (s *APIServer) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
	handleRegisterNode(s.db, s.registry)(w, r)
}

func (s *APIServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	handleHeartbeat(s.db, s.registry)(w, r)
}

func (s *APIServer) handleCompleteJob(w http.ResponseWriter, r *http.Request) {
	handleCompleteJob(s.db, s.registry)(w, r)
}

func (s *APIServer) handleRegisterNodePubkey(w http.ResponseWriter, r *http.Request) {
	handleRegisterNodePubkey(s.db)(w, r)
}

func (s *APIServer) handleStartedJob(w http.ResponseWriter, r *http.Request) {
	handleStartedJob(s.db)(w, r)
}

func (s *APIServer) handleReportPrinters(w http.ResponseWriter, r *http.Request) {
	handleReportPrinters(s.db)(w, r)
}

func (s *APIServer) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	handleTelemetry(s.db)(w, r)
}

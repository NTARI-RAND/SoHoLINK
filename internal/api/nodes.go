package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/metrics"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

type registerNodeRequest struct {
	NodeID      string `json:"node_id"`
	ProviderID  string `json:"provider_id"`
	NodeClass   string `json:"node_class"`
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
	NodeID string `json:"node_id"`
}

type telemetryRequest struct {
	NodeID        string    `json:"node_id"`
	CPUPct        float64   `json:"cpu_pct"`
	RAMPct        float64   `json:"ram_pct"`
	BandwidthMbps int       `json:"bandwidth_mbps"`
	Timestamp     time.Time `json:"timestamp"`
}

type jobEntry struct {
	JobID    string `json:"job_id"`
	JobToken string `json:"job_token"`
}

func registerNodeRoutes(mux *http.ServeMux, db *store.DB, registry *orchestrator.NodeRegistry) {
	mux.HandleFunc("POST /nodes/register", handleRegisterNode(db, registry))
	mux.HandleFunc("POST /nodes/heartbeat", handleHeartbeat(db, registry))
	mux.HandleFunc("GET /nodes/jobs", handleGetJobs(db))
	mux.HandleFunc("POST /jobs/{id}/telemetry", handleTelemetry(db))
	mux.HandleFunc("POST /jobs/{id}/complete", handleCompleteJob(db))
}

func handleRegisterNode(db *store.DB, registry *orchestrator.NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			ProviderID:    req.ProviderID,
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"registered": true}) //nolint:errcheck
	}
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

		if err := registry.Heartbeat(req.NodeID); err != nil {
			writeError(w, http.StatusNotFound, "node not found in registry")
			return
		}

		_, err := db.Pool.Exec(r.Context(),
			`UPDATE nodes SET last_heartbeat_at = NOW(), updated_at = NOW() WHERE id = $1`,
			req.NodeID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		_, err = db.Pool.Exec(r.Context(),
			`INSERT INTO node_heartbeat_events (node_id) VALUES ($1)`,
			req.NodeID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		metrics.HeartbeatsTotal.WithLabelValues(req.NodeID).Inc()

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

		rows, err := db.Pool.Query(r.Context(),
			`SELECT id, COALESCE(job_token, '') FROM jobs
			 WHERE node_id = $1 AND status = 'scheduled'::job_status`,
			nodeID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		defer rows.Close()

		jobs := []jobEntry{}
		var jobIDs []string
		for rows.Next() {
			var j jobEntry
			if err := rows.Scan(&j.JobID, &j.JobToken); err != nil {
				writeError(w, http.StatusInternalServerError, "scan error")
				return
			}
			jobs = append(jobs, j)
			jobIDs = append(jobIDs, j.JobID)
		}
		if err := rows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		if len(jobIDs) > 0 {
			_, err = db.Pool.Exec(r.Context(),
				`UPDATE jobs SET started_at = NOW(), status = 'running'::job_status, updated_at = NOW()
				 WHERE id = ANY($1) AND started_at IS NULL`,
				jobIDs,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobs) //nolint:errcheck
	}
}

func handleCompleteJob(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "job ID required")
			return
		}

		var nodeSpiffeID string
		err := db.Pool.QueryRow(r.Context(), `
			SELECT COALESCE(n.spiffe_id, '')
			FROM jobs j
			INNER JOIN nodes n ON j.node_id = n.id
			WHERE j.id = $1`,
			jobID,
		).Scan(&nodeSpiffeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		// Enforce SPIFFE ownership only when the node has a registered SPIFFE ID.
		// In production RequireSPIFFE middleware guarantees a valid SVID on the
		// wire regardless; nodes still bootstrapping SPIRE have no spiffe_id yet.
		if nodeSpiffeID != "" {
			spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
			if !ok || spiffeID.String() != nodeSpiffeID {
				writeError(w, http.StatusForbidden, "SPIFFE identity does not match job owner")
				return
			}
		}

		tag, err := db.Pool.Exec(r.Context(),
			`UPDATE jobs SET status = 'completed'::job_status, completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1 AND status = 'running'::job_status`,
			jobID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusConflict, "job is not in running state")
			return
		}

		if err := store.ComputeMetering(r.Context(), db, jobID); err != nil {
			log.Printf("ComputeMetering job=%s error=%v", jobID, err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"completed": true}) //nolint:errcheck
	}
}

func handleTelemetry(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("id")
		if jobID == "" {
			writeError(w, http.StatusBadRequest, "job ID required")
			return
		}

		spiffeID, ok := identity.SPIFFEIDFromContext(r.Context())
		if !ok {
			// RequireSPIFFE guarantees this is set; be defensive anyway.
			writeError(w, http.StatusUnauthorized, "no SPIFFE identity in context")
			return
		}

		// Verify the caller's SPIFFE ID matches the node that owns the job.
		// If the node has no registered spiffe_id yet, the check is skipped.
		var nodeSpiffeID string
		err := db.Pool.QueryRow(r.Context(), `
			SELECT COALESCE(n.spiffe_id, '')
			FROM jobs j
			INNER JOIN nodes n ON j.node_id = n.id
			WHERE j.id = $1`,
			jobID,
		).Scan(&nodeSpiffeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		if nodeSpiffeID != "" && spiffeID.String() != nodeSpiffeID {
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
	handleCompleteJob(s.db)(w, r)
}

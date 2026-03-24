package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/moderation"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestration"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/services"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// getGPUModel extracts GPU model name from node's GPU profile.
func getGPUModel(n *orchestration.Node) string {
	if n.GPUProfile != nil {
		return n.GPUProfile.Model
	}
	return ""
}

// ---------------------------------------------------------------------------
// Pricing constants (sats)  — platform default rates used for estimation.
// Actual rates are determined by provider bids when order is placed.
// ---------------------------------------------------------------------------

const (
	defaultCPUHourSats  = int64(100) // per core per hour
	defaultMemHourSats  = int64(10)  // per GiB per hour
	defaultDiskHourSats = int64(1)   // per GiB per hour
	platformFeePct      = 0.01       // 1 % platform fee
)

// ---------------------------------------------------------------------------
// Node browsing
// ---------------------------------------------------------------------------

// handleMarketplaceNodes serves GET /api/marketplace/nodes.
// Accepts optional query params: min_cpu, max_price_sats, region, gpu, min_reputation.
func (s *Server) handleMarketplaceNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Orchestration scheduler not configured", http.StatusServiceUnavailable)
		return
	}

	q := orchestration.NodeQuery{}

	if v := r.URL.Query().Get("min_cpu"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			q.MinCPU = f
		}
	}
	if v := r.URL.Query().Get("max_price_sats"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.MaxCostPerHour = n
		}
	}
	if v := r.URL.Query().Get("region"); v != "" {
		q.Regions = []string{v}
	}
	if v := r.URL.Query().Get("gpu"); v == "true" || v == "1" {
		q.GPURequired = true
	}
	if v := r.URL.Query().Get("min_reputation"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.MinReputation = n
		}
	}

	nodes, err := s.scheduler.FindNodes(r.Context(), q)
	if err != nil {
		log.Printf("[marketplace] FindNodes error: %v", err)
		http.Error(w, "Failed to query available nodes", http.StatusInternalServerError)
		return
	}
	if nodes == nil {
		nodes = []*orchestration.Node{}
	}

	// Convert to API-safe view (omit internal fields)
	type nodeView struct {
		NodeDID              string  `json:"node_did"`
		Address              string  `json:"address"`
		Region               string  `json:"region"`
		AvailableCPU         float64 `json:"available_cpu"`
		AvailableMemoryMB    int64   `json:"available_memory_mb"`
		AvailableDiskGB      int64   `json:"available_disk_gb"`
		HasGPU               bool    `json:"has_gpu"`
		GPUModel             string  `json:"gpu_model"`
		PricePerCPUHourSats  int64   `json:"price_per_cpu_hour_sats"`
		ReputationScore      int     `json:"reputation_score"`
		UptimePct            float64 `json:"uptime_pct"`
		Status               string  `json:"status"`
	}

	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		views = append(views, nodeView{
			NodeDID:             n.DID,
			Address:             n.Address,
			Region:              n.Region,
			AvailableCPU:        n.AvailableCPU,
			AvailableMemoryMB:   n.AvailableMemoryMB,
			AvailableDiskGB:     n.AvailableDiskGB,
			HasGPU:              n.HasGPU,
			GPUModel:            getGPUModel(n),
			PricePerCPUHourSats: n.PricePerCPUHour,
			ReputationScore:     n.ReputationScore,
			UptimePct:           n.UptimePercent,
			Status:              n.Status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"nodes": views,
		"count": len(views),
	})
}

// ---------------------------------------------------------------------------
// Cost estimation
// ---------------------------------------------------------------------------

// estimateRequest is the JSON body for POST /api/marketplace/estimate.
type estimateRequest struct {
	CPUCores      float64 `json:"cpu_cores"`
	MemoryMB      int64   `json:"memory_mb"`
	DiskGB        int64   `json:"disk_gb"`
	DurationHours int     `json:"duration_hours"`
}

// estimateResult is the JSON response from the estimate endpoint.
type estimateResult struct {
	CPUCostSats      int64   `json:"cpu_cost_sats"`
	MemoryCostSats   int64   `json:"memory_cost_sats"`
	DiskCostSats     int64   `json:"disk_cost_sats"`
	PlatformFeeSats  int64   `json:"platform_fee_sats"`
	TotalSats        int64   `json:"total_sats"`
	TotalUSD         float64 `json:"total_usd"`
	BTCUSDRate       float64 `json:"btc_usd_rate"`
	DurationHours    int     `json:"duration_hours"`
}

// handleMarketplaceEstimate serves POST /api/marketplace/estimate.
func (s *Server) handleMarketplaceEstimate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req estimateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.CPUCores <= 0 && req.MemoryMB <= 0 && req.DiskGB <= 0 {
		http.Error(w, "At least one resource (cpu_cores, memory_mb, disk_gb) must be specified", http.StatusBadRequest)
		return
	}
	if req.DurationHours <= 0 {
		req.DurationHours = 1
	}

	result := computeEstimate(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result) // #nosec G104
}

// computeEstimate applies platform pricing rules to produce a cost breakdown.
func computeEstimate(req estimateRequest) estimateResult {
	cpuCost := int64(req.CPUCores*float64(defaultCPUHourSats)) * int64(req.DurationHours)
	memCost := (req.MemoryMB / 1024) * defaultMemHourSats * int64(req.DurationHours)
	diskCost := req.DiskGB * defaultDiskHourSats * int64(req.DurationHours)
	subtotal := cpuCost + memCost + diskCost
	fee := int64(float64(subtotal) * platformFeePct)
	total := subtotal + fee

	btcRate := GetBtcUsdRate()
	totalUSD := float64(total) / 1e8 * btcRate

	return estimateResult{
		CPUCostSats:     cpuCost,
		MemoryCostSats:  memCost,
		DiskCostSats:    diskCost,
		PlatformFeeSats: fee,
		TotalSats:       total,
		TotalUSD:        totalUSD,
		BTCUSDRate:      btcRate,
		DurationHours:   req.DurationHours,
	}
}

// ---------------------------------------------------------------------------
// Managed service catalog
// ---------------------------------------------------------------------------

// handleMarketplaceServices serves GET /api/marketplace/services.
// Returns all service plans grouped by service type.
func (s *Server) handleMarketplaceServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type planView struct {
		PlanID       string  `json:"plan_id"`
		Name         string  `json:"name"`
		CPUCores     float64 `json:"cpu_cores"`
		MemoryMB     int64   `json:"memory_mb"`
		StorageGB    int64   `json:"storage_gb"`
		PricePerDay  int64   `json:"price_per_day_sats"`
		HA           bool    `json:"ha"`
		Replicas     int     `json:"replicas"`
	}
	type serviceGroup struct {
		Type  string     `json:"type"`
		Plans []planView `json:"plans"`
	}

	if s.catalog == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
			"services": []serviceGroup{},
		})
		return
	}

	serviceTypes := []services.ServiceType{
		services.ServiceTypePostgres,
		services.ServiceTypeMySQL,
		services.ServiceTypeMongoDB,
		services.ServiceTypeRedis,
		services.ServiceTypeObjectStore,
		services.ServiceTypeMessageQueue,
	}

	groups := make([]serviceGroup, 0, len(serviceTypes))
	for _, st := range serviceTypes {
		plans := s.catalog.GetPlans(st)
		if len(plans) == 0 {
			continue
		}
		views := make([]planView, 0, len(plans))
		for _, p := range plans {
			views = append(views, planView{
				PlanID:      p.PlanID,
				Name:        p.Name,
				CPUCores:    p.CPUCores,
				MemoryMB:    p.MemoryMB,
				StorageGB:   p.StorageGB,
				PricePerDay: p.PricePerDay,
				HA:          p.HA,
				Replicas:    p.Replicas,
			})
		}
		groups = append(groups, serviceGroup{Type: string(st), Plans: views})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"services": groups,
	})
}

// ---------------------------------------------------------------------------
// Workload purchase
// ---------------------------------------------------------------------------

// purchaseRequest is the JSON body for POST /api/marketplace/purchase.
// The Manifest field is required (Item 5 — workload intent declaration).
type purchaseRequest struct {
	CPUCores      float64                              `json:"cpu_cores"`
	MemoryMB      int64                                `json:"memory_mb"`
	DiskGB        int64                                `json:"disk_gb"`
	DurationHours int                                  `json:"duration_hours"`
	Replicas      int                                  `json:"replicas"`
	Constraints   orchestration.PlacementConstraints   `json:"constraints,omitempty"`
	Description   string                               `json:"description"`
	Image         string                               `json:"image"`
	Manifest      moderation.WorkloadManifest          `json:"manifest"`
}

// handleMarketplacePurchase serves POST /api/marketplace/purchase.
// Debits the wallet and submits a workload to the scheduler.
func (s *Server) handleMarketplacePurchase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler == nil {
		http.Error(w, "Orchestration scheduler not configured", http.StatusServiceUnavailable)
		return
	}
	if s.paymentLedger == nil {
		http.Error(w, "Payment system not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	did, err := s.store.GetNodeInfo(ctx, "owner_did")
	if err != nil || did == "" {
		http.Error(w, "Owner DID not configured", http.StatusInternalServerError)
		return
	}

	var req purchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.CPUCores <= 0 {
		http.Error(w, "cpu_cores must be positive", http.StatusBadRequest)
		return
	}
	if req.DurationHours <= 0 {
		req.DurationHours = 1
	}
	if req.Replicas <= 0 {
		req.Replicas = 1
	}

	// Validate workload manifest (Item 5 — required intent declaration)
	if errs := moderation.ValidateManifest(&req.Manifest); len(errs) > 0 {
		workloadPurchaseTotal.WithLabelValues("manifest_rejected").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"error":             "invalid manifest",
			"validation_errors": errs,
		})
		return
	}

	// OPA safety policy check (Items 3 & 4 — prohibition rules + network egress)
	if s.safetyPolicy != nil {
		if allowed, denyReasons, policyErr := s.safetyPolicy.Allow(r.Context(), req.Manifest, ""); policyErr != nil {
			log.Printf("[marketplace] safety policy eval error: %v", policyErr)
			// On eval error, allow with warning (fail-open for policy errors, not content errors)
		} else if !allowed {
			log.Printf("[marketplace] workload DENIED by safety policy: did=%s reasons=%v", did, denyReasons)
			workloadPurchaseTotal.WithLabelValues("policy_denied").Inc()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"error":        "workload rejected by platform safety policy",
				"deny_reasons": denyReasons,
			})
			return
		}
	}

	// Compute cost
	est := computeEstimate(estimateRequest{
		CPUCores:      req.CPUCores,
		MemoryMB:      req.MemoryMB,
		DiskGB:        req.DiskGB,
		DurationHours: req.DurationHours,
	})

	// Debit wallet — returns 402 if insufficient balance
	if err := s.paymentLedger.DebitWallet(ctx, did, est.TotalSats); err != nil {
		log.Printf("[marketplace] DebitWallet error: %v", err)
		workloadPurchaseTotal.WithLabelValues("payment_failed").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
			"error":       "Insufficient wallet balance",
			"needed_sats": est.TotalSats,
		})
		return
	}

	// Generate IDs
	workloadID := fmt.Sprintf("wl_%d", time.Now().UnixNano())
	orderID := fmt.Sprintf("ord_%d", time.Now().UnixNano())

	// Submit workload
	workload := &orchestration.Workload{
		WorkloadID: workloadID,
		OwnerDID:   did,
		Type:       "container",
		Replicas:   req.Replicas,
		Spec: orchestration.WorkloadSpec{
			CPUCores: req.CPUCores,
			MemoryMB: req.MemoryMB,
			DiskGB:   req.DiskGB,
			Image:    req.Image,
			Timeout:  time.Duration(req.DurationHours) * time.Hour,
		},
		Constraints:  req.Constraints,
		Status:       "pending",
		DesiredState: "running",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	s.scheduler.SubmitWorkload(workload)

	// Marshal manifest for audit trail storage (Item 5)
	manifestJSON := ""
	if mjBytes, mjErr := json.Marshal(req.Manifest); mjErr == nil {
		manifestJSON = string(mjBytes)
	}

	// Record order
	orderRow := &store.OrderRow{
		OrderID:       orderID,
		RequesterDID:  did,
		OrderType:     "workload",
		ResourceRefID: workloadID,
		Description:   req.Description,
		CPUCores:      req.CPUCores,
		MemoryMB:      req.MemoryMB,
		DiskGB:        req.DiskGB,
		DurationHours: req.DurationHours,
		EstimatedSats: est.TotalSats,
		ChargedSats:   est.TotalSats,
		Status:        "pending",
		ManifestJSON:  manifestJSON,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.store.CreateOrder(ctx, orderRow); err != nil {
		log.Printf("[marketplace] CreateOrder error: %v", err)
		// Workload and payment already committed — log but don't fail the response
	}

	workloadPurchaseTotal.WithLabelValues("success").Inc()
	log.Printf("[marketplace] purchase: order=%s workload=%s charged=%d sats did=%s",
		orderID, workloadID, est.TotalSats, did)

	estimatedCompletion := time.Now().UTC().Add(time.Duration(req.DurationHours) * time.Hour)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"order_id":             orderID,
		"workload_id":          workloadID,
		"charged_sats":         est.TotalSats,
		"estimated_completion": estimatedCompletion.Format(time.RFC3339),
		"status":               "pending",
	})
}

// ---------------------------------------------------------------------------
// Managed-service purchase
// ---------------------------------------------------------------------------

// purchaseServiceRequest is the JSON body for POST /api/marketplace/purchase-service.
type purchaseServiceRequest struct {
	ServiceType string `json:"service_type"` // "postgres", "object_storage", etc.
	Plan        string `json:"plan"`         // e.g. "pg-starter"
	Name        string `json:"name"`         // user-defined instance name
	Region      string `json:"region"`
}

// handleMarketplacePurchaseService serves POST /api/marketplace/purchase-service.
// Debits 24 h upfront cost and provisions the managed service instance.
func (s *Server) handleMarketplacePurchaseService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.catalog == nil {
		http.Error(w, "Service catalog not configured", http.StatusServiceUnavailable)
		return
	}
	if s.paymentLedger == nil {
		http.Error(w, "Payment system not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	did, err := s.store.GetNodeInfo(ctx, "owner_did")
	if err != nil || did == "" {
		http.Error(w, "Owner DID not configured", http.StatusInternalServerError)
		return
	}

	var req purchaseServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ServiceType == "" || req.Plan == "" || req.Name == "" {
		http.Error(w, "service_type, plan, and name are required", http.StatusBadRequest)
		return
	}

	// Look up plan to get 24-hour cost
	plans := s.catalog.GetPlans(services.ServiceType(req.ServiceType))
	var matchedPlan *services.ServicePlan
	for i := range plans {
		if plans[i].PlanID == req.Plan {
			matchedPlan = &plans[i]
			break
		}
	}
	if matchedPlan == nil {
		http.Error(w, fmt.Sprintf("Plan %q not found for service type %q", req.Plan, req.ServiceType), http.StatusBadRequest)
		return
	}

	chargedSats := matchedPlan.PricePerDay // 24h upfront

	// Debit wallet
	if err := s.paymentLedger.DebitWallet(ctx, did, chargedSats); err != nil {
		log.Printf("[marketplace] DebitWallet (service) error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
			"error":       "Insufficient wallet balance",
			"needed_sats": chargedSats,
		})
		return
	}

	// Provision the service
	instance, err := s.catalog.Provision(ctx, services.ProvisionRequest{
		OwnerDID:    did,
		ServiceType: services.ServiceType(req.ServiceType),
		Name:        req.Name,
		Plan:        req.Plan,
		Region:      req.Region,
	})
	if err != nil {
		log.Printf("[marketplace] Provision error: %v", err)
		// Attempt to refund on provision failure
		if refundErr := s.store.CreditWallet(ctx, did, chargedSats); refundErr != nil {
			log.Printf("[marketplace] refund after provision failure error: %v", refundErr)
		}
		http.Error(w, fmt.Sprintf("Failed to provision service: %v", err), http.StatusInternalServerError)
		return
	}

	// Record order
	orderID := fmt.Sprintf("ord_%d", time.Now().UnixNano())
	orderRow := &store.OrderRow{
		OrderID:       orderID,
		RequesterDID:  did,
		OrderType:     "service",
		ResourceRefID: instance.InstanceID,
		Description:   fmt.Sprintf("%s %s (%s)", req.ServiceType, req.Name, req.Plan),
		EstimatedSats: chargedSats,
		ChargedSats:   chargedSats,
		Status:        "running",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.store.CreateOrder(ctx, orderRow); err != nil {
		log.Printf("[marketplace] CreateOrder (service) error: %v", err)
	}

	log.Printf("[marketplace] service purchase: order=%s instance=%s charged=%d sats did=%s",
		orderID, instance.InstanceID, chargedSats, did)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"order_id":     orderID,
		"instance_id":  instance.InstanceID,
		"endpoint":     instance.Endpoint,
		"port":         instance.Port,
		"charged_sats": chargedSats,
		"status":       string(instance.Status),
		"credentials": map[string]string{
			"username": instance.Credentials.Username,
			"password": instance.Credentials.Password,
			"database": instance.Credentials.Database,
			"token":    instance.Credentials.Token,
		},
	})
}

// ---------------------------------------------------------------------------
// Order management
// ---------------------------------------------------------------------------

// handleListOrders serves GET /api/orders.
func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	did, err := s.store.GetNodeInfo(ctx, "owner_did")
	if err != nil || did == "" {
		http.Error(w, "Owner DID not configured", http.StatusInternalServerError)
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, parseErr := strconv.Atoi(l); parseErr == nil && n > 0 {
			limit = n
		}
	}

	orders, err := s.store.ListOrders(ctx, did, limit)
	if err != nil {
		log.Printf("[marketplace] ListOrders error: %v", err)
		http.Error(w, "Failed to retrieve orders", http.StatusInternalServerError)
		return
	}
	if orders == nil {
		orders = []store.OrderRow{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"orders": orders,
		"count":  len(orders),
	})
}

// handleOrderByID routes GET and POST /api/orders/{id} and /api/orders/{id}/cancel.
func (s *Server) handleOrderByID(w http.ResponseWriter, r *http.Request) {
	// Path: /api/orders/{id} or /api/orders/{id}/cancel
	path := strings.TrimPrefix(r.URL.Path, "/api/orders/")
	isCancelReq := strings.HasSuffix(path, "/cancel")
	orderID := strings.TrimSuffix(path, "/cancel")
	orderID = strings.TrimSuffix(orderID, "/")

	if orderID == "" {
		// Trailing slash with no ID → delegate to list handler
		s.handleListOrders(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && !isCancelReq:
		s.handleGetOrder(w, r, orderID)
	case r.Method == http.MethodPost && isCancelReq:
		s.handleCancelOrder(w, r, orderID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetOrder serves GET /api/orders/{id}.
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	ctx := r.Context()
	order, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		log.Printf("[marketplace] GetOrder error: %v", err)
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"order": order,
	}

	// Augment workload orders with live scheduler state
	if order.OrderType == "workload" && s.scheduler != nil {
		state := s.scheduler.GetWorkloadState(order.ResourceRefID)
		if state != nil {
			resp["workload_status"] = state.Status
			resp["placements"] = state.Placements
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // #nosec G104
}

// handleCancelOrder serves POST /api/orders/{id}/cancel.
// Cancels an active workload, computes unused time, and issues a proportional refund.
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	ctx := r.Context()
	did, err := s.store.GetNodeInfo(ctx, "owner_did")
	if err != nil || did == "" {
		http.Error(w, "Owner DID not configured", http.StatusInternalServerError)
		return
	}

	order, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if order.RequesterDID != did {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if order.Status != "pending" && order.Status != "running" {
		http.Error(w, fmt.Sprintf("Order cannot be cancelled (status: %s)", order.Status), http.StatusConflict)
		return
	}

	// Delete workload from scheduler if applicable
	if order.OrderType == "workload" && s.scheduler != nil {
		if err := s.scheduler.DeleteWorkload(ctx, order.ResourceRefID); err != nil {
			log.Printf("[marketplace] DeleteWorkload error: %v", err)
		}
	}

	// Compute proportional refund based on elapsed time
	elapsed := time.Since(order.CreatedAt)
	totalDur := time.Duration(order.DurationHours) * time.Hour
	var refundSats int64
	if elapsed < totalDur {
		unusedFraction := 1.0 - elapsed.Seconds()/totalDur.Seconds()
		refundSats = int64(float64(order.ChargedSats) * unusedFraction)
	}

	// Credit refund to wallet
	if refundSats > 0 {
		if err := s.store.CreditWallet(ctx, did, refundSats); err != nil {
			log.Printf("[marketplace] CreditWallet (cancel refund) error: %v", err)
		}
	}

	// Update order status
	if err := s.store.UpdateOrderStatus(ctx, orderID, "cancelled"); err != nil {
		log.Printf("[marketplace] UpdateOrderStatus error: %v", err)
	}

	newBalance, _ := s.store.GetWalletBalance(ctx, did)

	log.Printf("[marketplace] order %s cancelled — refund=%d sats, new_balance=%d sats", orderID, refundSats, newBalance)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // #nosec G104
		"status":           "cancelled",
		"refund_sats":      refundSats,
		"new_balance_sats": newBalance,
	})
}

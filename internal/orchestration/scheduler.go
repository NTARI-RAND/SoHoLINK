package orchestration

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/ml"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// FedScheduler is the central elastic orchestrator. It receives workloads,
// discovers suitable nodes, places replicas, monitors health, and auto-scales.
type FedScheduler struct {
	store     *store.Store
	discovery *NodeDiscovery
	placer    *Placer
	scaler    *AutoScaler
	monitor   *WorkloadMonitor
	portMgr   *PortManager       // Port allocation and firewall rules
	executor  *WorkloadExecutor  // Workload execution with network isolation
	policyEng *policy.Engine     // Policy engine for thermal budgets and scheduling rules

	// Work queues
	PendingQueue chan *Workload
	scalingQueue chan ScaleEvent

	// State
	mu              sync.RWMutex
	ActiveWorkloads map[string]*WorkloadState
	nodeCapacity    map[string]*NodeCapacity

	// mobileHub is set via SetMobileHub and used by ScheduleMobile.
	// Stored as the interface type to avoid an import cycle with httpapi.
	mobileHub MobileHub

	// mlBandit is the contextual bandit used by ScheduleMobile for node
	// selection.  If nil, the scheduler falls back to random round-robin.
	mlBandit *ml.LinUCBBandit

	// telemetry records scheduling decisions and outcomes for offline training.
	// If nil, telemetry is disabled.
	telemetry *ml.TelemetryRecorder

	// Two-tier federation topology (Sprint 3.5)
	clusterMgr *ClusterManager  // Manages local cluster membership
	meshGossip *MeshGossiper    // Global mesh for inter-cluster routing
}

// ScaleEvent is an internal event requesting a workload scale operation.
type ScaleEvent struct {
	WorkloadID     string
	TargetReplicas int
}

// NewFedScheduler creates a new federated scheduler.
func NewFedScheduler(s *store.Store) *FedScheduler {
	sched := &FedScheduler{
		store:           s,
		PendingQueue:    make(chan *Workload, 1000),
		scalingQueue:    make(chan ScaleEvent, 1000),
		ActiveWorkloads: make(map[string]*WorkloadState),
		nodeCapacity:    make(map[string]*NodeCapacity),
		portMgr:         NewPortManager(),
		executor:        NewWorkloadExecutor("/var/lib/soholink/workloads"), // Base directory for isolated workloads
	}

	sched.discovery = NewNodeDiscovery(s)
	sched.placer = NewPlacer()
	sched.monitor = NewWorkloadMonitor(sched)
	sched.scaler = NewAutoScaler(sched, sched.monitor)

	return sched
}

// Start launches all scheduler loops.
func (s *FedScheduler) Start(ctx context.Context) {
	go s.scheduleLoop(ctx)
	go s.scalingLoop(ctx)
	go s.monitor.MonitorLoop(ctx)
	go s.discovery.DiscoverLoop(ctx)
	log.Printf("[orchestration] FedScheduler started")
}

// Stop cancels in-flight work (relies on context cancellation from app).
func (s *FedScheduler) Stop() {
	log.Printf("[orchestration] FedScheduler stopping")
}

// FindNodes filters the federation node registry using the provided query and
// returns matching nodes sorted by reputation score (highest first).
// This exposes the internal NodeDiscovery to the marketplace HTTP API.
func (s *FedScheduler) FindNodes(ctx context.Context, q NodeQuery) ([]*Node, error) {
	if s.discovery == nil {
		return nil, fmt.Errorf("node discovery not initialised")
	}
	return s.discovery.FindNodes(ctx, q)
}

// SubmitWorkload queues a workload for scheduling.
func (s *FedScheduler) SubmitWorkload(w *Workload) {
	w.Status = "pending"
	w.CreatedAt = time.Now()
	w.UpdatedAt = time.Now()
	s.PendingQueue <- w
}

// GetWorkloadState returns the runtime state of a workload.
func (s *FedScheduler) GetWorkloadState(workloadID string) *WorkloadState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ActiveWorkloads[workloadID]
}

// ListActiveWorkloads returns a snapshot of all active workload states.
// Each returned WorkloadState contains a deep-copied Placements slice so that
// callers cannot race with handleScaleEvent, which mutates Placements under lock.
func (s *FedScheduler) ListActiveWorkloads() []*WorkloadState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*WorkloadState, 0, len(s.ActiveWorkloads))
	for _, ws := range s.ActiveWorkloads {
		placements := make([]Placement, len(ws.Placements))
		copy(placements, ws.Placements)
		result = append(result, &WorkloadState{
			Workload:   ws.Workload,
			Placements: placements,
			Health:     ws.Health,
		})
	}
	return result
}

// ActivePlacements implements payment.PlacementSource.
// Returns a snapshot of all currently-running placement billing records.
// Only placements with status "running" are included; pending/failed are skipped.
func (s *FedScheduler) ActivePlacements() []payment.ActivePlacement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []payment.ActivePlacement
	for _, state := range s.ActiveWorkloads {
		if state.Workload == nil {
			continue
		}
		for _, p := range state.Placements {
			if p.Status != "running" {
				continue
			}
			out = append(out, payment.ActivePlacement{
				PlacementID:  p.PlacementID,
				WorkloadID:   state.Workload.WorkloadID,
				OwnerDID:     state.Workload.OwnerDID,
				ProviderDID:  p.NodeDID,
				CPUCores:     state.Workload.Spec.CPUCores,
				MemoryMB:     state.Workload.Spec.MemoryMB,
				DiskGB:       state.Workload.Spec.DiskGB,
				PricePerHour: 0, // populated from federation_nodes registry by caller when needed
				StartedAt:    p.StartedAt,
			})
		}
	}
	return out
}

// scheduleLoop pulls workloads from the pending queue and schedules them.
func (s *FedScheduler) scheduleLoop(ctx context.Context) {
	for {
		select {
		case w := <-s.PendingQueue:
			if err := s.scheduleWorkload(ctx, w); err != nil {
				log.Printf("[orchestration] failed to schedule %s: %v", w.WorkloadID, err)
				w.Status = "failed"
			}
		case <-ctx.Done():
			return
		}
	}
}

// scalingLoop processes scale events from the auto-scaler.
func (s *FedScheduler) scalingLoop(ctx context.Context) {
	for {
		select {
		case ev := <-s.scalingQueue:
			s.handleScaleEvent(ctx, ev)
		case <-ctx.Done():
			return
		}
	}
}

// scheduleWorkload finds suitable nodes and places replicas.
func (s *FedScheduler) scheduleWorkload(ctx context.Context, w *Workload) error {
	log.Printf("[orchestration] scheduling workload %s (%d replicas)", w.WorkloadID, w.Replicas)

	// Allocate ports for this workload (port isolation security)
	portMap, err := s.AllocateWorkloadPorts(w.WorkloadID, w.Spec.Ports)
	if err != nil {
		w.Status = "failed"
		return fmt.Errorf("port allocation failed: %w", err)
	}
	log.Printf("[orchestration] port allocation: %v", portMap)

	candidates, err := s.discovery.FindNodes(ctx, NodeQuery{
		MinCPU:         w.Spec.CPUCores,
		MinMemory:      w.Spec.MemoryMB,
		MinDisk:        w.Spec.DiskGB,
		GPURequired:    w.Spec.GPURequired,
		GPUModel:       w.Spec.GPUModel,
		Regions:        w.Constraints.Regions,
		MinReputation:  w.Constraints.MinProviderScore,
		MaxCostPerHour: w.Constraints.MaxCostPerHour,
	})
	if err != nil || len(candidates) == 0 {
		// Cleanup allocated ports on failure
		s.ReleaseWorkloadPorts(w.WorkloadID)
		return fmt.Errorf("no suitable nodes found for workload %s", w.WorkloadID)
	}

	// Score and sort candidates
	scores := s.placer.ScoreNodes(candidates, w)
	sort.Slice(candidates, func(i, j int) bool {
		return scores[candidates[i].DID] > scores[candidates[j].DID]
	})

	// Filter candidates by thermal policy (if policy engine is available)
	if s.policyEng != nil {
		candidates = s.filterCandidatesByPolicy(ctx, candidates, w)
		if len(candidates) == 0 {
			s.ReleaseWorkloadPorts(w.WorkloadID)
			return fmt.Errorf("no nodes passed thermal policy checks for workload %s", w.WorkloadID)
		}
	}

	// Filter candidates by capability requirements
	candidates = s.filterCandidatesByCapability(candidates, w)
	if len(candidates) == 0 {
		s.ReleaseWorkloadPorts(w.WorkloadID)
		return fmt.Errorf("no nodes support required capabilities for workload %s", w.WorkloadID)
	}

	// Place replicas (anti-affinity: avoid same node)
	var placements []Placement
	usedNodes := make(map[string]bool)

	for i := 0; i < w.Replicas && i < len(candidates); i++ {
		// Pick a candidate not yet used (for anti-affinity)
		var chosen *Node
		for _, c := range candidates {
			if !usedNodes[c.DID] {
				chosen = c
				break
			}
		}
		if chosen == nil {
			// Allow reuse if not enough unique nodes
			chosen = candidates[i%len(candidates)]
		}

		placement := Placement{
			PlacementID: fmt.Sprintf("pl_%s_%d_%d", w.WorkloadID, i, time.Now().UnixNano()),
			WorkloadID:  w.WorkloadID,
			ReplicaNum:  i,
			NodeDID:     chosen.DID,
			NodeAddress: chosen.Address,
			Status:      "running",
			StartedAt:   time.Now(),
		}

		placements = append(placements, placement)
		usedNodes[chosen.DID] = true

		// Reserve capacity
		s.reserveCapacity(chosen.DID, w.Spec)
	}

	if len(placements) == 0 {
		return fmt.Errorf("failed to place any replicas for %s", w.WorkloadID)
	}

	w.Status = "running"
	w.UpdatedAt = time.Now()

	s.mu.Lock()
	s.ActiveWorkloads[w.WorkloadID] = &WorkloadState{
		Workload:   w,
		Placements: placements,
		Health:     HealthStatus{Status: "healthy"},
	}
	s.mu.Unlock()

	// Store placements
	for _, p := range placements {
		_ = s.store.CreatePlacement(ctx, &store.PlacementRow{
			PlacementID: p.PlacementID,
			WorkloadID:  p.WorkloadID,
			ReplicaNum:  p.ReplicaNum,
			NodeDID:     p.NodeDID,
			NodeAddress: p.NodeAddress,
			Status:      p.Status,
			StartedAt:   p.StartedAt,
		})
	}

	log.Printf("[orchestration] workload %s scheduled (%d replicas placed)", w.WorkloadID, len(placements))
	return nil
}

// handleScaleEvent adjusts the number of replicas for a workload.
func (s *FedScheduler) handleScaleEvent(ctx context.Context, ev ScaleEvent) {
	s.mu.Lock()
	state, ok := s.ActiveWorkloads[ev.WorkloadID]
	if !ok {
		s.mu.Unlock()
		return
	}

	current := len(state.Placements)
	target := ev.TargetReplicas
	// Copy fields needed after unlock to avoid data race on WorkloadState.
	spec := state.Workload.Spec
	constraints := state.Workload.Constraints
	s.mu.Unlock()

	if target > current {
		// Scale up — submit extra replica placements
		extra := &Workload{
			WorkloadID:  ev.WorkloadID,
			Spec:        spec,
			Constraints: constraints,
			Replicas:    target - current,
		}
		_ = s.scheduleWorkload(ctx, extra)
	} else if target < current {
		// Scale down — remove trailing placements
		s.mu.Lock()
		remove := current - target
		if remove > len(state.Placements) {
			remove = len(state.Placements)
		}
		state.Placements = state.Placements[:len(state.Placements)-remove]
		s.mu.Unlock()
	}
}

// RemovePlacement removes a single placement (for scale-down).
// Note: Ports are shared at the workload level, not per-placement.
// Ports are only released when the entire workload is deleted.
func (s *FedScheduler) RemovePlacement(ctx context.Context, placementID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ws := range s.ActiveWorkloads {
		for i, p := range ws.Placements {
			if p.PlacementID == placementID {
				ws.Placements = append(ws.Placements[:i], ws.Placements[i+1:]...)
				log.Printf("[orchestration] removed placement %s for workload %s", placementID, ws.Workload.WorkloadID)
				return
			}
		}
	}
}

// reserveCapacity reduces tracked available capacity for a node.
func (s *FedScheduler) reserveCapacity(nodeDID string, spec WorkloadSpec) {
	cap, ok := s.nodeCapacity[nodeDID]
	if !ok {
		return
	}
	cap.AvailableCPU -= spec.CPUCores
	cap.AvailableMem -= spec.MemoryMB
	cap.AvailableDisk -= spec.DiskGB
	cap.ActiveJobs++
}

// ---------------------------------------------------------------------------
// Mobile scheduling
// ---------------------------------------------------------------------------

// MobileHub is the minimal interface the scheduler needs to push tasks to
// mobile nodes.  It is satisfied by *httpapi.MobileHub but defined here to
// avoid an import cycle.
type MobileHub interface {
	PushTask(nodeDID string, task MobileTaskDescriptor) bool
	ActiveNodes() []MobileNodeInfo
}

// SetMobileHub wires the mobile WebSocket hub into the scheduler so that
// ScheduleMobile can push task descriptors to connected mobile nodes.
func (s *FedScheduler) SetMobileHub(hub MobileHub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mobileHub = hub
}

// SetMLBandit attaches a contextual bandit for node selection in ScheduleMobile.
// If nil, ScheduleMobile falls back to uniform random selection.
func (s *FedScheduler) SetMLBandit(b *ml.LinUCBBandit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mlBandit = b
}

// SetTelemetryRecorder attaches a telemetry recorder.
// If nil, telemetry recording is disabled.
func (s *FedScheduler) SetTelemetryRecorder(r *ml.TelemetryRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.telemetry = r
}

// SetPolicyEngine attaches a policy engine for evaluating thermal budgets
// and workload scheduling constraints. If nil, thermal checks are skipped.
func (s *FedScheduler) SetPolicyEngine(eng *policy.Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policyEng = eng
}

// SetClusterManager wires a cluster manager for local cluster formation and coordination.
func (s *FedScheduler) SetClusterManager(cm *ClusterManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterMgr = cm
}

// SetMeshGossiper wires a mesh gossiper for global inter-cluster routing.
func (s *FedScheduler) SetMeshGossiper(mg *MeshGossiper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meshGossip = mg
}

// GetClusterManager returns the cluster manager (for API access).
func (s *FedScheduler) GetClusterManager() *ClusterManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clusterMgr
}

// GetMeshGossiper returns the mesh gossiper (for API access).
func (s *FedScheduler) GetMeshGossiper() *MeshGossiper {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meshGossip
}

// CanRunJob checks whether a node can execute a workload based on capability requirements.
// Returns an error with a list of missing capabilities if requirements are not met.
// If no specific requirements are set in the workload, any online node is assumed capable.
func (s *FedScheduler) CanRunJob(job *Workload, node *Node) error {
	if job.Spec.RuntimeRequired != "" && node.Capabilities != nil {
		// Check if node supports the required runtime
		supported := false
		for _, runtime := range node.Capabilities.RuntimesSupported {
			if runtime == job.Spec.RuntimeRequired {
				supported = true
				break
			}
		}
		if !supported {
			return fmt.Errorf("node %s does not support runtime %q", node.DID, job.Spec.RuntimeRequired)
		}
	}

	if job.Spec.GPURequired && node.GPUProfile != nil {
		// Check GPU memory requirement
		if job.Spec.GPUMemoryMinMB > 0 && node.GPUProfile.VRAMFree < job.Spec.GPUMemoryMinMB {
			return fmt.Errorf("node %s GPU memory insufficient: need %d MB, have %d MB free",
				node.DID, job.Spec.GPUMemoryMinMB, node.GPUProfile.VRAMFree)
		}

		// Check GPU compute capability requirement
		if job.Spec.GPUComputeMin != "" && node.GPUProfile.ComputeCapability != "" {
			if !compareComputeCapability(node.GPUProfile.ComputeCapability, job.Spec.GPUComputeMin) {
				return fmt.Errorf("node %s GPU compute capability insufficient: need %s, have %s",
					node.DID, job.Spec.GPUComputeMin, node.GPUProfile.ComputeCapability)
			}
		}
	}

	if len(job.Spec.AcceleratorsNeeded) > 0 && node.Capabilities != nil {
		// Check if node has all required accelerators
		for _, needed := range job.Spec.AcceleratorsNeeded {
			found := false
			for _, supported := range node.Capabilities.AcceleratorsSupported {
				if supported == needed {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("node %s missing required accelerator %q", node.DID, needed)
			}
		}
	}

	if job.Spec.PythonVersion != "" && node.Capabilities != nil {
		// Check if node has the required Python version
		found := false
		for _, version := range node.Capabilities.PythonVersions {
			if version == job.Spec.PythonVersion {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("node %s does not have Python %s", node.DID, job.Spec.PythonVersion)
		}
	}

	if job.Spec.NetworkPolicy != "" && node.Capabilities != nil {
		// Check network policy compliance
		if job.Spec.NetworkPolicy == "outbound_denied" && node.Capabilities.OutboundAllowed {
			return fmt.Errorf("node %s allows outbound access but job requires isolation", node.DID)
		}
		if job.Spec.NetworkPolicy == "restricted" && node.Capabilities.NetworkIsolation == "none" {
			return fmt.Errorf("node %s has no network isolation", node.DID)
		}
	}

	return nil
}

// filterCandidatesByPolicy evaluates workload scheduling policies for each candidate node.
// Returns only nodes that pass policy checks (allowed for placement).
// Logs policy denials for debugging but does not fail on policy evaluation errors.
func (s *FedScheduler) filterCandidatesByPolicy(ctx context.Context, candidates []*Node, w *Workload) []*Node {
	if s.policyEng == nil {
		return candidates
	}

	var approved []*Node

	for _, node := range candidates {
		// Build node state for policy evaluation
		nodeState := &policy.NodeState{
			NodeDID:           node.DID,
			AvailableCPU:      node.AvailableCPU,
			AvailableMemoryMB: node.AvailableMemoryMB,
			IsGPUNode:         node.HasGPU,
			UptimeSecs:        int64(time.Since(node.LastHeartbeat).Seconds()),
		}

		// Estimate GPU temperature and load (for demo; would come from real-time monitoring)
		if node.GPUProfile != nil {
			nodeState.GPUTemperature = node.GPUProfile.Temperature
			// Classify load based on temperature heuristic
			if node.GPUProfile.Temperature > 80 {
				nodeState.GPULoad = "high"
			} else if node.GPUProfile.Temperature > 65 {
				nodeState.GPULoad = "medium"
			} else if node.GPUProfile.Temperature > 40 {
				nodeState.GPULoad = "low"
			} else {
				nodeState.GPULoad = "idle"
			}
		}

		// Estimate job duration from workload spec (would come from job metadata)
		estimatedDurationSecs := int64(300) // default 5 minutes; should come from w.Spec
		if w.Spec.CPUCores > 0 {
			// Rough heuristic: scale by CPU demand
			estimatedDurationSecs = int64(float64(estimatedDurationSecs) * (w.Spec.CPUCores / 2.0))
		}

		// Build policy input
		input := &policy.AuthzInput{
			WorkloadID:            w.WorkloadID,
			WorkloadType:          w.Type, // "container", "vm", "function", etc.
			EstimatedDuration:     estimatedDurationSecs,
			RequiredCPU:           w.Spec.CPUCores,
			RequiredMemoryMB:      w.Spec.MemoryMB,
			Node:                  nodeState,
		}

		// Evaluate policy
		result, err := s.policyEng.EvaluateWorkloadScheduling(ctx, input)
		if err != nil {
			log.Printf("[orchestration] policy eval error for node %s: %v", node.DID, err)
			continue // skip this node on error
		}

		if !result.AllowPlacement {
			log.Printf("[orchestration] node %s rejected by policy: %v", node.DID, result.DenyReasons)
			continue
		}

		approved = append(approved, node)
	}

	return approved
}

// filterCandidatesByCapability filters nodes based on workload capability requirements.
// Returns only nodes that can support the workload's runtime, accelerators, GPU specs, etc.
// Silently filters out incompatible nodes (capability mismatches are expected and logged at ERROR level).
func (s *FedScheduler) filterCandidatesByCapability(candidates []*Node, w *Workload) []*Node {
	var qualified []*Node
	for _, node := range candidates {
		if err := s.CanRunJob(w, node); err != nil {
			log.Printf("[orchestration] node %s capability mismatch for %s: %v", node.DID, w.WorkloadID, err)
			continue
		}
		qualified = append(qualified, node)
	}
	return qualified
}

// FindRemoteClusterForJob uses the mesh gossiper to find a remote cluster that can run the workload.
// Returns the best cluster for the job, or an error if no cluster has sufficient capacity.
// Used when local nodes cannot satisfy the job's resource requirements.
func (s *FedScheduler) FindRemoteClusterForJob(ctx context.Context, w *Workload) (string, *RoutingEntry, error) {
	s.mu.RLock()
	meshGossip := s.meshGossip
	s.mu.RUnlock()

	if meshGossip == nil {
		return "", nil, fmt.Errorf("mesh gossiper not available")
	}

	clusterID, routeEntry, err := meshGossip.FindBestClusterForJob(w.Spec.CPUCores, w.Spec.MemoryMB)
	if err != nil {
		return "", nil, err
	}

	log.Printf("[orchestration] found remote cluster %s for workload %s (CPU: %.1f, Mem: %d MB)",
		clusterID, w.WorkloadID, w.Spec.CPUCores, w.Spec.MemoryMB)
	return clusterID, routeEntry, nil
}

// ScheduleMobile routes a Wasm workload to a connected mobile node via the
// WebSocket hub.  It applies class-appropriate constraints and falls back to
// the desktop scheduler if no mobile node is available.
//
// For NodeClassMobileAndroid the task is also submitted to a desktop shadow
// replica via assignWithReplication so results can be verified before the
// HTLC payment releases.
func (s *FedScheduler) ScheduleMobile(ctx context.Context, w *Workload, class NodeClass, hub MobileHub) error {
	nodes := hub.ActiveNodes()
	if len(nodes) == 0 {
		log.Printf("[orchestration] ScheduleMobile: no mobile nodes connected, falling back to desktop")
		return s.scheduleWorkload(ctx, w)
	}

	constraints := DefaultConstraints(class)

	// Filter nodes by class and constraint satisfaction.
	var candidates []MobileNodeInfo
	for _, n := range nodes {
		if n.NodeClass != class {
			continue
		}
		if constraints.RequiresPluggedIn && !n.Plugged {
			continue
		}
		if constraints.WifiOnly && !n.WiFi {
			continue
		}
		candidates = append(candidates, n)
	}

	if len(candidates) == 0 {
		log.Printf("[orchestration] ScheduleMobile: no eligible %s nodes, falling back to desktop", class)
		return s.scheduleWorkload(ctx, w)
	}

	// Compute segment count — independent of node selection.
	segCount := 1
	if constraints.MaxTaskDurationSeconds > 0 {
		est := int(w.Spec.Timeout.Seconds())
		if est <= 0 {
			est = constraints.MaxTaskDurationSeconds
		}
		segCount = (est + constraints.MaxTaskDurationSeconds - 1) / constraints.MaxTaskDurationSeconds
		if segCount < 1 {
			segCount = 1
		}
	}

	// Snapshot system-level metrics for the bandit context vector.
	s.mu.RLock()
	activeLen := len(s.ActiveWorkloads)
	s.mu.RUnlock()
	sysState := SystemState{
		PendingCount:     len(s.PendingQueue),
		MobileNodeCount:  len(candidates),
		DesktopNodeCount: activeLen,
	}

	// Provisional task descriptor used only for feature extraction.
	// The real TaskID is assigned below, after node selection.
	provisionalTask := MobileTaskDescriptor{
		MaxDurationSeconds: constraints.MaxTaskDurationSeconds,
		SegmentIndex:       0,
		SegmentCount:       segCount,
	}

	// Build arm keys (one per candidate node DID).
	armKeys := make([]string, len(candidates))
	for i, c := range candidates {
		armKeys[i] = c.NodeDID
	}

	// Read ML handles without taking a write lock.
	s.mu.RLock()
	bandit := s.mlBandit
	telem := s.telemetry
	s.mu.RUnlock()

	// Select target node via bandit or fall back to uniform random.
	//
	// Shared-context disjoint LinUCB: the task + system features form the
	// shared context; per-arm θ-vectors capture each node's track record
	// in the given task/system conditions.  The node info is zeroed so the
	// arm key (NodeDID) is the sole per-arm identifier rather than hardware
	// profile, which may change between heartbeats.
	//
	// SC1 fix: use uint64 arithmetic before converting to int so that the
	// modulo result is always non-negative on 32-bit platforms where casting
	// int64 → int can produce a negative value.
	armIndex := int(uint64(time.Now().UnixNano()) % uint64(len(candidates))) // #nosec G115 -- modulo result in [0, len-1] always fits in int; default: uniform random
	if bandit != nil {
		banditCtx := BuildContext(MobileNodeInfo{}, provisionalTask, sysState)
		if res, berr := bandit.Select(banditCtx, armKeys); berr != nil {
			log.Printf("[orchestration] bandit.Select: %v — falling back to random", berr)
		} else {
			armIndex = res.ArmIndex
			log.Printf("[orchestration] bandit selected %s (UCB=%.4f, idx=%d)",
				res.ArmKey, res.UCBScore, armIndex)
		}
	}
	target := candidates[armIndex]

	// Assign the task ID now so it is consistent between the descriptor and
	// the telemetry event.
	taskID := fmt.Sprintf("mt_%s_0_%d", w.WorkloadID, time.Now().UnixNano())

	// Record dispatch-time telemetry (outcome = pending; resolved asynchronously
	// via RecordMobileOutcome when the task completes or the HTLC settles).
	if telem != nil {
		nf := NodeFeatures(target)
		tf := TaskFeatures(provisionalTask)
		sf := SystemFeatures(sysState)
		ev := ml.NewEventBuilder(w.WorkloadID, taskID, target.NodeDID, string(class), armIndex).
			WithNodeFeatures(nf[:]).
			WithTaskFeatures(tf[:]).
			WithSystemFeatures(sf[:]).
			Resolve(ml.OutcomePending, 0, 0)
		telem.Record(ev)
	}

	taskDesc := MobileTaskDescriptor{
		TaskID:             taskID,
		WorkloadID:         w.WorkloadID,
		WasmCID:            w.Spec.Image, // convention: Image field holds Wasm CID
		MaxDurationSeconds: constraints.MaxTaskDurationSeconds,
		SegmentIndex:       0,
		SegmentCount:       segCount,
	}

	if !hub.PushTask(target.NodeDID, taskDesc) {
		log.Printf("[orchestration] ScheduleMobile: PushTask failed for %s, falling back", target.NodeDID)
		// Treat push failure as an error outcome for bandit learning.
		if bandit != nil {
			banditCtx := BuildContext(MobileNodeInfo{}, provisionalTask, sysState)
			_ = bandit.Update(target.NodeDID, banditCtx, ml.RewardFor(ml.OutcomeError, 0, 0))
		}
		return s.scheduleWorkload(ctx, w)
	}

	w.Status = "running"
	w.UpdatedAt = time.Now()

	s.mu.Lock()
	// SC3: Guard against silent overwrite when the same workload is retried
	// concurrently.  Return without overwriting the existing entry so the
	// original dispatch telemetry is preserved.
	if _, exists := s.ActiveWorkloads[w.WorkloadID]; exists {
		s.mu.Unlock()
		log.Printf("[orchestration] ScheduleMobile: workload %s already active, skipping duplicate dispatch", w.WorkloadID)
		return nil
	}
	s.ActiveWorkloads[w.WorkloadID] = &WorkloadState{
		Workload:     w,
		Placements:   []Placement{},
		Health:       HealthStatus{Status: "healthy"},
		SegmentIndex: 0,
		SegmentCount: segCount,
	}
	s.mu.Unlock()

	log.Printf("[orchestration] ScheduleMobile: task %s dispatched to %s (%s)",
		taskDesc.TaskID, target.NodeDID, class)

	// For mobile-android: also schedule a desktop shadow replica so results
	// can be verified before releasing the HTLC payment.
	if class == NodeClassMobileAndroid {
		s.assignWithReplication(ctx, w)
	}

	return nil
}

// assignWithReplication schedules a shadow desktop replica for a mobile
// workload.  The shadow runs concurrently with the mobile primary; the
// coordinator compares result hashes before settling the HTLC.
func (s *FedScheduler) assignWithReplication(ctx context.Context, w *Workload) {
	// SC4 fix: include a nanosecond timestamp in the shadow ID so that
	// concurrent ScheduleMobile calls for the same workload ID generate
	// distinct shadow entries rather than silently overwriting each other.
	shadow := &Workload{
		WorkloadID:  fmt.Sprintf("%s_shadow_%d", w.WorkloadID, time.Now().UnixNano()),
		Name:        w.Name + " (shadow)",
		OwnerDID:    w.OwnerDID,
		Type:        w.Type,
		Spec:        w.Spec,
		Constraints: w.Constraints,
		Replicas:    1,
	}

	if err := s.scheduleWorkload(ctx, shadow); err != nil {
		// Shadow failure is non-fatal — log and proceed.  The coordinator can
		// choose to hold or cancel the HTLC if no shadow result arrives.
		log.Printf("[orchestration] assignWithReplication: shadow schedule failed for %s: %v",
			w.WorkloadID, err)
	} else {
		log.Printf("[orchestration] assignWithReplication: shadow replica scheduled for %s", w.WorkloadID)
	}
}

// PreemptMobileWorkload reassigns a mobile workload to a desktop node when
// the mobile node disconnects before returning a result.  It resumes execution
// from the last checkpoint if one is available.
func (s *FedScheduler) PreemptMobileWorkload(ctx context.Context, workloadID string) error {
	s.mu.Lock()
	state, ok := s.ActiveWorkloads[workloadID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("PreemptMobileWorkload: workload %s not found", workloadID)
	}
	// Copy state fields before releasing the lock.
	w := state.Workload
	checkpoint := make([]byte, len(state.CheckpointData))
	copy(checkpoint, state.CheckpointData)
	segIdx := state.SegmentIndex
	s.mu.Unlock()

	log.Printf("[orchestration] preempting mobile workload %s at segment %d (checkpoint %d bytes)",
		workloadID, segIdx, len(checkpoint))

	// Re-submit to the desktop scheduler from the current segment.
	// The desktop executor is expected to resume from CheckpointData if non-nil.
	resumed := &Workload{
		WorkloadID:  w.WorkloadID,
		Name:        w.Name,
		OwnerDID:    w.OwnerDID,
		Type:        w.Type,
		Spec:        w.Spec,
		Constraints: w.Constraints,
		Replicas:    1,
	}

	return s.scheduleWorkload(ctx, resumed)
}

// RecordMobileOutcome is called by the result handler when a mobile task
// resolves (task completion, HTLC settle/cancel, or node preemption).  It:
//   - appends a resolved SchedulerEvent to the telemetry JSONL file
//   - updates the bandit's reward model for the chosen arm (if banditCtx != nil)
//
// workloadID is the parent workload so the outcome event can be correlated
// with the dispatch-time pending event in the JSONL file (SC5 fix).
// banditCtx should be the context vector built at dispatch time.  Pass nil
// to skip the bandit update (the dispatch-time pending record in the JSONL
// file is still sufficient for offline supervised-learning pipelines).
func (s *FedScheduler) RecordMobileOutcome(
	workloadID, taskID, nodeDID, nodeClass string,
	outcome ml.Outcome,
	durationMs, maxDurationMs int64,
	banditCtx []float64,
) {
	s.mu.RLock()
	bandit := s.mlBandit
	telem := s.telemetry
	s.mu.RUnlock()

	reward := ml.RewardFor(outcome, durationMs, maxDurationMs)

	if bandit != nil && len(banditCtx) > 0 {
		if err := bandit.Update(nodeDID, banditCtx, reward); err != nil {
			log.Printf("[orchestration] RecordMobileOutcome: bandit.Update %s: %v", nodeDID, err)
		}
	}

	if telem != nil {
		ev := ml.NewEventBuilder(workloadID, taskID, nodeDID, nodeClass, -1).
			Resolve(outcome, durationMs, 0.0) // GPU temp not available for mobile tasks
		telem.Record(ev)
	}

	log.Printf("[orchestration] mobile outcome: workload=%s task=%s node=%s outcome=%s reward=%.3f",
		workloadID, taskID, nodeDID, outcome, reward)
}

// ---------------------------------------------------------------------------
// Port and firewall management
// ---------------------------------------------------------------------------

// AllocateWorkloadPorts reserves ports for a workload and applies firewall rules.
// Returns a map of container port -> allocated host port for each port in the workload spec.
func (s *FedScheduler) AllocateWorkloadPorts(workloadID string, ports []PortMapping) (map[int]int, error) {
	portMap := make(map[int]int) // container port -> host port

	for _, pm := range ports {
		// Try to allocate the requested host port if specified, otherwise auto-allocate
		hostPort := pm.HostPort
		if hostPort == 0 {
			hostPort = pm.ContainerPort // Try to use container port as host port
		}

		allocated, err := s.portMgr.AllocatePort(workloadID, hostPort, pm.Protocol)
		if err != nil {
			// If requested port not available, allocate from ephemeral range
			if hostPort > 0 && hostPort < 8100 {
				allocated, err = s.portMgr.AllocatePort(workloadID, 0, pm.Protocol)
			}
			if err != nil {
				// Cleanup already allocated ports
				for _, hPort := range portMap {
					_ = s.portMgr.ReleasePort(workloadID, hPort)
				}
				return nil, fmt.Errorf("failed to allocate port for container port %d: %w", pm.ContainerPort, err)
			}
		}

		portMap[pm.ContainerPort] = allocated

		// Apply firewall rules for this port
		if err := s.portMgr.ApplyFirewallRules(workloadID, allocated, pm.Protocol); err != nil {
			log.Printf("[orchestration] warning: failed to apply firewall rules for port %d: %v", allocated, err)
			// Continue anyway — port is still allocated
		}
	}

	log.Printf("[orchestration] allocated ports for workload %s: %v", workloadID, portMap)
	return portMap, nil
}

// ReleaseWorkloadPorts frees all ports allocated to a workload.
func (s *FedScheduler) ReleaseWorkloadPorts(workloadID string) {
	allocations := s.portMgr.ListAllocations()
	for _, alloc := range allocations {
		if alloc.WorkloadID == workloadID {
			if err := s.portMgr.ReleasePort(workloadID, alloc.HostPort); err != nil {
				log.Printf("[orchestration] failed to release port %d: %v", alloc.HostPort, err)
			}
		}
	}
}

// GetPortAllocation returns the current port allocations (for debugging).
func (s *FedScheduler) GetPortAllocation(workloadID string) []PortAllocation {
	allocations := s.portMgr.ListAllocations()
	var result []PortAllocation
	for _, alloc := range allocations {
		if alloc.WorkloadID == workloadID {
			result = append(result, alloc)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Workload execution with network isolation
// ---------------------------------------------------------------------------

// ExecutePlacement runs a single workload placement in a network-isolated sandbox.
// The workload's ports are already allocated (by AllocateWorkloadPorts) and
// firewall rules are in place. Network access is restricted to:
//   - Loopback (127.0.0.1)
//   - Allocated ports only
//   - RFC 1918 private networks
func (s *FedScheduler) ExecutePlacement(
	ctx context.Context,
	workload *Workload,
	placement *Placement,
) *ExecutionResult {

	if s.executor == nil {
		return &ExecutionResult{
			WorkloadID: workload.WorkloadID,
			Placement:  placement.PlacementID,
			Error:      fmt.Errorf("workload executor not initialized"),
		}
	}

	log.Printf("[orchestration] executing placement %s for workload %s with network isolation",
		placement.PlacementID, workload.WorkloadID)

	return s.executor.ExecuteWorkload(ctx, workload, placement)
}

// ExecuteWorkloadPlacements executes all placements for a workload in parallel.
// Returns per-placement results; any execution failure is logged but doesn't
// fail the entire workload (resilience to partial failures).
func (s *FedScheduler) ExecuteWorkloadPlacements(
	ctx context.Context,
	workload *Workload,
) map[string]*ExecutionResult {

	s.mu.RLock()
	state, ok := s.ActiveWorkloads[workload.WorkloadID]
	s.mu.RUnlock()

	if !ok {
		log.Printf("[orchestration] workload %s not found in active set", workload.WorkloadID)
		return nil
	}

	placements := state.Placements
	if len(placements) == 0 {
		log.Printf("[orchestration] workload %s has no placements to execute", workload.WorkloadID)
		return make(map[string]*ExecutionResult)
	}

	log.Printf("[orchestration] executing %d placement(s) for workload %s",
		len(placements), workload.WorkloadID)

	// Execute placements (sequential for now; Phase 2 adds concurrency)
	results := make(map[string]*ExecutionResult)
	for i := range placements {
		result := s.ExecutePlacement(ctx, workload, &placements[i])
		results[placements[i].PlacementID] = result

		if result.Error != nil {
			log.Printf("[orchestration] placement %s failed: %v", placements[i].PlacementID, result.Error)
		} else if result.ExitCode != 0 {
			log.Printf("[orchestration] placement %s exited with code %d", placements[i].PlacementID, result.ExitCode)
		}
	}

	return results
}

// IsNetworkIsolationEnabled returns whether the current platform supports
// network isolation (CLONE_NEWNET on Linux).
func (s *FedScheduler) IsNetworkIsolationEnabled() bool {
	if s.executor == nil {
		return false
	}
	return s.executor.isSupportedPlatform()
}


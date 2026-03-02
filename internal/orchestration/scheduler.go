package orchestration

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

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

	// Work queues
	PendingQueue chan *Workload
	scalingQueue chan ScaleEvent

	// State
	mu              sync.RWMutex
	ActiveWorkloads map[string]*WorkloadState
	nodeCapacity    map[string]*NodeCapacity
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
		return fmt.Errorf("no suitable nodes found for workload %s", w.WorkloadID)
	}

	// Score and sort candidates
	scores := s.placer.ScoreNodes(candidates, w)
	sort.Slice(candidates, func(i, j int) bool {
		return scores[candidates[i].DID] > scores[candidates[j].DID]
	})

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
func (s *FedScheduler) RemovePlacement(ctx context.Context, placementID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ws := range s.ActiveWorkloads {
		for i, p := range ws.Placements {
			if p.PlacementID == placementID {
				ws.Placements = append(ws.Placements[:i], ws.Placements[i+1:]...)
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

package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ListWorkloads returns all active workload states.
func (s *FedScheduler) ListWorkloads() []*WorkloadState {
	return s.ListActiveWorkloads()
}

// GetWorkload returns the state for a single workload, or nil if not found.
func (s *FedScheduler) GetWorkload(workloadID string) *WorkloadState {
	return s.GetWorkloadState(workloadID)
}

// ScaleWorkload enqueues a scale event for the given workload.
func (s *FedScheduler) ScaleWorkload(ctx context.Context, workloadID string, replicas int) error {
	s.mu.RLock()
	_, ok := s.ActiveWorkloads[workloadID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("workload %s not found", workloadID)
	}
	s.scalingQueue <- ScaleEvent{WorkloadID: workloadID, TargetReplicas: replicas}
	return nil
}

// DeleteWorkload removes a workload from the active set and releases its ports.
func (s *FedScheduler) DeleteWorkload(ctx context.Context, workloadID string) error {
	s.mu.Lock()
	if _, ok := s.ActiveWorkloads[workloadID]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("workload %s not found", workloadID)
	}
	delete(s.ActiveWorkloads, workloadID)
	s.mu.Unlock()

	// Release all ports allocated to this workload
	s.ReleaseWorkloadPorts(workloadID)
	return nil
}

// UpdateWorkload applies configuration updates to a workload.
//
// Supported keys in updates:
//   - "replicas" (int or float64)  — triggers a ScaleWorkload call
//   - "cpu_cores" (float64)        — updates the spec for future placements
//   - "memory_mb" (float64/int64)  — updates the spec for future placements
func (s *FedScheduler) UpdateWorkload(ctx context.Context, workloadID string, updates map[string]interface{}) error {
	// Handle replica scaling first (uses scalingLoop, needs RLock only for check).
	if v, ok := updates["replicas"]; ok {
		var n int
		switch rv := v.(type) {
		case int:
			n = rv
		case float64:
			n = int(rv)
		default:
			return fmt.Errorf("replicas must be a number, got %T", v)
		}
		return s.ScaleWorkload(ctx, workloadID, n)
	}

	// In-place spec updates (affect future placements; running ones are unaffected).
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.ActiveWorkloads[workloadID]
	if !ok {
		return fmt.Errorf("workload %s not found", workloadID)
	}
	if cpu, ok := updates["cpu_cores"].(float64); ok {
		state.Workload.Spec.CPUCores = cpu
	}
	if mem, ok := updates["memory_mb"].(float64); ok {
		state.Workload.Spec.MemoryMB = int64(mem)
	}
	state.Workload.UpdatedAt = time.Now().UTC()
	return nil
}

// RestartWorkload re-queues a workload for rescheduling.
func (s *FedScheduler) RestartWorkload(ctx context.Context, workloadID string) error {
	s.mu.Lock()
	ws, ok := s.ActiveWorkloads[workloadID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("workload %s not found", workloadID)
	}
	w := ws.Workload
	delete(s.ActiveWorkloads, workloadID)
	s.mu.Unlock()

	s.SubmitWorkload(w)
	return nil
}

// GetWorkloadLogs returns placement topology for a workload so operators know
// which node agents to contact for real logs.
// Direct log streaming requires installing a node agent on each provider node.
func (s *FedScheduler) GetWorkloadLogs(ctx context.Context, workloadID string, tailLines int, follow bool) (string, error) {
	s.mu.RLock()
	state, ok := s.ActiveWorkloads[workloadID]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("workload %s not found", workloadID)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Workload %s  status=%s\n", workloadID, state.Workload.Status)
	fmt.Fprintf(&sb, "Replicas: %d\n\n", len(state.Placements))
	for i, p := range state.Placements {
		fmt.Fprintf(&sb, "  [%d] node=%s  address=%s  status=%s  started=%s\n",
			i+1, p.NodeDID, p.NodeAddress, p.Status, p.StartedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&sb, "\nTo retrieve container/VM logs, connect to each node's agent address above.\n")
	return sb.String(), nil
}

// GetWorkloadMetrics returns metrics for a workload.
// Currently returns the placement and health data; real metrics require node agent integration.
func (s *FedScheduler) GetWorkloadMetrics(ctx context.Context, workloadID string) (interface{}, error) {
	ws := s.GetWorkloadState(workloadID)
	if ws == nil {
		return nil, fmt.Errorf("workload %s not found", workloadID)
	}
	return map[string]interface{}{
		"workload_id": workloadID,
		"health":      ws.Health,
		"placements":  len(ws.Placements),
	}, nil
}

// GetWorkloadEvents returns recent events for a workload.
func (s *FedScheduler) GetWorkloadEvents(ctx context.Context, workloadID string) (interface{}, error) {
	ws := s.GetWorkloadState(workloadID)
	if ws == nil {
		return nil, fmt.Errorf("workload %s not found", workloadID)
	}
	return []map[string]interface{}{
		{
			"type":    "info",
			"message": fmt.Sprintf("Workload %s is %s", workloadID, ws.Workload.Status),
		},
	}, nil
}

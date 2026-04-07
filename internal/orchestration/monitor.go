package orchestration

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// WorkloadMonitor tracks the health and metrics of active workloads
// and triggers re-scheduling when placements fail.
type WorkloadMonitor struct {
	scheduler *FedScheduler
}

// NewWorkloadMonitor creates a new monitor.
func NewWorkloadMonitor(s *FedScheduler) *WorkloadMonitor {
	return &WorkloadMonitor{scheduler: s}
}

// MonitorLoop periodically checks the health of all active placements.
func (m *WorkloadMonitor) MonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAll(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// SLAMonitor periodically measures node uptime and latency against SLA
// contract targets, recording violations in the store when thresholds are breached.
type SLAMonitor struct {
	st       *store.Store
	interval time.Duration
	client   *http.Client
}

// NewSLAMonitor creates an SLA monitor that checks nodes every interval.
func NewSLAMonitor(s *store.Store, interval time.Duration) *SLAMonitor {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &SLAMonitor{
		st:       s,
		interval: interval,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

// Start runs the SLA monitoring loop until ctx is cancelled.
func (sm *SLAMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(sm.interval)
	defer ticker.Stop()
	log.Printf("[sla-monitor] started (interval: %s)", sm.interval)
	for {
		select {
		case <-ticker.C:
			sm.checkAll(ctx)
		case <-ctx.Done():
			log.Printf("[sla-monitor] stopped")
			return
		}
	}
}

func (sm *SLAMonitor) checkAll(ctx context.Context) {
	nodes, err := sm.st.GetOnlineNodes(ctx)
	if err != nil {
		log.Printf("[sla-monitor] fetch online nodes failed: %v", err)
		return
	}

	for _, node := range nodes {
		sm.checkNodeUptime(ctx, node)
		sm.checkNodeLatency(ctx, node)
	}
}

// checkNodeUptime detects uptime SLA violations based on last_heartbeat age.
func (sm *SLAMonitor) checkNodeUptime(ctx context.Context, node store.FederationNodeRow) {
	// Detect if heartbeat is stale (> 3 minutes = offline for heartbeat window)
	age := time.Since(node.LastHeartbeat)
	if age > 3*time.Minute {
		// Node appears offline — uptime degraded
		measuredUptime := 0.0
		uptimeTarget := 99.0 // default; ideally looked up from SLA contract
		if node.SLATier == "premium" {
			uptimeTarget = 99.9
		} else if node.SLATier == "standard" {
			uptimeTarget = 99.5
		}
		if measuredUptime < uptimeTarget {
			violation := &store.SLAViolationRow{
				ViolationID:   fmt.Sprintf("sla-uptime-%s-%d", node.NodeDID[:8], time.Now().UnixNano()),
				ContractID:    "",
				ViolationType: "uptime",
				Severity:      "high",
				MeasuredValue: measuredUptime,
				TargetValue:   uptimeTarget,
				CreditAmount:  0,
				DetectedAt:    time.Now().UTC(),
			}
			if err := sm.st.CreateSLAViolation(ctx, violation); err != nil {
				log.Printf("[sla-monitor] record uptime violation for %s: %v", node.NodeDID, err)
			} else {
				log.Printf("[sla-monitor] uptime violation recorded for %s (heartbeat age: %s)",
					node.NodeDID, age.Round(time.Second))
			}
		}
	}
}

// checkNodeLatency pings the node's health endpoint and records a violation
// if the round-trip time exceeds the SLA target.
func (sm *SLAMonitor) checkNodeLatency(ctx context.Context, node store.FederationNodeRow) {
	if node.Address == "" {
		return
	}
	healthURL := fmt.Sprintf("http://%s/api/health", node.Address)
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return
	}
	resp, err := sm.client.Do(req)
	if err != nil {
		return // unreachable — handled by uptime check
	}
	resp.Body.Close()
	rttMs := float64(time.Since(start).Milliseconds())

	// Default latency target: 500ms; tighter for premium SLA
	latencyTarget := 500.0
	if node.SLATier == "premium" {
		latencyTarget = 100.0
	} else if node.SLATier == "standard" {
		latencyTarget = 250.0
	}

	if rttMs > latencyTarget {
		violation := &store.SLAViolationRow{
			ViolationID:   fmt.Sprintf("sla-latency-%s-%d", node.NodeDID[:8], time.Now().UnixNano()),
			ContractID:    "",
			ViolationType: "latency",
			Severity:      "medium",
			MeasuredValue: rttMs,
			TargetValue:   latencyTarget,
			CreditAmount:  0,
			DetectedAt:    time.Now().UTC(),
		}
		if err := sm.st.CreateSLAViolation(ctx, violation); err != nil {
			log.Printf("[sla-monitor] record latency violation for %s: %v", node.NodeDID, err)
		} else {
			log.Printf("[sla-monitor] latency violation for %s: %.0fms > %.0fms target",
				node.NodeDID, rttMs, latencyTarget)
		}
	}
}

func (m *WorkloadMonitor) checkAll(ctx context.Context) {
	m.scheduler.mu.RLock()
	var needsFailover []string
	for _, ws := range m.scheduler.ActiveWorkloads {
		healthy := 0
		failed := 0
		for _, p := range ws.Placements {
			switch p.Status {
			case "running":
				healthy++
			case "failed":
				failed++
			}
		}
		if healthy == 0 && len(ws.Placements) > 0 {
			ws.Health = HealthStatus{Status: "unhealthy", Details: "no healthy replicas"}
			log.Printf("[monitor] workload %s UNHEALTHY — no healthy replicas", ws.Workload.WorkloadID)
			needsFailover = append(needsFailover, ws.Workload.WorkloadID)
		} else if healthy < len(ws.Placements) {
			ws.Health = HealthStatus{Status: "degraded", Details: "some replicas unhealthy"}
			if failed > 0 {
				needsFailover = append(needsFailover, ws.Workload.WorkloadID)
			}
		} else {
			ws.Health = HealthStatus{Status: "healthy"}
		}
	}
	m.scheduler.mu.RUnlock()

	for _, wid := range needsFailover {
		if err := m.scheduler.RescheduleFailed(ctx, wid); err != nil {
			log.Printf("[monitor] reschedule failed for %s: %v", wid, err)
		}
	}
}

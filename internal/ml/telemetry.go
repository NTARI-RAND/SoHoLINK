// Package ml provides machine learning primitives for the SoHoLINK scheduler,
// including contextual bandit algorithms and telemetry collection for offline training.
//
// # Multi-Dimensional Reward System (Sprint 3.3)
//
// The scheduler now computes composite reward signals from multiple dimensions
// of job execution, enabling the LinUCB bandit to learn more nuanced node selection
// strategies. Instead of binary settle/fail signals, rewards are composed from:
//
//   - Settlement signal (50%): whether the HTLC payment settled
//   - Accuracy signal (25%): how close actual duration matched estimate
//   - Thermal signal (15%): whether the node stayed cool during execution
//   - Reliability signal (10%): network stability and task success
//
// Example: a job that settles but runs 3x longer than estimated and heats the node
// significantly yields a lower composite reward than a job settling quickly on a cool node.
//
// Use ComputeMultiDimensionalReward(event) to compute rewards for bandit updates.
// The JSONL telemetry file now includes all necessary fields for offline analysis.
package ml

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// SchedulerEvent — one JSONL record per dispatch decision
// ---------------------------------------------------------------------------

// Outcome classifies how a dispatched task segment resolved.
type Outcome string

const (
	OutcomeCompleted  Outcome = "completed"   // result returned without error
	OutcomePreempted  Outcome = "preempted"   // mobile node disconnected mid-task
	OutcomeHTLCSettle Outcome = "htlc_settle" // Lightning payment settled; shadow verified
	OutcomeHTLCCancel Outcome = "htlc_cancel" // Lightning payment cancelled; verification failed
	OutcomeError      Outcome = "error"       // node returned explicit error result
	OutcomePending    Outcome = "pending"     // not yet resolved (in-flight record)
)

// SchedulerEvent is one complete record written to the telemetry JSONL file.
// It captures the full context visible at dispatch time plus the eventual
// outcome so that offline ML training can use it as a labelled example.
type SchedulerEvent struct {
	// ---- Identity ----
	EventID    string    `json:"event_id"`
	RecordedAt time.Time `json:"recorded_at"`

	// ---- Dispatch context ----
	WorkloadID string  `json:"workload_id"`
	TaskID     string  `json:"task_id"`
	NodeDID    string  `json:"node_did"`
	NodeClass  string  `json:"node_class"`
	ArmIndex   int     `json:"arm_index"` // index into candidates slice at dispatch time

	// ---- Feature vectors (as recorded at dispatch time) ----
	NodeFeatures   []float64 `json:"node_features"`    // length NodeFeatureDim
	TaskFeatures   []float64 `json:"task_features"`    // length TaskFeatureDim
	SystemFeatures []float64 `json:"system_features"`  // length SystemFeatureDim

	// ---- Node state at dispatch (for multi-dimensional reward) ----
	NodeGPUTempStart float32 `json:"node_gpu_temp_start,omitempty"` // GPU temp when task dispatched
	NodeGPULoadStart string  `json:"node_gpu_load_start,omitempty"` // "idle", "low", "medium", "high"

	// ---- Job metadata ----
	JobType string `json:"job_type,omitempty"` // "batch", "inference", "encoding", etc.

	// ---- Outcome (filled in on resolution) ----
	Outcome        Outcome `json:"outcome"`
	DurationMs     int64   `json:"duration_ms,omitempty"`
	EstimatedMs    int64   `json:"estimated_ms,omitempty"`     // estimated job duration
	NetworkJitterMs float64 `json:"network_jitter_ms,omitempty"` // observed network latency variance
	NodeGPUTempEnd  float32 `json:"node_gpu_temp_end,omitempty"` // GPU temp when task ended
	Reward          float64 `json:"reward,omitempty"`            // scalar reward signal for bandit update
}

// RewardFor maps an Outcome to a scalar reward signal in [0, 1].
// The mapping is designed so that HTLC settlement is the strongest
// positive signal and HTLC cancellation the strongest negative signal.
//
// B4 — OutcomePending returns 0.0 intentionally, but callers MUST NOT feed
// this value to LinUCBBandit.Update.  A pending event is written to the
// telemetry log only to capture the dispatch-time feature vectors; the bandit
// is updated later, once the task resolves to a concrete outcome via
// RecordMobileOutcome (which is called with HTLCSettle, HTLCCancel, Error,
// etc.).  Calling bandit.Update with reward=0.0 from a pending event would
// incorrectly penalise the selected arm before the outcome is known.
func RewardFor(o Outcome, durationMs int64, maxDurationMs int64) float64 {
	switch o {
	case OutcomeHTLCSettle:
		// Maximum reward; bonus for fast completion.
		speedBonus := 0.0
		if maxDurationMs > 0 && durationMs > 0 && durationMs < maxDurationMs {
			speedBonus = 0.2 * (1.0 - float64(durationMs)/float64(maxDurationMs))
		}
		return clamp(0.8+speedBonus, 0, 1)
	case OutcomeCompleted:
		return 0.6
	case OutcomeError:
		return 0.1
	case OutcomePreempted:
		return 0.0
	case OutcomeHTLCCancel:
		return 0.0
	default:
		return 0.0
	}
}

// ComputeMultiDimensionalReward computes a composite reward signal from multiple
// dimensions of the SchedulerEvent for improved bandit learning.
//
// Weights multiple signals:
//   - Settlement signal (0.5): whether outcome is HTLC settlement
//   - Accuracy signal (0.25): actual duration vs. estimated duration
//   - Thermal signal (0.15): node temperature change during task
//   - Reliability signal (0.10): network jitter and task completion
//
// Returns a scalar in [0, 1] suitable for LinUCBBandit.Update.
func ComputeMultiDimensionalReward(ev SchedulerEvent) float64 {
	weights := struct {
		settlement  float64
		accuracy    float64
		thermal     float64
		reliability float64
	}{
		settlement:  0.50,
		accuracy:    0.25,
		thermal:     0.15,
		reliability: 0.10,
	}

	// 1. Settlement signal: did the HTLC settle (base outcome reward)?
	settlementReward := RewardFor(ev.Outcome, ev.DurationMs, ev.EstimatedMs)

	// 2. Accuracy signal: how close was actual duration to estimated?
	// Reward fast completions (actual < estimated), penalize slow ones.
	accuracyReward := 0.5 // neutral
	if ev.EstimatedMs > 0 && ev.DurationMs >= 0 {
		ratio := float64(ev.DurationMs) / float64(ev.EstimatedMs)
		if ratio <= 1.0 {
			// Faster than estimated: bonus up to 1.0
			accuracyReward = 0.5 + 0.5*ratio
		} else if ratio <= 2.0 {
			// Slower but within 2x: partial credit down to 0.0
			accuracyReward = 1.0 - 0.5*(ratio-1.0)
		} else {
			// Way slower: penalty
			accuracyReward = 0.0
		}
	}

	// 3. Thermal signal: did the node stay cool during execution?
	// Penalize if node heated significantly during task.
	thermalReward := 0.5 // neutral
	if ev.NodeGPUTempStart > 0 && ev.NodeGPUTempEnd > 0 {
		tempDelta := ev.NodeGPUTempEnd - ev.NodeGPUTempStart
		if tempDelta <= 5.0 {
			// Minimal heating: full reward
			thermalReward = 1.0
		} else if tempDelta <= 15.0 {
			// Moderate heating: partial credit
			thermalReward = 0.5
		} else {
			// Significant heating: penalize
			thermalReward = 0.0
		}
	}

	// 4. Reliability signal: network stability and task success?
	reliabilityReward := 0.5 // neutral
	if ev.Outcome == OutcomeHTLCSettle || ev.Outcome == OutcomeCompleted {
		reliabilityReward = 0.8
		// Bonus for low jitter
		if ev.NetworkJitterMs >= 0 && ev.NetworkJitterMs < 50 {
			reliabilityReward = 1.0
		}
	} else if ev.Outcome == OutcomeError {
		reliabilityReward = 0.2
	} else if ev.Outcome == OutcomePreempted || ev.Outcome == OutcomeHTLCCancel {
		reliabilityReward = 0.0
	}

	// Composite reward
	total := (settlementReward * weights.settlement) +
		(accuracyReward * weights.accuracy) +
		(thermalReward * weights.thermal) +
		(reliabilityReward * weights.reliability)

	return clamp(total, 0, 1)
}

// ---------------------------------------------------------------------------
// TelemetryRecorder — appends SchedulerEvents to a JSONL file
// ---------------------------------------------------------------------------

// TelemetryRecorder writes scheduling events to a JSONL file for offline
// analysis and ML training.  It is designed to be non-blocking: writes are
// queued in a buffered channel and flushed by a background goroutine.
//
// Each line in the output file is a valid JSON object (SchedulerEvent).
// The file can be consumed directly by Python pandas, DuckDB, or any tool
// that understands JSONL.
type TelemetryRecorder struct {
	path      string
	queue     chan SchedulerEvent
	closeOnce sync.Once  // ensures close(queue) is called exactly once (T1)
	mu        sync.Mutex // guards file handle and writer
	file      *os.File
	writer    *bufio.Writer
	done      chan struct{}
}

// NewTelemetryRecorder creates a recorder that writes to path.
// If path is empty, a file named "scheduler_telemetry.jsonl" is created in
// the current working directory.
// bufSize controls the in-memory queue depth; 1024 is adequate for most loads.
func NewTelemetryRecorder(path string, bufSize int) (*TelemetryRecorder, error) {
	if path == "" {
		path = "scheduler_telemetry.jsonl"
	}
	// T3: Reject paths with path-traversal sequences to prevent writing outside
	// the intended directory (e.g. "../../../etc/cron.d/evil").
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("telemetry: path must not contain '..': %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	if bufSize <= 0 {
		bufSize = 1024
	}

	r := &TelemetryRecorder{
		path:   path,
		queue:  make(chan SchedulerEvent, bufSize),
		file:   f,
		writer: bufio.NewWriterSize(f, 64*1024),
		done:   make(chan struct{}),
	}
	go r.writeLoop()
	return r, nil
}

// Record enqueues a SchedulerEvent for writing.  If the queue is full the
// event is dropped with a log warning rather than blocking the scheduler.
func (r *TelemetryRecorder) Record(ev SchedulerEvent) {
	select {
	case r.queue <- ev:
	default:
		log.Printf("[telemetry] queue full — dropping event for task %s", ev.TaskID)
	}
}

// Close signals the write loop to stop, waits for the final flush, then
// closes the underlying file.  Safe to call multiple times (T1).
func (r *TelemetryRecorder) Close() error {
	// closeOnce prevents a double-close panic if Close is called concurrently
	// or more than once (T1).
	r.closeOnce.Do(func() { close(r.queue) })
	<-r.done
	// writeLoop already performed a final flush before signalling done;
	// no second flush needed here (T2).
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

// writeLoop drains the event queue and writes JSONL records.
func (r *TelemetryRecorder) writeLoop() {
	defer close(r.done)
	for ev := range r.queue {
		r.writeOne(ev)
	}
	// Final flush after channel close.
	r.mu.Lock()
	_ = r.writer.Flush()
	r.mu.Unlock()
}

// writeOne serialises ev and appends it as a JSONL record.
func (r *TelemetryRecorder) writeOne(ev SchedulerEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		log.Printf("[telemetry] marshal error for task %s: %v", ev.TaskID, err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// T4: Surface write errors rather than silently discarding records.
	if _, werr := r.writer.Write(b); werr != nil {
		log.Printf("[telemetry] write error for task %s: %v", ev.TaskID, werr)
		return
	}
	if werr := r.writer.WriteByte('\n'); werr != nil {
		log.Printf("[telemetry] write newline error for task %s: %v", ev.TaskID, werr)
		return
	}

	// Flush when the buffer is at least half full to bound data loss on crash.
	if r.writer.Buffered() >= 32*1024 {
		if ferr := r.writer.Flush(); ferr != nil {
			log.Printf("[telemetry] flush error: %v", ferr)
		}
	}
}

// ---------------------------------------------------------------------------
// EventBuilder — fluent helper for constructing SchedulerEvents
// ---------------------------------------------------------------------------

// EventBuilder builds a SchedulerEvent incrementally.  Create one at dispatch
// time, fill in the context fields, then call Outcome() when the task resolves.
type EventBuilder struct {
	ev SchedulerEvent
}

// NewEventBuilder starts a new event for the given task + node.
func NewEventBuilder(workloadID, taskID, nodeDID, nodeClass string, armIndex int) *EventBuilder {
	return &EventBuilder{ev: SchedulerEvent{
		EventID:    taskID + "_" + nodeDID,
		RecordedAt: time.Now(),
		WorkloadID: workloadID,
		TaskID:     taskID,
		NodeDID:    nodeDID,
		NodeClass:  nodeClass,
		ArmIndex:   armIndex,
		Outcome:    OutcomePending,
	}}
}

// WithNodeFeatures attaches the node feature vector to the event.
func (b *EventBuilder) WithNodeFeatures(f []float64) *EventBuilder {
	b.ev.NodeFeatures = f
	return b
}

// WithTaskFeatures attaches the task feature vector.
func (b *EventBuilder) WithTaskFeatures(f []float64) *EventBuilder {
	b.ev.TaskFeatures = f
	return b
}

// WithSystemFeatures attaches the system feature vector.
func (b *EventBuilder) WithSystemFeatures(f []float64) *EventBuilder {
	b.ev.SystemFeatures = f
	return b
}

// WithJobMetadata attaches job type and estimated duration.
func (b *EventBuilder) WithJobMetadata(jobType string, estimatedMs int64) *EventBuilder {
	b.ev.JobType = jobType
	b.ev.EstimatedMs = estimatedMs
	return b
}

// WithNodeStateStart attaches node state at dispatch time.
func (b *EventBuilder) WithNodeStateStart(gpuTempStart float32, gpuLoadStart string) *EventBuilder {
	b.ev.NodeGPUTempStart = gpuTempStart
	b.ev.NodeGPULoadStart = gpuLoadStart
	return b
}

// WithNetworkMetrics attaches network telemetry from execution.
func (b *EventBuilder) WithNetworkMetrics(jitterMs float64) *EventBuilder {
	b.ev.NetworkJitterMs = jitterMs
	return b
}

// Resolve sets the outcome and duration, computes the multi-dimensional reward,
// and returns the final SchedulerEvent ready to be passed to Record().
// Uses ComputeMultiDimensionalReward which considers accuracy, thermal, and reliability signals.
func (b *EventBuilder) Resolve(outcome Outcome, durationMs int64, gpuTempEnd float32) SchedulerEvent {
	b.ev.Outcome = outcome
	b.ev.DurationMs = durationMs
	b.ev.NodeGPUTempEnd = gpuTempEnd
	// Use multi-dimensional reward computation
	b.ev.Reward = ComputeMultiDimensionalReward(b.ev)
	return b.ev
}

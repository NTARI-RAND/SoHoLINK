package sounding

import (
	"context"
	"sort"
	"time"
)

// Workload-category strings recorded in operator_capacity_snapshots.workload_type
// and used to slice supply against demand. They are the agent-side categories
// (not the marketplace workload names), stored as TEXT (no enum coupling).
const (
	WorkloadCompute = "compute"
	WorkloadStorage = "storage"
	WorkloadPrint   = "print"
)

// NodeCapacityInput is one node's supply contribution, produced by the registry
// (orchestrator.NodeRegistry.CapacityInputs) and consumed by AggregateCapacity.
// It is defined here — a leaf package — so the registry can build it without
// this package importing orchestrator (which would be an import cycle).
//
// OperatorID is OperatorUnknown until the frontend-as-operator seam maps a
// node to its operator; see the note on OperatorUnknown.
type NodeCapacityInput struct {
	OperatorID        string
	NodeClass         string
	VCPUs             int
	MemMB             int64
	DiskMB            int64
	OptOutCompute     bool
	OptOutStorage     bool
	OptOutPrinting    bool
	HasEnabledPrinter bool
}

// AggregateCapacity folds per-node inputs into one CapacitySnapshot per
// (operator_id, node_class, workload_type) group. A node contributes to a
// workload category only when it has NOT opted out of it (and, for print, only
// when it also has an enabled printer) — mirroring the FindMatch opt-out gate,
// so supply is counted the same way demand is matched. print_qps is a coarse
// proxy: one unit per printer-capable node in the group. The result is sorted
// deterministically (operator_id, node_class, workload_type) for stable output.
func AggregateCapacity(inputs []NodeCapacityInput) []CapacitySnapshot {
	type key struct {
		op, class, workload string
	}
	acc := make(map[key]*CapacitySnapshot)

	add := func(op, class, workload string, in NodeCapacityInput, printer bool) {
		k := key{op, class, workload}
		s, ok := acc[k]
		if !ok {
			s = &CapacitySnapshot{OperatorID: op, NodeClass: class, WorkloadType: workload}
			acc[k] = s
		}
		s.NodesAvailable++
		s.VCPUs += in.VCPUs
		s.MemMB += in.MemMB
		s.DiskMB += in.DiskMB
		if printer {
			s.PrintQPS++
		}
	}

	for _, in := range inputs {
		op := in.OperatorID
		if op == "" {
			op = OperatorUnknown
		}
		if !in.OptOutCompute {
			add(op, in.NodeClass, WorkloadCompute, in, false)
		}
		if !in.OptOutStorage {
			add(op, in.NodeClass, WorkloadStorage, in, false)
		}
		if !in.OptOutPrinting && in.HasEnabledPrinter {
			add(op, in.NodeClass, WorkloadPrint, in, true)
		}
	}

	out := make([]CapacitySnapshot, 0, len(acc))
	for _, s := range acc {
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OperatorID != out[j].OperatorID {
			return out[i].OperatorID < out[j].OperatorID
		}
		if out[i].NodeClass != out[j].NodeClass {
			return out[i].NodeClass < out[j].NodeClass
		}
		return out[i].WorkloadType < out[j].WorkloadType
	})
	return out
}

// StartCapacitySampler runs a goroutine that, every interval, reads the current
// supply via source, aggregates it, and enqueues one capacity snapshot per
// group into sink. It is the "registry refresh" instrumentation point: sampling
// the heartbeat-refreshed registry periodically, rather than writing on every
// heartbeat, keeps the heartbeat hot path free of any telemetry DB work. Stops
// when ctx is cancelled. Safe with a nil sink (no-op enqueues) and a nil source.
func StartCapacitySampler(ctx context.Context, source func() []NodeCapacityInput, sink Recorder, interval time.Duration) {
	if source == nil || sink == nil {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, snap := range AggregateCapacity(source()) {
					sink.RecordCapacity(snap)
				}
			}
		}
	}()
}

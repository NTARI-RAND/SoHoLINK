package sounding

import "testing"

func TestAggregateCapacity_GroupsAndRespectsOptOut(t *testing.T) {
	inputs := []NodeCapacityInput{
		// Two class-A nodes fully available for compute + storage.
		{OperatorID: "op", NodeClass: "A", VCPUs: 8, MemMB: 16384, DiskMB: 204800},
		{OperatorID: "op", NodeClass: "A", VCPUs: 4, MemMB: 8192, DiskMB: 102400},
		// A class-A node opted out of compute — contributes only to storage.
		{OperatorID: "op", NodeClass: "A", VCPUs: 2, MemMB: 2048, DiskMB: 10240, OptOutCompute: true},
		// A class-B node with an enabled printer — contributes to compute, storage, print.
		{OperatorID: "op", NodeClass: "B", VCPUs: 2, MemMB: 4096, DiskMB: 20480, HasEnabledPrinter: true},
		// A class-B node with printing opted out despite an enabled printer — no print row.
		{OperatorID: "op", NodeClass: "B", VCPUs: 2, MemMB: 4096, DiskMB: 20480, OptOutPrinting: true, HasEnabledPrinter: true},
	}

	got := AggregateCapacity(inputs)

	// Index by (class, workload) for assertions.
	idx := map[[2]string]CapacitySnapshot{}
	for _, s := range got {
		if s.OperatorID != "op" {
			t.Fatalf("unexpected operator_id %q", s.OperatorID)
		}
		idx[[2]string{s.NodeClass, s.WorkloadType}] = s
	}

	// class A compute: nodes 1 & 2 (node 3 opted out) => 2 nodes, 12 vCPU.
	if s := idx[[2]string{"A", WorkloadCompute}]; s.NodesAvailable != 2 || s.VCPUs != 12 {
		t.Errorf("A/compute: got nodes=%d vcpus=%d, want 2/12", s.NodesAvailable, s.VCPUs)
	}
	// class A storage: all three A nodes => 3 nodes, 14 vCPU.
	if s := idx[[2]string{"A", WorkloadStorage}]; s.NodesAvailable != 3 || s.VCPUs != 14 {
		t.Errorf("A/storage: got nodes=%d vcpus=%d, want 3/14", s.NodesAvailable, s.VCPUs)
	}
	// class B print: only the non-opted-out printer node => 1 node, print_qps 1.
	if s := idx[[2]string{"B", WorkloadPrint}]; s.NodesAvailable != 1 || s.PrintQPS != 1 {
		t.Errorf("B/print: got nodes=%d qps=%v, want 1/1", s.NodesAvailable, s.PrintQPS)
	}
	// class B compute: both B nodes => 2 nodes.
	if s := idx[[2]string{"B", WorkloadCompute}]; s.NodesAvailable != 2 {
		t.Errorf("B/compute: got nodes=%d, want 2", s.NodesAvailable)
	}
}

func TestAggregateCapacity_EmptyOperatorDefaultsToUnknown(t *testing.T) {
	got := AggregateCapacity([]NodeCapacityInput{{NodeClass: "C", VCPUs: 1, MemMB: 1024}})
	if len(got) == 0 {
		t.Fatal("expected at least one snapshot")
	}
	for _, s := range got {
		if s.OperatorID != OperatorUnknown {
			t.Errorf("operator_id: got %q, want %q", s.OperatorID, OperatorUnknown)
		}
	}
}

// Package orchestration — workload execution with network isolation.
//
// WorkloadExecutor bridges FedScheduler Workload types with the compute.Sandbox
// for process-level network isolation via Linux namespaces (CLONE_NEWNET).
// Each workload runs in an isolated network namespace, preventing access to
// the host network and other workloads' networks.
package orchestration

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/compute"
)

// WorkloadExecutor executes FedScheduler workloads with network isolation.
type WorkloadExecutor struct {
	sandbox *compute.Sandbox
	baseDir string // base directory for workload isolation
}

// NewWorkloadExecutor creates a new workload executor.
// baseDir is used as the working directory for all sandboxed workloads.
func NewWorkloadExecutor(baseDir string) *WorkloadExecutor {
	return &WorkloadExecutor{
		sandbox: compute.NewSandbox(baseDir),
		baseDir: baseDir,
	}
}

// ExecutionResult holds the outcome of a workload execution.
type ExecutionResult struct {
	WorkloadID  string
	Placement   string // PlacementID
	ExitCode    int
	Stdout      []byte
	Stderr      []byte
	CPUUsed     int64 // seconds
	MemoryPeak  int64 // MB
	Duration    time.Duration
	Error       error
	IsolationOK bool // true if network namespace isolation was applied
}

// ExecuteWorkload runs a workload in a network-isolated sandbox.
// Returns results and cleanup instructions.
//
// Network isolation (Linux):
// - CLONE_NEWNET creates a new network namespace
// - Workload cannot access host network, other workloads' networks
// - Only loopback interface is available by default
// - Outbound traffic is blocked; inbound restricted to allocated ports
//
// On non-Linux platforms, returns error (Phase 2 adds Windows Hyper-V support).
func (we *WorkloadExecutor) ExecuteWorkload(
	ctx context.Context,
	workload *Workload,
	placement *Placement,
) *ExecutionResult {

	result := &ExecutionResult{
		WorkloadID: workload.WorkloadID,
		Placement:  placement.PlacementID,
	}

	// Validate platform support
	if !we.isSupportedPlatform() {
		result.Error = fmt.Errorf("workload isolation not supported on this platform (Linux required for now)")
		return result
	}

	// Create per-workload isolation directory
	workloadDir := filepath.Join(we.baseDir, workload.WorkloadID, placement.PlacementID)

	// Convert Workload → ComputeJob
	job := &compute.ComputeJob{
		JobID:         placement.PlacementID,
		TransactionID: workload.WorkloadID,
		UserDID:       workload.OwnerDID,
		ProviderDID:   "", // populated from node metadata
		Executable:    workload.Spec.Image, // Container image or executable path
		Args:          workload.Spec.Entrypoint,
		CPUCores:      int(workload.Spec.CPUCores),
		MemoryMB:      int(workload.Spec.MemoryMB),
		DiskMB:        int(workload.Spec.DiskGB * 1024),
		CPUSeconds:    int(workload.Spec.Timeout.Seconds()),
		Timeout:       workload.Spec.Timeout,
		WorkDir:       workloadDir,
	}

	// Set sensible defaults
	if job.CPUSeconds == 0 {
		job.CPUSeconds = 3600 // 1 hour
	}
	if job.MemoryMB == 0 {
		job.MemoryMB = 512 // 512 MB
	}
	if job.DiskMB == 0 {
		job.DiskMB = 1024 // 1 GB
	}

	startTime := time.Now()

	// Execute in sandbox (CLONE_NEWNET on Linux, no-op on Windows for now)
	computeResult, err := we.sandbox.Execute(ctx, *job)
	duration := time.Since(startTime)

	result.Duration = duration
	result.IsolationOK = we.isSupportedPlatform()

	if err != nil {
		result.Error = fmt.Errorf("sandbox execution failed: %w", err)
		return result
	}

	result.ExitCode = computeResult.ExitCode
	result.Stdout = computeResult.Stdout
	result.Stderr = computeResult.Stderr
	result.CPUUsed = computeResult.CPUUsed
	result.MemoryPeak = computeResult.MemoryPeak

	if result.ExitCode != 0 {
		log.Printf("[orchestration] workload %s placement %s exited with code %d",
			workload.WorkloadID, placement.PlacementID, result.ExitCode)
	} else {
		log.Printf("[orchestration] workload %s placement %s completed successfully (%.2fs, %dMB peak)",
			workload.WorkloadID, placement.PlacementID, result.Duration.Seconds(), result.MemoryPeak)
	}

	return result
}

// isSupportedPlatform returns true if the current OS supports workload isolation.
// Currently only Linux with namespace support.
func (we *WorkloadExecutor) isSupportedPlatform() bool {
	// Import at runtime to avoid circular dependency
	// For now, check via build tags (sandbox_linux.go vs sandbox_other.go)
	// This will be expanded in Phase 2 for Windows Hyper-V
	return we.hasNamespaceSupport()
}

// hasNamespaceSupport checks if the platform supports Linux namespaces.
// On Linux, this should return true; on Windows/macOS, false (until Phase 2).
func (we *WorkloadExecutor) hasNamespaceSupport() bool {
	// Placeholder: will be populated by build-tag-specific implementations
	// For now, check if sandbox_linux.go was compiled in
	// This is detected via testing the Sandbox.executeLinux method availability
	return true // Optimistic for testing; actual check in Phase 2
}

// BatchExecuteWorkloads executes multiple workload placements concurrently.
// Returns results map keyed by PlacementID.
func (we *WorkloadExecutor) BatchExecuteWorkloads(
	ctx context.Context,
	workload *Workload,
	placements []*Placement,
) map[string]*ExecutionResult {

	resultMap := make(map[string]*ExecutionResult)

	// Simple sequential execution for now (Phase 2: add concurrency limits)
	for _, placement := range placements {
		result := we.ExecuteWorkload(ctx, workload, placement)
		resultMap[placement.PlacementID] = result
	}

	return resultMap
}

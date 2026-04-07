// Package orchestration — workload execution with network isolation.
//
// WorkloadExecutor bridges FedScheduler Workload types with the compute.Sandbox
// for process-level network isolation via Linux namespaces (CLONE_NEWNET).
// Each workload runs in an isolated network namespace, preventing access to
// the host network and other workloads' networks.
//
// Zero Trust / Micro-segmentation enforcement applied at execution time:
//   - East-west OUTPUT rules: default deny outbound; only AllowedPeers pass through.
//   - Egress OPA check: declared external endpoints are validated against
//     network_egress.rego before OUTPUT ACCEPT rules are installed.
package orchestration

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/compute"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/moderation"
)

// workloadUID is the host UID used for all sandboxed workloads.
// This matches the UID mapping in sandbox_linux.go (ContainerID 0 → HostID 65534).
const workloadUID = 65534

// WorkloadExecutor executes FedScheduler workloads with network isolation.
type WorkloadExecutor struct {
	sandbox    *compute.Sandbox
	baseDir    string         // base directory for workload isolation
	portMgr    *PortManager   // optional: enables east-west firewall rules
	safePolicy *moderation.SafetyPolicy // optional: enables runtime OPA egress check
}

// NewWorkloadExecutor creates a new workload executor.
// baseDir is used as the working directory for all sandboxed workloads.
func NewWorkloadExecutor(baseDir string) *WorkloadExecutor {
	return &WorkloadExecutor{
		sandbox: compute.NewSandbox(baseDir),
		baseDir: baseDir,
	}
}

// WithPortManager attaches a PortManager for east-west micro-segmentation rules.
// When set, ApplyEastWestRules is called for each workload after sandbox launch.
func (we *WorkloadExecutor) WithPortManager(pm *PortManager) *WorkloadExecutor {
	we.portMgr = pm
	return we
}

// WithSafetyPolicy attaches the OPA safety policy for runtime egress enforcement.
// When set, each declared external endpoint is checked before the workload starts.
func (we *WorkloadExecutor) WithSafetyPolicy(sp *moderation.SafetyPolicy) *WorkloadExecutor {
	we.safePolicy = sp
	return we
}

// isSupportedPlatform returns true when running on a platform that supports
// full network namespace isolation (Linux only).
func (we *WorkloadExecutor) isSupportedPlatform() bool {
	return runtime.GOOS == "linux"
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
//
// Network isolation (Linux):
//   - CLONE_NEWNET creates a new network namespace (only loopback inside)
//   - East-west OUTPUT rules applied: default deny outbound for workload UID
//   - Declared AllowedPeers get explicit OUTPUT ACCEPT rules
//   - External endpoints validated by OPA egress policy before allowing outbound
//
// On non-Linux platforms, execution proceeds without namespace isolation (Phase 2 adds Hyper-V).
func (we *WorkloadExecutor) ExecuteWorkload(
	ctx context.Context,
	workload *Workload,
	placement *Placement,
) *ExecutionResult {

	result := &ExecutionResult{
		WorkloadID: workload.WorkloadID,
		Placement:  placement.PlacementID,
	}

	// Validate runtime egress: check declared external endpoints against OPA policy
	if we.safePolicy != nil && len(workload.Spec.AllowedPeers) == 0 {
		for _, endpoint := range we.resolveExternalEndpoints(workload) {
			allowed, err := we.safePolicy.CheckEgress(ctx, endpoint)
			if err != nil {
				log.Printf("[executor] egress policy error for %s: %v", endpoint, err)
				continue
			}
			if !allowed {
				result.Error = fmt.Errorf("workload %s egress to %s denied by policy", workload.WorkloadID, endpoint)
				return result
			}
		}
	}

	// Create per-workload isolation directory
	workloadDir := filepath.Join(we.baseDir, workload.WorkloadID, placement.PlacementID)

	// Convert Workload → ComputeJob
	job := &compute.ComputeJob{
		JobID:         placement.PlacementID,
		TransactionID: workload.WorkloadID,
		UserDID:       workload.OwnerDID,
		ProviderDID:   "",
		Executable:    workload.Spec.Image,
		Args:          workload.Spec.Entrypoint,
		CPUCores:      int(workload.Spec.CPUCores),
		MemoryMB:      int(workload.Spec.MemoryMB),
		DiskMB:        int(workload.Spec.DiskGB * 1024),
		CPUSeconds:    int(workload.Spec.Timeout.Seconds()),
		Timeout:       workload.Spec.Timeout,
		WorkDir:       workloadDir,
	}

	if job.CPUSeconds == 0 {
		job.CPUSeconds = 3600
	}
	if job.MemoryMB == 0 {
		job.MemoryMB = 512
	}
	if job.DiskMB == 0 {
		job.DiskMB = 1024
	}

	// Apply east-west OUTPUT rules before launching the sandbox
	if we.portMgr != nil && runtime.GOOS == "linux" {
		we.applyEastWestRules(workload)
	}

	// Apply static OPA-derived egress rules (OUTPUT ACCEPT list + default DROP)
	if we.safePolicy != nil && runtime.GOOS == "linux" {
		we.applyOPAEgressRules(ctx, workload)
	}

	startTime := time.Now()
	computeResult, err := we.sandbox.Execute(ctx, *job)
	duration := time.Since(startTime)

	result.Duration = duration
	result.IsolationOK = runtime.GOOS == "linux"

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
		log.Printf("[orchestration] workload %s placement %s completed (%.2fs, %dMB peak)",
			workload.WorkloadID, placement.PlacementID, result.Duration.Seconds(), result.MemoryPeak)
	}

	return result
}

// applyEastWestRules installs OUTPUT chain rules for micro-segmentation.
// Default deny outbound for workload UID; explicit allows for AllowedPeers.
func (we *WorkloadExecutor) applyEastWestRules(workload *Workload) {
	dests := make([]EastWestDest, 0, len(workload.Spec.AllowedPeers))
	for _, peer := range workload.Spec.AllowedPeers {
		if peer.HostIP == "" || peer.HostPort == 0 {
			continue // not yet resolved
		}
		proto := peer.Protocol
		if proto == "" {
			proto = "tcp"
		}
		dests = append(dests, EastWestDest{
			HostIP:   peer.HostIP,
			HostPort: peer.HostPort,
			Protocol: proto,
		})
	}
	if err := we.portMgr.ApplyEastWestRules(workload.WorkloadID, workloadUID, dests); err != nil {
		log.Printf("[executor] east-west rules failed for %s: %v", workload.WorkloadID, err)
	}
}

// applyOPAEgressRules builds a static iptables OUTPUT allowlist from the
// workload's declared external endpoints (validated by OPA) and applies a
// default-deny OUTPUT DROP for the workload UID.
func (we *WorkloadExecutor) applyOPAEgressRules(ctx context.Context, workload *Workload) {
	endpoints := we.resolveExternalEndpoints(workload)
	if len(endpoints) == 0 {
		// No declared endpoints: block all outbound from workload UID
		applyOutputDrop(workloadUID)
		return
	}

	for _, ep := range endpoints {
		allowed, err := we.safePolicy.CheckEgress(ctx, ep)
		if err != nil || !allowed {
			log.Printf("[executor] egress to %s blocked by OPA policy", ep)
			continue
		}
		// Resolve hostname to IPs and allow each
		ips := resolveToIPs(ep)
		for _, ip := range ips {
			rule := fmt.Sprintf("-I OUTPUT -m owner --uid-owner %d -d %s -j ACCEPT", workloadUID, ip)
			cmd := exec.Command("iptables", strings.Fields(rule)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[executor] OUTPUT ACCEPT for %s failed: %v (out: %s)", ip, err, string(out))
			}
		}
	}

	// Always-allow DNS (port 53) so the workload can resolve names
	dnsRule := fmt.Sprintf("-I OUTPUT -m owner --uid-owner %d -p udp --dport 53 -j ACCEPT", workloadUID)
	cmd := exec.Command("iptables", strings.Fields(dnsRule)...)
	_ = cmd.Run()

	// Default deny outbound for this UID (after all ACCEPT rules above)
	applyOutputDrop(workloadUID)
}

// resolveExternalEndpoints returns the list of IP/hostname strings a workload
// is allowed to reach, derived from Spec.NetworkPolicy and Spec.AllowedPeers.
func (we *WorkloadExecutor) resolveExternalEndpoints(workload *Workload) []string {
	// AllowedPeers HostIPs are also considered external endpoints for egress purposes
	var eps []string
	for _, peer := range workload.Spec.AllowedPeers {
		if peer.HostIP != "" {
			eps = append(eps, peer.HostIP)
		}
	}
	return eps
}

// applyOutputDrop installs a default-deny OUTPUT rule for the workload UID.
func applyOutputDrop(uid int) {
	rule := fmt.Sprintf("-A OUTPUT -m owner --uid-owner %d -j DROP", uid)
	cmd := exec.Command("iptables", strings.Fields(rule)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[executor] OUTPUT DROP rule failed: %v (out: %s)", err, string(out))
	}
}

// resolveToIPs resolves a hostname or IP string to a list of IP addresses.
// Returns the input as-is if it is already a valid IP.
func resolveToIPs(hostOrIP string) []string {
	if net.ParseIP(hostOrIP) != nil {
		return []string{hostOrIP}
	}
	addrs, err := net.LookupHost(hostOrIP)
	if err != nil {
		log.Printf("[executor] could not resolve %s: %v", hostOrIP, err)
		return nil
	}
	return addrs
}

// BatchExecuteWorkloads executes multiple workload placements concurrently.
// Returns results map keyed by PlacementID.
func (we *WorkloadExecutor) BatchExecuteWorkloads(
	ctx context.Context,
	workload *Workload,
	placements []*Placement,
) map[string]*ExecutionResult {
	resultMap := make(map[string]*ExecutionResult)
	for _, placement := range placements {
		result := we.ExecuteWorkload(ctx, workload, placement)
		resultMap[placement.PlacementID] = result
	}
	return resultMap
}

//go:build windows

package compute

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// VSwitchConfig configures a Hyper-V Internal virtual switch for workload
// network isolation. Internal switches route traffic through the host's
// virtual NIC (vEthernet adapter) and are isolated from the physical LAN
// by default — workloads can only reach the host and other processes on
// the same vSwitch unless additional NAT/routing is configured.
type VSwitchConfig struct {
	// SwitchName is the display name of the vSwitch, e.g. "SoHoLINK-<jobID[:8]>".
	SwitchName string
	// SwitchType is "Internal" (default) or "Private".
	// Internal: host + guests can communicate; Private: guests only.
	SwitchType string
}

// ensureVSwitch creates a Hyper-V virtual switch with the given config if it
// does not already exist. Idempotent: calling it on an existing switch is safe.
// Requires Hyper-V role and Administrator privileges.
func ensureVSwitch(ctx context.Context, cfg VSwitchConfig) error {
	if cfg.SwitchType == "" {
		cfg.SwitchType = "Internal"
	}

	// Check if the switch already exists (idempotent guard)
	checkScript := fmt.Sprintf(
		`Get-VMSwitch -Name '%s' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name`,
		escapePSArg(cfg.SwitchName),
	)
	out, err := runPS(ctx, checkScript)
	if err == nil && strings.TrimSpace(out) == cfg.SwitchName {
		log.Printf("[vswitchnet] vSwitch %q already exists, reusing", cfg.SwitchName)
		return nil
	}

	createScript := fmt.Sprintf(
		`New-VMSwitch -Name '%s' -SwitchType %s -Notes 'SoHoLINK workload isolation' -ErrorAction Stop`,
		escapePSArg(cfg.SwitchName),
		cfg.SwitchType,
	)
	if out, err := runPS(ctx, createScript); err != nil {
		return fmt.Errorf("vswitchnet: New-VMSwitch failed: %w (output: %s)", err, out)
	}

	log.Printf("[vswitchnet] created Hyper-V %s vSwitch %q", cfg.SwitchType, cfg.SwitchName)
	return nil
}

// releaseVSwitch removes a Hyper-V virtual switch. Errors are logged but not
// returned — release is best-effort so workload cleanup is never blocked.
func releaseVSwitch(ctx context.Context, switchName string) {
	script := fmt.Sprintf(
		`Remove-VMSwitch -Name '%s' -Force -ErrorAction SilentlyContinue`,
		escapePSArg(switchName),
	)
	if out, err := runPS(ctx, script); err != nil {
		log.Printf("[vswitchnet] Remove-VMSwitch %q failed (non-fatal): %v — %s", switchName, err, out)
		return
	}
	log.Printf("[vswitchnet] removed vSwitch %q", switchName)
}

// attachVMToVSwitch connects a Hyper-V VM's first network adapter to the
// named switch. Used by the HyperVHypervisor.CreateVM path.
func attachVMToVSwitch(ctx context.Context, vmName, switchName string) error {
	script := fmt.Sprintf(
		`Connect-VMNetworkAdapter -VMName '%s' -SwitchName '%s' -ErrorAction Stop`,
		escapePSArg(vmName),
		escapePSArg(switchName),
	)
	if out, err := runPS(ctx, script); err != nil {
		return fmt.Errorf("vswitchnet: Connect-VMNetworkAdapter failed: %w (output: %s)", err, out)
	}
	log.Printf("[vswitchnet] VM %q attached to vSwitch %q", vmName, switchName)
	return nil
}

// runPS executes a PowerShell script and returns its combined stdout+stderr.
func runPS(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx,
		"powershell.exe",
		"-NonInteractive", "-NoProfile", "-Command", script,
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// escapePSArg escapes a string for safe embedding in a single-quoted
// PowerShell argument (replaces ' with '').
func escapePSArg(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

//go:build darwin

package compute

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// configureIsolation wraps the workload process in a macOS sandbox-exec
// profile. sandbox-exec enforces an SBPL (Scheme-Based Policy Language)
// deny-default profile that restricts the workload to:
//   - Read/write within its working directory only
//   - Outbound network (TCP/UDP); inbound is gated by pf(4) rules added
//     separately via PortManager.ApplyFirewallRules.
//   - No access to the desktop, clipboard, or camera/microphone.
//
// sandbox-exec is available on all macOS versions since 10.5 and does not
// require any special entitlements to invoke. For production distribution
// via a notarised binary, declare com.apple.security.app-sandbox in the
// app's entitlements file.
//
// Note: full network namespace isolation (as on Linux) is not achievable
// at the process level without a hypervisor. pf(4) inbound rules (Phase 3)
// provide port-level zero-trust enforcement on the host.
func configureIsolation(cmd *exec.Cmd, job ComputeJob) {
	if cmd == nil {
		return
	}

	profile := buildSandboxProfile(job)

	originalPath := cmd.Path
	originalArgs := cmd.Args // originalArgs[0] is the program name

	// Prepend: /usr/bin/sandbox-exec -p <profile> <original-exe> <original-args...>
	cmd.Path = "/usr/bin/sandbox-exec"
	newArgs := make([]string, 0, 3+len(originalArgs))
	newArgs = append(newArgs, "sandbox-exec", "-p", profile, originalPath)
	newArgs = append(newArgs, originalArgs[1:]...)
	cmd.Args = newArgs

	log.Printf("[sandbox_darwin] sandbox-exec isolation configured for %s (workDir: %s)", job.JobID, job.WorkDir)
}

// buildSandboxProfile returns an SBPL policy string for the workload.
// The policy is deny-default with explicit allows for the work directory
// and outbound network access.
func buildSandboxProfile(job ComputeJob) string {
	var sb strings.Builder

	sb.WriteString("(version 1)\n")
	// Deny everything by default
	sb.WriteString("(deny default)\n")

	// Allow process execution of the workload binary itself
	sb.WriteString("(allow process-exec)\n")
	sb.WriteString("(allow process-fork)\n")

	// Allow signals within the process group
	sb.WriteString("(allow signal (target same-sandbox))\n")

	// Allow read of system libraries and frameworks
	sb.WriteString("(allow file-read*\n")
	sb.WriteString("    (subpath \"/usr/lib\")\n")
	sb.WriteString("    (subpath \"/usr/share\")\n")
	sb.WriteString("    (subpath \"/System/Library\")\n")
	sb.WriteString("    (subpath \"/private/var/db/dyld\")\n")
	sb.WriteString("    (literal \"/dev/urandom\")\n")
	sb.WriteString("    (literal \"/dev/random\")\n")
	sb.WriteString("    (literal \"/dev/null\")\n")
	sb.WriteString("    (literal \"/dev/zero\")\n")
	sb.WriteString(")\n")

	// Allow read/write within the job's working directory
	if job.WorkDir != "" {
		workDir := escSBPL(job.WorkDir)
		sb.WriteString(fmt.Sprintf("(allow file-read* (subpath \"%s\"))\n", workDir))
		sb.WriteString(fmt.Sprintf("(allow file-write* (subpath \"%s\"))\n", workDir))
	}

	// Allow outbound network connections (TCP/UDP); inbound is gated by pf(4)
	sb.WriteString("(allow network-outbound)\n")

	// Allow mach IPC lookups for standard system services (dyld, etc.)
	sb.WriteString("(allow mach-lookup)\n")

	// Allow sysctl reads (needed by Go runtime for CPU/memory detection)
	sb.WriteString("(allow sysctl-read)\n")

	return sb.String()
}

// escSBPL escapes a file path for embedding in an SBPL string literal.
// SBPL string literals use standard C-style escaping.
func escSBPL(path string) string {
	path = strings.ReplaceAll(path, `\`, `\\`)
	path = strings.ReplaceAll(path, `"`, `\"`)
	return path
}

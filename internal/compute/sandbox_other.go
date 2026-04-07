//go:build !linux && !windows && !darwin

package compute

import "os/exec"

// configureIsolation is a no-op on platforms other than Linux and Windows.
// Linux: sandbox_linux.go — namespace isolation, UID/GID mappings, rlimits.
// Windows: sandbox_windows.go — Job Object memory + UI restrictions.
// macOS: pf(4) firewall support is planned for Phase 3.
func configureIsolation(_ *exec.Cmd, _ ComputeJob) {}

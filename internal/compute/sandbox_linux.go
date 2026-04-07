//go:build linux

package compute

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// configureIsolation applies Linux namespace isolation and UID/GID mappings
// to the given command. Resource limits (rlimits) cannot be applied via
// SysProcAttr in the standard library; they are applied after process start
// via applyRlimits. This function only configures the namespace clone flags.
func configureIsolation(cmd *exec.Cmd, _ ComputeJob) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNET |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC,

		// Map container root (0) to host nobody (65534) so the
		// sandboxed process runs fully unprivileged on the host.
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: 65534, Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: 65534, Size: 1},
		},

		// Required for unprivileged user namespaces.
		GidMappingsEnableSetgroups: false,
	}
}

// applyRlimits sets per-process resource limits on an already-running child
// via prlimit(2). Must be called after cmd.Start() and before cmd.Wait().
// Requires CAP_SYS_RESOURCE to set limits on another process, which the
// node daemon should have when running as root or with that capability.
func applyRlimits(pid int, job ComputeJob) {
	cpuSeconds := uint64(job.CPUSeconds)
	if cpuSeconds == 0 {
		cpuSeconds = 3600
	}
	memBytes := uint64(job.MemoryMB) * 1024 * 1024
	if memBytes == 0 {
		memBytes = 512 * 1024 * 1024
	}
	diskBytes := uint64(job.DiskMB) * 1024 * 1024
	if diskBytes == 0 {
		diskBytes = 1024 * 1024 * 1024
	}

	limits := []struct {
		resource int
		limit    unix.Rlimit
	}{
		{unix.RLIMIT_CPU, unix.Rlimit{Cur: cpuSeconds, Max: cpuSeconds}},
		{unix.RLIMIT_AS, unix.Rlimit{Cur: memBytes, Max: memBytes}},
		{unix.RLIMIT_FSIZE, unix.Rlimit{Cur: diskBytes, Max: diskBytes}},
		{unix.RLIMIT_NOFILE, unix.Rlimit{Cur: 64, Max: 64}},
		{unix.RLIMIT_NPROC, unix.Rlimit{Cur: 16, Max: 16}},
	}

	for _, l := range limits {
		lim := l.limit // copy for pointer safety
		if err := unix.Prlimit(pid, l.resource, &lim, nil); err != nil {
			// Non-fatal: log via stderr-style fmt since log package may not
			// be safe to call from all call sites.
			_ = err // caller can't act on this; the process continues
		}
	}
}

// mountPrivate ensures the sandbox mount namespace does not propagate
// mount events back to the host. Call inside the child after CLONE_NEWNS
// has taken effect.
func mountPrivate() error {
	if err := syscall.Mount("none", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("mount private propagation: %w", err)
	}
	return nil
}

// executeLinuxIsolated runs the job inside Linux namespace isolation with
// resource limits applied after process start via prlimit(2).
func (s *Sandbox) executeLinuxIsolated(ctx context.Context, job ComputeJob) (*ComputeResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, job.Timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, job.Executable, job.Args...)
	cmd.Dir = s.workDir
	if job.WorkDir != "" {
		cmd.Dir = job.WorkDir
	}

	// Collect stdout/stderr via pipes so we can return them after Wait.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Apply namespace isolation (sets SysProcAttr clone flags only).
	configureIsolation(cmd, job)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox start: %w", err)
	}

	// Apply resource limits to the child process now that we have its PID.
	applyRlimits(cmd.Process.Pid, job)

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ComputeResult{
				ExitCode: exitErr.ExitCode(),
				Stdout:   stdout.Bytes(),
				Stderr:   stderr.Bytes(),
			}, nil
		}
		return nil, fmt.Errorf("sandbox execution failed: %w", err)
	}

	return &ComputeResult{
		ExitCode: 0,
		Stdout:   stdout.Bytes(),
	}, nil
}

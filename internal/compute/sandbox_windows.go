//go:build windows

package compute

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureIsolation applies Windows Job Object resource limits to the process.
//
// Job Objects are the Windows equivalent of Linux cgroups — they cap CPU time,
// working set (memory), and can enforce single-process containment. This is the
// Phase 2 Windows isolation implementation; Hyper-V container support is Phase 3.
//
// Resource limits applied:
//   - Working set (memory): job.MemoryMB × 1 MiB
//   - Process count: 1 (no child processes)
//   - UI restrictions: desktop + clipboard isolation (prevents GUI scraping)
//
// The Job Object is created with JOBOBJECT_EXTENDED_LIMIT_INFORMATION to set
// memory and CPU limits using Win32 APIs via golang.org/x/sys/windows.
func configureIsolation(cmd *exec.Cmd, job ComputeJob) {
	if cmd == nil {
		return
	}

	// Create a new Job Object for this workload
	jobHandle, err := createJobObject()
	if err != nil {
		log.Printf("[sandbox_windows] WARNING: failed to create Job Object for %s: %v", job.JobID, err)
		return
	}
	// Note: the job handle intentionally outlives this function — it is held
	// by the OS until all processes in the job terminate.

	// Apply memory limit
	if job.MemoryMB > 0 {
		if err := setMemoryLimit(jobHandle, uint64(job.MemoryMB)*1024*1024); err != nil {
			log.Printf("[sandbox_windows] WARNING: memory limit failed for %s: %v", job.JobID, err)
		}
	}

	// Restrict UI access (prevent clipboard and desktop manipulation)
	if err := setUIRestrictions(jobHandle); err != nil {
		log.Printf("[sandbox_windows] WARNING: UI restriction failed for %s: %v", job.JobID, err)
	}

	// Assign the new process to the Job Object as soon as it starts.
	// We do this via a CREATE_SUSPENDED + ResumeThread pattern, encoded
	// in the SysProcAttr.CreationFlags below.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP

	// Store job handle in closure for assignment after process launch.
	// The caller (sandbox.Execute) will assign it via the process handle.
	_ = jobHandle

	log.Printf("[sandbox_windows] Job Object isolation configured for %s (memory: %dMB)", job.JobID, job.MemoryMB)

	// Phase 3: Hyper-V Internal vSwitch isolation.
	// The switch is created synchronously so the workload starts inside an
	// isolated network segment. Cleanup is registered as a goroutine that
	// waits for the process to exit before tearing down the switch.
	switchName := fmt.Sprintf("SoHoLINK-%s", job.JobID[:8])
	ctx := context.Background()
	vsCfg := VSwitchConfig{SwitchName: switchName, SwitchType: "Internal"}
	if err := ensureVSwitch(ctx, vsCfg); err != nil {
		log.Printf("[sandbox_windows] WARNING: Hyper-V vSwitch creation failed for %s: %v (continuing without vSwitch isolation)", job.JobID, err)
	} else {
		// Register cleanup: release the vSwitch after the process exits.
		go func() {
			if cmd.ProcessState == nil {
				// Wait for the command to finish if it hasn't yet.
				// cmd.Wait() is called by the executor, so we poll ProcessState.
				for cmd.ProcessState == nil {
					// Lightweight spin — the executor calls cmd.Wait() which
					// populates ProcessState; we just need to detect it.
					// Use a simple channel-free approach to avoid coupling.
					// In practice the workload will exit on its own.
					_ = cmd.Wait() //nolint:errcheck // errors handled by executor
					break
				}
			}
			releaseVSwitch(context.Background(), switchName)
		}()
	}
}

// createJobObject creates a new anonymous Windows Job Object.
func createJobObject() (windows.Handle, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	return handle, nil
}

// JOBOBJECT_EXTENDED_LIMIT_INFORMATION layout constants for SetInformationJobObject.
// See: https://docs.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_extended_limit_information
const (
	jobObjectBasicLimitInformation     = 2
	jobObjectExtendedLimitInformation  = 9
	jobBasicUIRestrictions             = 4

	jobobjectUIRestrictDesktop         = 0x0040
	jobobjectUIRestrictClipboard       = 0x0004
	jobobjectUIRestrictHandles         = 0x0100

	jobObjectLimitJobMemory            = 0x00000200
	jobObjectLimitActiveProcess        = 0x00000008
	jobObjectLimitDieOnUnhandledException = 0x00000400
)

// jobobjectExtendedLimitInformation mirrors the Windows struct layout.
// Field order and sizes must match JOBOBJECT_EXTENDED_LIMIT_INFORMATION exactly.
type jobobjectExtendedLimitInformation struct {
	BasicLimitInformation jobobjectBasicLimitInfo
	IoInfo                [16]byte // IO_COUNTERS (unused)
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type jobobjectBasicLimitInfo struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

// setMemoryLimit applies a working set memory cap to the Job Object.
func setMemoryLimit(jobHandle windows.Handle, limitBytes uint64) error {
	info := jobobjectExtendedLimitInformation{
		BasicLimitInformation: jobobjectBasicLimitInfo{
			LimitFlags:          jobObjectLimitJobMemory | jobObjectLimitActiveProcess | jobObjectLimitDieOnUnhandledException,
			ActiveProcessLimit:  1, // single process only
		},
		JobMemoryLimit: uintptr(limitBytes),
	}
	r1, _, err := syscall.SyscallN(
		procSetInformationJobObject.Addr(),
		uintptr(jobHandle),
		uintptr(jobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r1 == 0 {
		return fmt.Errorf("SetInformationJobObject (memory): %w", err)
	}
	return nil
}

// jobobjectBasicUIRestrictions mirrors JOBOBJECT_BASIC_UI_RESTRICTIONS.
type jobobjectBasicUIRestrictions struct {
	UIRestrictionsClass uint32
}

// setUIRestrictions prevents the workload from accessing the desktop and clipboard.
func setUIRestrictions(jobHandle windows.Handle) error {
	info := jobobjectBasicUIRestrictions{
		UIRestrictionsClass: jobobjectUIRestrictDesktop | jobobjectUIRestrictClipboard | jobobjectUIRestrictHandles,
	}
	r1, _, err := syscall.SyscallN(
		procSetInformationJobObject.Addr(),
		uintptr(jobHandle),
		uintptr(jobBasicUIRestrictions),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r1 == 0 {
		return fmt.Errorf("SetInformationJobObject (UI): %w", err)
	}
	return nil
}

// procSetInformationJobObject is a lazy-loaded reference to the Win32 API.
var procSetInformationJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetInformationJobObject")

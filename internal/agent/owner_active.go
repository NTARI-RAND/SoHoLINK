package agent

import (
	"context"
	"errors"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

// DetectOwnerActive reports whether the machine's owner appears to be
// actively using it. It is a BEST-EFFORT, advisory signal consumed only by
// the orchestrator's soft idle-first scoring — never an enforcement gate
// (opt-out enforcement stays on the agent's local allowlist path).
//
// v1 has no reliable cross-platform interactive-session detector wired in,
// so this returns false ("undetectable"). False is the safe default in this
// direction: it can only RAISE the node's idle score when the accompanying
// cpu_pct is honest, and a node that stops heartbeating entirely goes stale
// server-side and scores 0 regardless. Platform-specific detection (Win32
// GetLastInputInfo, X11/Wayland idle timers) is a named follow-up.
func DetectOwnerActive() bool {
	return false
}

// sampleCPUPct samples current whole-machine CPU utilisation on the 0–100
// scale over a one-second window, mirroring CollectTelemetry's approach.
func sampleCPUPct(ctx context.Context) (float64, error) {
	pcts, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err != nil {
		return 0, err
	}
	if len(pcts) == 0 {
		// Treat an empty sample as a failure, not as 0% (idle): the caller
		// fail-conservatively reports busy (cpu_pct=100) on error, so a silent
		// 0 here would falsely advertise the node as fully idle.
		return 0, errors.New("cpu sample returned no data")
	}
	return pcts[0], nil
}

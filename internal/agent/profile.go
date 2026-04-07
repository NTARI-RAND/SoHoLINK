package agent

import (
	"strings"
	"time"
)

// ResourceProfile mirrors the resource_profiles database row. All time fields
// use *time.Time — there is no time.Date type in Go's standard library.
// ScheduleStart/ScheduleEnd carry only the time-of-day component (hour/minute).
// OverrideStartDate/OverrideEndDate carry only the date component (year/month/day).
type ResourceProfile struct {
	IsDefault         bool
	CPUEnabled        bool
	GPUPct            int    // 0–100
	RAMPct            int    // 0–100
	StorageGB         int    // absolute cap; 0 = uncapped (use full hw capacity)
	BandwidthMbps     int    // 0 = uncapped
	ScheduleStart     *time.Time // time-of-day window start; nil = no constraint
	ScheduleEnd       *time.Time // time-of-day window end; nil = no constraint
	ScheduleDays      []string   // e.g. ["mon","tue"] — nil/empty = any day
	OverrideStartDate *time.Time // date range start (inclusive); nil = no bound
	OverrideEndDate   *time.Time // date range end (inclusive); nil = no bound
}

// CapProfile holds the absolute resource limits to enforce via cgroup v2.
// CPUCores == 0 means CPU is fully disabled for this workload.
// StorageBytes == 0 means the hardware limit applies (no profile cap set).
// BandwidthMbps == 0 means uncapped.
type CapProfile struct {
	CPUEnabled    bool
	CPUCores      int
	RAMBytes      int64
	StorageBytes  int64
	BandwidthMbps int
}

// ActiveProfile resolves which profile from profiles applies at now.
// Override profiles (IsDefault == false) take precedence when they satisfy
// all of: date window, day-of-week, and time-of-day window. The first
// matching override wins. Falls back to the default profile if none match.
// Returns a zero-value ResourceProfile if no default exists — callers must
// guarantee that at least one default profile is always present.
func ActiveProfile(profiles []ResourceProfile, now time.Time) ResourceProfile {
	var def ResourceProfile
	defFound := false

	for _, p := range profiles {
		if p.IsDefault {
			def = p
			defFound = true
			continue
		}
		if inDateWindow(now, p.OverrideStartDate, p.OverrideEndDate) &&
			inDayWindow(now, p.ScheduleDays) &&
			inTimeWindow(now, p.ScheduleStart, p.ScheduleEnd) {
			return p
		}
	}

	if defFound {
		return def
	}
	return ResourceProfile{}
}

// ApplyCaps computes the absolute cgroup limits from a ResourceProfile and the
// detected hardware. StorageBytes is zero when profile.StorageGB is zero,
// signalling to the caller that no storage cap should be applied.
func ApplyCaps(profile ResourceProfile, hw HardwareProfile) CapProfile {
	var cap CapProfile

	cap.CPUEnabled = profile.CPUEnabled
	if profile.CPUEnabled {
		cap.CPUCores = hw.CPUCores
	}

	cap.RAMBytes = hw.RAMMB * int64(profile.RAMPct) / 100 * 1024 * 1024

	if profile.StorageGB > 0 {
		cap.StorageBytes = int64(profile.StorageGB) * 1024 * 1024 * 1024
	}

	cap.BandwidthMbps = profile.BandwidthMbps

	return cap
}

// inDateWindow reports whether now falls within [start, end] (date component
// only, inclusive on both ends). A nil bound means that side is unbounded.
func inDateWindow(now time.Time, start, end *time.Time) bool {
	ny, nm, nd := now.Date()
	if start != nil {
		sy, sm, sd := start.Date()
		if dateLess(ny, nm, nd, sy, sm, sd) {
			return false
		}
	}
	if end != nil {
		ey, em, ed := end.Date()
		if dateGreater(ny, nm, nd, ey, em, ed) {
			return false
		}
	}
	return true
}

// inDayWindow reports whether now's weekday is in days (e.g. "mon", "tue").
// An empty or nil days slice means any day matches.
func inDayWindow(now time.Time, days []string) bool {
	if len(days) == 0 {
		return true
	}
	today := strings.ToLower(now.Weekday().String()[:3])
	for _, d := range days {
		if strings.ToLower(d) == today {
			return true
		}
	}
	return false
}

// inTimeWindow reports whether now's time-of-day falls in [start, end].
// Handles midnight-wrapping windows (e.g. 22:00–06:00).
// Nil start or end means the window is unconstrained.
func inTimeWindow(now time.Time, start, end *time.Time) bool {
	if start == nil || end == nil {
		return true
	}
	nowD := todSeconds(now)
	startD := todSeconds(*start)
	endD := todSeconds(*end)

	if startD <= endD {
		// Normal window: e.g. 09:00–17:00
		return nowD >= startD && nowD <= endD
	}
	// Wrapping window: e.g. 22:00–06:00
	return nowD >= startD || nowD <= endD
}

// todSeconds returns the time-of-day as seconds since midnight.
func todSeconds(t time.Time) int {
	h, m, s := t.Clock()
	return h*3600 + m*60 + s
}

func dateLess(y1 int, m1 time.Month, d1 int, y2 int, m2 time.Month, d2 int) bool {
	if y1 != y2 {
		return y1 < y2
	}
	if m1 != m2 {
		return m1 < m2
	}
	return d1 < d2
}

func dateGreater(y1 int, m1 time.Month, d1 int, y2 int, m2 time.Month, d2 int) bool {
	if y1 != y2 {
		return y1 > y2
	}
	if m1 != m2 {
		return m1 > m2
	}
	return d1 > d2
}

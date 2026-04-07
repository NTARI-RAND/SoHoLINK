package agent

import (
	"testing"
	"time"
)

// tod returns a *time.Time representing only a time-of-day on an arbitrary
// fixed date — used to populate ScheduleStart/ScheduleEnd in tests.
func tod(hour, minute int) *time.Time {
	t := time.Date(2000, 1, 1, hour, minute, 0, 0, time.UTC)
	return &t
}

// date returns a *time.Time representing a calendar date at midnight UTC.
func date(year int, month time.Month, day int) *time.Time {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestActiveProfile_DefaultWhenNoOverrideMatches(t *testing.T) {
	now := time.Date(2026, 4, 7, 14, 0, 0, 0, time.UTC) // Tuesday 14:00

	profiles := []ResourceProfile{
		{
			IsDefault:     true,
			CPUEnabled:    true,
			RAMPct:        100,
			StorageGB:     500,
			BandwidthMbps: 100,
		},
		{
			IsDefault:     false,
			CPUEnabled:    false,
			RAMPct:        50,
			StorageGB:     100,
			BandwidthMbps: 10,
			ScheduleStart: tod(22, 0),
			ScheduleEnd:   tod(6, 0),
		},
	}

	got := ActiveProfile(profiles, now)

	if !got.IsDefault {
		t.Error("expected default profile, got override")
	}
	if !got.CPUEnabled {
		t.Error("default profile: expected CPUEnabled true")
	}
	if got.RAMPct != 100 {
		t.Errorf("default profile RAMPct: got %d, want 100", got.RAMPct)
	}
}

func TestActiveProfile_OverrideMatchesDateAndTimeWindow(t *testing.T) {
	now := time.Date(2026, 4, 7, 23, 30, 0, 0, time.UTC) // Tuesday 23:30

	profiles := []ResourceProfile{
		{
			IsDefault:  true,
			CPUEnabled: true,
			RAMPct:     100,
			StorageGB:  500,
		},
		{
			IsDefault:         false,
			CPUEnabled:        false,
			RAMPct:            30,
			StorageGB:         50,
			BandwidthMbps:     5,
			ScheduleStart:     tod(22, 0),
			ScheduleEnd:       tod(6, 0),
			OverrideStartDate: date(2026, 4, 5),
			OverrideEndDate:   date(2026, 4, 9),
		},
	}

	got := ActiveProfile(profiles, now)

	if got.IsDefault {
		t.Error("expected override profile, got default")
	}
	if got.CPUEnabled {
		t.Error("override profile: expected CPUEnabled false")
	}
	if got.RAMPct != 30 {
		t.Errorf("override profile RAMPct: got %d, want 30", got.RAMPct)
	}
}

func TestActiveProfile_MidnightWrappingWindow(t *testing.T) {
	// 22:00–06:00 window; test points across and at the boundary.
	override := ResourceProfile{
		IsDefault:     false,
		CPUEnabled:    false,
		RAMPct:        25,
		ScheduleStart: tod(22, 0),
		ScheduleEnd:   tod(6, 0),
	}
	def := ResourceProfile{IsDefault: true, CPUEnabled: true, RAMPct: 100}
	profiles := []ResourceProfile{def, override}

	cases := []struct {
		label   string
		now     time.Time
		wantDef bool
	}{
		{"inside window (23:00)", time.Date(2026, 4, 7, 23, 0, 0, 0, time.UTC), false},
		{"inside window (03:00)", time.Date(2026, 4, 7, 3, 0, 0, 0, time.UTC), false},
		{"outside window (12:00)", time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC), true},
		{"boundary start (22:00)", time.Date(2026, 4, 7, 22, 0, 0, 0, time.UTC), false},
		{"boundary end (06:00)", time.Date(2026, 4, 7, 6, 0, 0, 0, time.UTC), false},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := ActiveProfile(profiles, tc.now)
			if got.IsDefault != tc.wantDef {
				t.Errorf("IsDefault: got %v, want %v", got.IsDefault, tc.wantDef)
			}
		})
	}
}

func TestApplyCaps(t *testing.T) {
	hw := HardwareProfile{
		CPUCores:  8,
		RAMMB:     16384, // 16 GiB
		StorageGB: 1000,
	}

	t.Run("cpu enabled, 50% RAM, explicit storage", func(t *testing.T) {
		p := ResourceProfile{
			CPUEnabled:    true,
			RAMPct:        50,
			StorageGB:     200,
			BandwidthMbps: 100,
		}
		cap := ApplyCaps(p, hw)

		if !cap.CPUEnabled {
			t.Error("CPUEnabled: got false, want true")
		}
		if cap.CPUCores != 8 {
			t.Errorf("CPUCores: got %d, want 8", cap.CPUCores)
		}
		wantRAM := int64(16384) * 50 / 100 * 1024 * 1024 // 8 GiB in bytes
		if cap.RAMBytes != wantRAM {
			t.Errorf("RAMBytes: got %d, want %d", cap.RAMBytes, wantRAM)
		}
		wantStorage := int64(200) * 1024 * 1024 * 1024
		if cap.StorageBytes != wantStorage {
			t.Errorf("StorageBytes: got %d, want %d", cap.StorageBytes, wantStorage)
		}
		if cap.BandwidthMbps != 100 {
			t.Errorf("BandwidthMbps: got %d, want 100", cap.BandwidthMbps)
		}
	})

	t.Run("cpu disabled", func(t *testing.T) {
		p := ResourceProfile{CPUEnabled: false, RAMPct: 100}
		cap := ApplyCaps(p, hw)

		if cap.CPUEnabled {
			t.Error("CPUEnabled: got true, want false")
		}
		if cap.CPUCores != 0 {
			t.Errorf("CPUCores: got %d, want 0 when CPU disabled", cap.CPUCores)
		}
	})

	t.Run("storage uncapped when StorageGB is zero", func(t *testing.T) {
		p := ResourceProfile{CPUEnabled: true, RAMPct: 100, StorageGB: 0}
		cap := ApplyCaps(p, hw)

		if cap.StorageBytes != 0 {
			t.Errorf("StorageBytes: got %d, want 0 (uncapped)", cap.StorageBytes)
		}
	})
}

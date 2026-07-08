package sounding

import "testing"

// seedLadder mirrors the migration-025 seed: three available stages + a top
// coming_soon "storm" tier. Ceilings in vCPU / MB / MB.
func seedLadder() Ladder {
	return NewLadder([]Tier{
		{Name: "cumulus", Order: 1, CPUCeiling: 2, MemCeiling: 4096, DiskCeiling: 20480, State: "available"},
		{Name: "congestus", Order: 2, CPUCeiling: 8, MemCeiling: 16384, DiskCeiling: 102400, State: "available"},
		{Name: "cumulonimbus", Order: 3, CPUCeiling: 32, MemCeiling: 65536, DiskCeiling: 512000, State: "available"},
		{Name: "storm", Order: 4, CPUCeiling: 128, MemCeiling: 262144, DiskCeiling: 2097152, State: "coming_soon"},
	})
}

func TestFitRung(t *testing.T) {
	l := seedLadder()
	cases := []struct {
		name      string
		cpu       float64
		mem, disk int64
		wantRung  string
		wantOK    bool
	}{
		{"tiny fits cumulus", 1, 2048, 10240, "cumulus", true},
		{"medium fits congestus", 4, 8192, 51200, "congestus", true},
		{"large fits cumulonimbus", 16, 32768, 256000, "cumulonimbus", true},
		{"cpu over top available", 64, 1024, 1024, "", false},
		{"mem over top available", 1, 131072, 1024, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rung, ok := l.FitRung(tc.cpu, tc.mem, tc.disk)
			if rung != tc.wantRung || ok != tc.wantOK {
				t.Fatalf("FitRung=(%q,%v), want (%q,%v)", rung, ok, tc.wantRung, tc.wantOK)
			}
		})
	}
}

func TestFootprintAndIntensity(t *testing.T) {
	l := seedLadder() // top available = cumulonimbus (cpu 32, mem 65536, disk 512000)

	// A job at exactly the top-available CPU ceiling => footprint 1.0 on the CPU axis.
	if fp := l.Footprint(32, 1, 1); fp != 1.0 {
		t.Fatalf("Footprint at ceiling: got %v, want 1.0", fp)
	}
	// Towering past the top available tier => footprint > 1.
	if fp := l.Footprint(64, 1, 1); fp <= 1.0 {
		t.Fatalf("Footprint over ceiling: got %v, want > 1.0", fp)
	}
	// Intensity is cpu / top-available cpu ceiling.
	if in := l.Intensity(16); in != 0.5 {
		t.Fatalf("Intensity: got %v, want 0.5", in)
	}
	// Empty ladder never fabricates a signal.
	var empty Ladder
	if fp := empty.Footprint(100, 100, 100); fp != 0 {
		t.Fatalf("empty ladder footprint: got %v, want 0", fp)
	}
	if in := empty.Intensity(100); in != 0 {
		t.Fatalf("empty ladder intensity: got %v, want 0", in)
	}
}

func TestClassifyRejection(t *testing.T) {
	l := seedLadder()

	// Fits an available tier but got rejected => backlog / no_capacity, wanted=that tier.
	if reason, wanted := l.ClassifyRejection(4, 8192, 51200); reason != ReasonNoCapacity || wanted != "congestus" {
		t.Fatalf("fits-available: got (%q,%q), want (%q,%q)", reason, wanted, ReasonNoCapacity, "congestus")
	}

	// Exceeds every available tier, coming_soon exists => too_big, wanted=storm.
	if reason, wanted := l.ClassifyRejection(64, 1024, 1024); reason != ReasonTooBig || wanted != "storm" {
		t.Fatalf("too-big: got (%q,%q), want (%q,%q)", reason, wanted, ReasonTooBig, "storm")
	}

	// Empty ladder => no_capacity, no wanted rung.
	var empty Ladder
	if reason, wanted := empty.ClassifyRejection(4, 8192, 51200); reason != ReasonNoCapacity || wanted != "" {
		t.Fatalf("empty ladder: got (%q,%q), want (%q,\"\")", reason, wanted, ReasonNoCapacity)
	}

	// No coming_soon tier + exceeds all available => no_matching_tier.
	noCS := NewLadder([]Tier{
		{Name: "cumulus", Order: 1, CPUCeiling: 2, MemCeiling: 4096, DiskCeiling: 20480, State: "available"},
	})
	if reason, wanted := noCS.ClassifyRejection(64, 1, 1); reason != ReasonNoMatchingTier || wanted != "" {
		t.Fatalf("no-coming-soon: got (%q,%q), want (%q,\"\")", reason, wanted, ReasonNoMatchingTier)
	}
}

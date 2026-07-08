package sounding

import (
	"context"
	"sort"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// Rejection reason constants. These MUST match the CHECK constraint on
// operator_placement_rejections.reason (migration 025); an INSERT with any
// other value fails the CHECK and — being fire-and-forget — is silently
// dropped. The instrumentation only emits ReasonTooBig / ReasonNoMatchingTier /
// ReasonNoCapacity, the subset derivable from the placement outcome without
// changing FindMatch (which collapses every miss into one opaque error).
//   - ReasonOptedOut is indistinguishable from ReasonNoCapacity at the
//     SubmitJob seam (FindMatch folds the opt-out skip into the same "no
//     available nodes" error), so it is not emitted from here; the agent-side
//     opt-out gate remains the canonical signal.
//   - ReasonHadToSplit is emitted only if a split path exists. None does, so
//     it is never emitted.
const (
	ReasonTooBig         = "too_big"
	ReasonNoMatchingTier = "no_matching_tier"
	ReasonHadToSplit     = "had_to_split"
	ReasonOptedOut       = "opted_out"
	ReasonNoCapacity     = "no_capacity"
)

// Tier is one rung of the capacity ladder (a row of rung_tiers). Units match
// operator_job_shapes: CPUCeiling in vCPU, MemCeiling / DiskCeiling in MB.
type Tier struct {
	Name        string
	Order       int
	CPUCeiling  float64
	MemCeiling  int64
	DiskCeiling int64
	State       string // "available" | "coming_soon"
}

const (
	stateAvailable  = "available"
	stateComingSoon = "coming_soon"
)

// Ladder is the rung-tier ladder used to classify a job's shape against the
// tiers. It is loaded once at startup (LoadLadder) and treated as immutable;
// the zero Ladder (no tiers) is valid and degrades to ReasonNoCapacity with a
// zero footprint, so instrumentation is safe even if the ladder never loaded.
type Ladder struct {
	tiers []Tier // sorted ascending by Order
}

// NewLadder returns a Ladder over a copy of tiers, sorted ascending by Order.
// Exported so tests (and future callers) can build a ladder without a DB.
func NewLadder(tiers []Tier) Ladder {
	cp := make([]Tier, len(tiers))
	copy(cp, tiers)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Order < cp[j].Order })
	return Ladder{tiers: cp}
}

// LoadLadder reads rung_tiers into a Ladder. On any error the caller should log
// and proceed with the zero Ladder (fail-open); classification then degrades
// gracefully rather than blocking startup.
func LoadLadder(ctx context.Context, db *store.DB) (Ladder, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT name, tier_order, cpu_ceiling, mem_ceiling, disk_ceiling, state
		   FROM rung_tiers`)
	if err != nil {
		return Ladder{}, err
	}
	defer rows.Close()

	var tiers []Tier
	for rows.Next() {
		var t Tier
		if err := rows.Scan(&t.Name, &t.Order, &t.CPUCeiling, &t.MemCeiling, &t.DiskCeiling, &t.State); err != nil {
			return Ladder{}, err
		}
		tiers = append(tiers, t)
	}
	if err := rows.Err(); err != nil {
		return Ladder{}, err
	}
	return NewLadder(tiers), nil
}

// Tiers returns a copy of the ladder's tiers, sorted ascending by Order,
// including any 'coming_soon' rungs. The :8090 sounding dashboard reads this to
// render the rung ladder (weather stages, ceilings, and the fake-door storm
// tier). A copy is returned so callers cannot mutate the ladder's backing slice.
func (l Ladder) Tiers() []Tier {
	cp := make([]Tier, len(l.tiers))
	copy(cp, l.tiers)
	return cp
}

// topAvailable returns the highest-Order tier in state 'available', or nil.
func (l Ladder) topAvailable() *Tier {
	var top *Tier
	for i := range l.tiers {
		if l.tiers[i].State != stateAvailable {
			continue
		}
		if top == nil || l.tiers[i].Order > top.Order {
			top = &l.tiers[i]
		}
	}
	return top
}

// topComingSoon returns the highest-Order tier in state 'coming_soon', or nil.
// This is the fake-door "storm" tier — the tier a towering job is asking for.
func (l Ladder) topComingSoon() *Tier {
	var top *Tier
	for i := range l.tiers {
		if l.tiers[i].State != stateComingSoon {
			continue
		}
		if top == nil || l.tiers[i].Order > top.Order {
			top = &l.tiers[i]
		}
	}
	return top
}

// fits reports whether a job of (cpu, memMB, diskMB) is within a tier's ceilings.
func (t Tier) fits(cpu float64, memMB, diskMB int64) bool {
	return cpu <= t.CPUCeiling && memMB <= t.MemCeiling && diskMB <= t.DiskCeiling
}

// FitRung returns the name of the smallest AVAILABLE tier that fits the job and
// true, or ("", false) if no available tier fits (or the ladder is empty).
func (l Ladder) FitRung(cpu float64, memMB, diskMB int64) (string, bool) {
	// tiers are sorted ascending by Order, so the first available fit is smallest.
	for i := range l.tiers {
		if l.tiers[i].State != stateAvailable {
			continue
		}
		if l.tiers[i].fits(cpu, memMB, diskMB) {
			return l.tiers[i].Name, true
		}
	}
	return "", false
}

// Footprint is a single-axis composite saturation of the job against the top
// AVAILABLE tier's ceilings: max(cpu/cpuCeil, mem/memCeil, disk/diskCeil).
// A value > 1.0 means the job exceeds the largest available tier — the
// "towering against the ceiling" signal. Returns 0 when there is no available
// tier (empty ladder), so a missing ladder never fabricates a signal.
func (l Ladder) Footprint(cpu float64, memMB, diskMB int64) float64 {
	top := l.topAvailable()
	if top == nil || top.CPUCeiling <= 0 || top.MemCeiling <= 0 || top.DiskCeiling <= 0 {
		return 0
	}
	fp := cpu / top.CPUCeiling
	if r := float64(memMB) / float64(top.MemCeiling); r > fp {
		fp = r
	}
	if r := float64(diskMB) / float64(top.DiskCeiling); r > fp {
		fp = r
	}
	return fp
}

// Intensity is a normalized compute-intensity proxy: the job's vCPU demand as a
// fraction of the top available tier's CPU ceiling. Returns 0 for an empty
// ladder. It is the scatter Y-candidate in step 3's intensity×duration view.
func (l Ladder) Intensity(cpu float64) float64 {
	top := l.topAvailable()
	if top == nil || top.CPUCeiling <= 0 {
		return 0
	}
	return cpu / top.CPUCeiling
}

// ClassifyRejection derives a rejection reason and the wanted rung for a job
// that failed placement, using only the job shape and the ladder — no DB, no
// change to FindMatch. Semantics:
//   - fits an available tier            → ReasonNoCapacity, wanted = that tier
//     (the tier exists, but no node had room right now — the classic backlog).
//   - exceeds every available tier, and
//     a coming_soon tier is defined      → ReasonTooBig, wanted = coming_soon
//     (towering past what we offer — the "ship the storm tier" demand signal).
//   - exceeds every available tier, and
//     NO coming_soon tier is defined     → ReasonNoMatchingTier, wanted = "".
//   - empty ladder                       → ReasonNoCapacity, wanted = "".
func (l Ladder) ClassifyRejection(cpu float64, memMB, diskMB int64) (reason, wantedRung string) {
	if len(l.tiers) == 0 {
		return ReasonNoCapacity, ""
	}
	if fit, ok := l.FitRung(cpu, memMB, diskMB); ok {
		return ReasonNoCapacity, fit
	}
	if cs := l.topComingSoon(); cs != nil {
		return ReasonTooBig, cs.Name
	}
	return ReasonNoMatchingTier, ""
}

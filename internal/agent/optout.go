package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ResourceOptOut captures the contributor's consent for each resource
// category their node can offer. All fields default to disabled
// (fail-closed). The contributor explicitly enables what they wish to
// share via the portal; the agent persists the result in opt-out.json
// next to agent.conf and consults it before accepting any work.
//
// Per-printer policy is stricter than the category toggle: even when
// PrintingEnabled is true, only printers explicitly listed in
// EnabledPrinters (keyed by PrinterInfo.ID) will accept print jobs.
// This matches the stated trust model: print jobs consume physical
// consumables (paper, ink, filament) the contributor paid for, and
// failed prints are the contributor's responsibility, so each printer
// must be opted in individually.
type ResourceOptOut struct {
	ComputeEnabled   bool            `json:"compute_enabled"`
	StorageEnabled   bool            `json:"storage_enabled"`
	PrintingEnabled  bool            `json:"printing_enabled"`
	EnabledPrinters  map[string]bool `json:"enabled_printers,omitempty"`
}

// DefaultOptOut returns a ResourceOptOut with every resource disabled.
// Used when no opt-out file exists yet (first run before portal sync)
// or when loading fails for any reason. Fail-closed: an agent with no
// confirmed opt-out does no work until the contributor opts in via
// the portal.
func DefaultOptOut() ResourceOptOut {
	return ResourceOptOut{
		ComputeEnabled:  false,
		StorageEnabled:  false,
		PrintingEnabled: false,
		EnabledPrinters: map[string]bool{},
	}
}

// OptOutCachePath returns the on-disk path for the opt-out file. It
// lives next to agent.conf so it shares the same protected directory.
// Exposed as a variable so tests can override it.
var OptOutCachePath = func() string {
	return filepath.Join(filepath.Dir(DefaultConfigPath()), "opt-out.json")
}

// Errors returned by opt-out operations.
var (
	ErrOptOutMalformed = errors.New("opt-out file malformed")
)

// LoadOptOutFromFile reads the opt-out file at the given path and
// returns the parsed ResourceOptOut. If the file does not exist, it
// returns DefaultOptOut() and a nil error — first-run is normal, not
// an error condition. Other read or parse failures are returned so
// the caller can decide whether to log and proceed with defaults or
// fail loudly.
func LoadOptOutFromFile(path string) (ResourceOptOut, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultOptOut(), nil
		}
		return DefaultOptOut(), fmt.Errorf("read opt-out file: %w", err)
	}
	var oo ResourceOptOut
	if err := json.Unmarshal(data, &oo); err != nil {
		return DefaultOptOut(), fmt.Errorf("%w: %v", ErrOptOutMalformed, err)
	}
	if oo.EnabledPrinters == nil {
		oo.EnabledPrinters = map[string]bool{}
	}
	return oo, nil
}

// SaveOptOutToFile persists the opt-out atomically to the given path.
// Parent directory is created if missing (mode 0700). File is written
// to a temp file and renamed into place to avoid leaving a partial
// file on crash mid-write.
func SaveOptOutToFile(path string, oo ResourceOptOut) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create opt-out dir: %w", err)
	}
	data, err := json.MarshalIndent(oo, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal opt-out: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp opt-out: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename opt-out: %w", err)
	}
	return nil
}

// OptOutStore holds the live ResourceOptOut behind a sync.RWMutex so
// the job-poll loop can read it cheaply on every iteration while the
// portal-sync code path can update it atomically. The store is the
// single source of truth at runtime; callers must never read or
// mutate the underlying ResourceOptOut directly.
type OptOutStore struct {
	mu sync.RWMutex
	oo ResourceOptOut
}

// NewOptOutStore constructs a store seeded with the given initial
// value. Callers typically pass the result of LoadOptOutFromFile or
// DefaultOptOut.
func NewOptOutStore(initial ResourceOptOut) *OptOutStore {
	if initial.EnabledPrinters == nil {
		initial.EnabledPrinters = map[string]bool{}
	}
	return &OptOutStore{oo: initial}
}

// Get returns a deep copy of the current opt-out value. The copy is
// safe to mutate without affecting the store. Use this when you need
// to inspect multiple fields atomically without holding the lock.
func (s *OptOutStore) Get() ResourceOptOut {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.oo
	out.EnabledPrinters = make(map[string]bool, len(s.oo.EnabledPrinters))
	for k, v := range s.oo.EnabledPrinters {
		out.EnabledPrinters[k] = v
	}
	return out
}

// Set atomically replaces the stored opt-out. Used by the portal-sync
// path to apply contributor-driven changes immediately. The very next
// IsResourceEnabled or Get call observes the new value.
func (s *OptOutStore) Set(oo ResourceOptOut) {
	if oo.EnabledPrinters == nil {
		oo.EnabledPrinters = map[string]bool{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.oo = oo
}

// IsResourceEnabled is the single chokepoint the job-poll loop and
// the heartbeat consult before accepting work. printerID is required
// only for the printing workload types and is ignored otherwise; a
// caller may pass "" for compute or storage workloads.
//
// Decision matrix:
//   - compute / storage: requires the matching category toggle to be true.
//   - print_traditional / print_3d: requires PrintingEnabled true AND
//     the specific printerID present in EnabledPrinters with value true.
//   - unknown workload type: false (fail-closed for safety).
func (s *OptOutStore) IsResourceEnabled(workload WorkloadType, printerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch workload {
	case WorkloadCompute:
		return s.oo.ComputeEnabled
	case WorkloadStorage:
		return s.oo.StorageEnabled
	case WorkloadPrintTraditional, WorkloadPrint3D:
		if !s.oo.PrintingEnabled {
			return false
		}
		return s.oo.EnabledPrinters[printerID]
	default:
		return false
	}
}

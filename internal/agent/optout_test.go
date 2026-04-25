package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// withTestOptOutPath redirects OptOutCachePath to a file in a temp dir
// for the duration of the test. Returns the redirected path.
func withTestOptOutPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "opt-out.json")
	original := OptOutCachePath
	OptOutCachePath = func() string { return path }
	t.Cleanup(func() { OptOutCachePath = original })
	return path
}

func TestDefaultOptOut_AllDisabled(t *testing.T) {
	oo := DefaultOptOut()
	if oo.ComputeEnabled || oo.StorageEnabled || oo.PrintingEnabled {
		t.Fatalf("expected all categories disabled by default, got %+v", oo)
	}
	if len(oo.EnabledPrinters) != 0 {
		t.Fatalf("expected no enabled printers by default, got %v", oo.EnabledPrinters)
	}
}

func TestLoadOptOutFromFile_MissingFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.json")
	oo, err := LoadOptOutFromFile(missing)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if oo.ComputeEnabled || oo.StorageEnabled || oo.PrintingEnabled {
		t.Fatalf("expected default opt-out, got %+v", oo)
	}
}

func TestLoadOptOutFromFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opt-out.json")
	want := ResourceOptOut{
		ComputeEnabled:  true,
		StorageEnabled:  false,
		PrintingEnabled: true,
		EnabledPrinters: map[string]bool{
			"cups:laser": true,
			"usb:2C99":   true,
		},
	}
	if err := SaveOptOutToFile(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadOptOutFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ComputeEnabled != want.ComputeEnabled ||
		got.StorageEnabled != want.StorageEnabled ||
		got.PrintingEnabled != want.PrintingEnabled {
		t.Fatalf("category mismatch: want %+v got %+v", want, got)
	}
	if len(got.EnabledPrinters) != len(want.EnabledPrinters) {
		t.Fatalf("printer count mismatch: want %v got %v",
			want.EnabledPrinters, got.EnabledPrinters)
	}
	for k, v := range want.EnabledPrinters {
		if got.EnabledPrinters[k] != v {
			t.Fatalf("printer %s: want %v got %v", k, v, got.EnabledPrinters[k])
		}
	}
}

func TestLoadOptOutFromFile_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opt-out.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	oo, err := LoadOptOutFromFile(path)
	if !errors.Is(err, ErrOptOutMalformed) {
		t.Fatalf("expected ErrOptOutMalformed, got %v", err)
	}
	// Even on error, defaults should be returned for safe fallback.
	if oo.ComputeEnabled || oo.StorageEnabled || oo.PrintingEnabled {
		t.Fatalf("expected default opt-out on malformed file, got %+v", oo)
	}
}

func TestSaveOptOutToFile_AtomicityViaTempThenRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opt-out.json")
	oo := DefaultOptOut()
	oo.ComputeEnabled = true
	if err := SaveOptOutToFile(path, oo); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Confirm no .tmp file remains alongside.
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); err == nil {
		t.Fatalf("expected no temp file after successful save, found %s", tmp)
	}
	// Confirm the actual file is well-formed JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var roundtrip ResourceOptOut
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestOptOutStore_DefaultIsAllDisabled(t *testing.T) {
	store := NewOptOutStore(DefaultOptOut())
	if store.IsResourceEnabled(WorkloadCompute, "") {
		t.Fatal("expected compute disabled by default")
	}
	if store.IsResourceEnabled(WorkloadStorage, "") {
		t.Fatal("expected storage disabled by default")
	}
	if store.IsResourceEnabled(WorkloadPrintTraditional, "cups:laser") {
		t.Fatal("expected printing disabled by default")
	}
}

func TestOptOutStore_ComputeStorageToggles(t *testing.T) {
	oo := DefaultOptOut()
	oo.ComputeEnabled = true
	oo.StorageEnabled = false
	store := NewOptOutStore(oo)

	if !store.IsResourceEnabled(WorkloadCompute, "") {
		t.Fatal("expected compute enabled")
	}
	if store.IsResourceEnabled(WorkloadStorage, "") {
		t.Fatal("expected storage disabled")
	}
}

func TestOptOutStore_PrintingRequiresBothToggleAndPerPrinter(t *testing.T) {
	cases := []struct {
		name           string
		printingOn     bool
		enabledPrinter string // "" means none enabled
		queryPrinterID string
		want           bool
	}{
		{"category off, printer enabled", false, "cups:laser", "cups:laser", false},
		{"category on, printer not enabled", true, "", "cups:laser", false},
		{"category on, different printer enabled", true, "cups:other", "cups:laser", false},
		{"category on, this printer enabled", true, "cups:laser", "cups:laser", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oo := DefaultOptOut()
			oo.PrintingEnabled = tc.printingOn
			if tc.enabledPrinter != "" {
				oo.EnabledPrinters[tc.enabledPrinter] = true
			}
			store := NewOptOutStore(oo)
			got := store.IsResourceEnabled(WorkloadPrintTraditional, tc.queryPrinterID)
			if got != tc.want {
				t.Fatalf("want %v got %v", tc.want, got)
			}
			// Same logic must apply to 3D printing.
			got3d := store.IsResourceEnabled(WorkloadPrint3D, tc.queryPrinterID)
			if got3d != tc.want {
				t.Fatalf("3d: want %v got %v", tc.want, got3d)
			}
		})
	}
}

func TestOptOutStore_UnknownWorkloadDenied(t *testing.T) {
	oo := DefaultOptOut()
	oo.ComputeEnabled = true
	oo.StorageEnabled = true
	oo.PrintingEnabled = true
	store := NewOptOutStore(oo)
	if store.IsResourceEnabled(WorkloadType("unknown_future_type"), "") {
		t.Fatal("expected unknown workload to be denied")
	}
}

func TestOptOutStore_GetReturnsDeepCopy(t *testing.T) {
	oo := DefaultOptOut()
	oo.ComputeEnabled = true
	oo.EnabledPrinters["cups:laser"] = true
	store := NewOptOutStore(oo)

	snapshot := store.Get()
	// Mutate the snapshot map.
	snapshot.EnabledPrinters["cups:other"] = true
	snapshot.ComputeEnabled = false

	// Store must be unaffected.
	again := store.Get()
	if !again.ComputeEnabled {
		t.Fatal("store ComputeEnabled mutated by snapshot edit")
	}
	if again.EnabledPrinters["cups:other"] {
		t.Fatal("store EnabledPrinters mutated by snapshot edit")
	}
}

func TestOptOutStore_SetAppliesImmediately(t *testing.T) {
	store := NewOptOutStore(DefaultOptOut())
	if store.IsResourceEnabled(WorkloadCompute, "") {
		t.Fatal("precondition: compute should be off initially")
	}
	updated := DefaultOptOut()
	updated.ComputeEnabled = true
	store.Set(updated)
	if !store.IsResourceEnabled(WorkloadCompute, "") {
		t.Fatal("expected compute enabled immediately after Set")
	}
}

func TestOptOutStore_ConcurrentReadsAndWrites(t *testing.T) {
	store := NewOptOutStore(DefaultOptOut())
	var wg sync.WaitGroup

	// 50 readers + 5 writers, run for a short burst. We rely on the
	// race detector (go test -race) to catch any unsafe sharing; the
	// test passes as long as nothing panics or deadlocks.
	const readers = 50
	const writers = 5
	const iterations = 200

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = store.IsResourceEnabled(WorkloadCompute, "")
				_ = store.Get()
			}
		}()
	}

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				oo := DefaultOptOut()
				oo.ComputeEnabled = (i+wid)%2 == 0
				store.Set(oo)
			}
		}(w)
	}

	wg.Wait()
}

func TestLoadOptOutFromFile_NilMapIsNormalized(t *testing.T) {
	// Older opt-out files might omit the EnabledPrinters key entirely;
	// after loading, the map must be initialized to a non-nil empty
	// map so callers can index into it without panicking.
	dir := t.TempDir()
	path := filepath.Join(dir, "opt-out.json")
	if err := os.WriteFile(path, []byte(`{"compute_enabled":true}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	oo, err := LoadOptOutFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if oo.EnabledPrinters == nil {
		t.Fatal("EnabledPrinters should be initialized to non-nil empty map")
	}
	// A query against the empty map must not panic.
	if _, ok := oo.EnabledPrinters["any"]; ok {
		t.Fatal("expected empty map to have no keys")
	}
}
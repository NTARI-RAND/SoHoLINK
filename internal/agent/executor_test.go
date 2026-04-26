package agent

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	dockerclient "github.com/docker/docker/client"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// cupsSocketTestPath mirrors cupsSocketHostPath from executor_devices_unix.go.
// Duplicated here as a literal because the constant lives behind a Unix
// build tag and executor_test.go is built on all platforms.
const cupsSocketTestPath = "/var/run/cups/cups.sock"

// fakeInspector satisfies imageInspector without a Docker daemon.
type fakeInspector struct {
	response image.InspectResponse
	err      error
}

func (f *fakeInspector) ImageInspect(_ context.Context, _ string, _ ...dockerclient.ImageInspectOption) (image.InspectResponse, error) {
	return f.response, f.err
}

// minimalAllowlist returns an Allowlist with a single entry and no signature
// validation — valid for unit tests that never call Verify().
func minimalAllowlist() *Allowlist {
	return &Allowlist{
		Version: 1,
		Entries: []AllowlistEntry{
			{
				Name:   "worker",
				Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Type:   WorkloadCompute,
				Egress: EgressNone,
			},
		},
	}
}

const allowedImage = "soholink/worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// permissiveOptOutStore returns an OptOutStore with every resource category
// enabled. Used by tests that exercise non-opt-out behaviour so they are not
// blocked by the gate.
func permissiveOptOutStore() *OptOutStore {
	return NewOptOutStore(ResourceOptOut{
		ComputeEnabled:  true,
		StorageEnabled:  true,
		PrintingEnabled: true,
		EnabledPrinters: map[string]bool{"printer-1": true},
	})
}

// TestNewExecutor_NilAllowlist confirms fail-closed construction on nil allowlist.
func TestNewExecutor_NilAllowlist(t *testing.T) {
	_, err := NewExecutor(nil, permissiveOptOutStore())
	if err == nil {
		t.Fatal("expected error for nil allowlist, got nil")
	}
	if !strings.Contains(err.Error(), "allowlist required") {
		t.Errorf("error %q does not mention 'allowlist required'", err)
	}
}

// TestNewExecutor_NilOptOutRejected confirms fail-closed construction on nil optout.
func TestNewExecutor_NilOptOutRejected(t *testing.T) {
	_, err := NewExecutor(minimalAllowlist(), nil)
	if err == nil {
		t.Fatal("expected error for nil optout, got nil")
	}
	if !strings.Contains(err.Error(), "optout store required") {
		t.Errorf("error %q does not mention 'optout store required'", err)
	}
}

// TestRun_TagOnlyImageRejected confirms Lookup rejects tag-only references
// before any Docker call is made.
func TestRun_TagOnlyImageRejected(t *testing.T) {
	ex := newExecutorForTest(minimalAllowlist(), &fakeInspector{}, permissiveOptOutStore())
	spec := ContainerSpec{Image: "soholink/worker:latest"}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Errorf("expected ErrImageNotAllowed, got %v", err)
	}
}

// TestRun_DigestNotInAllowlist confirms Lookup rejects an unknown digest.
func TestRun_DigestNotInAllowlist(t *testing.T) {
	ex := newExecutorForTest(minimalAllowlist(), &fakeInspector{}, permissiveOptOutStore())
	spec := ContainerSpec{
		Image: "soholink/worker@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Errorf("expected ErrImageNotAllowed, got %v", err)
	}
}

// TestRun_RootContainerRejected confirms root rejection when Config.User is empty.
func TestRun_RootContainerRejected(t *testing.T) {
	inspector := &fakeInspector{
		response: image.InspectResponse{
			Config: &dockerspec.DockerOCIImageConfig{
				ImageConfig: ocispec.ImageConfig{User: ""},
			},
		},
	}
	ex := newExecutorForTest(minimalAllowlist(), inspector, permissiveOptOutStore())
	spec := ContainerSpec{Image: allowedImage}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrRootContainerNotAllowed) {
		t.Errorf("expected ErrRootContainerNotAllowed, got %v", err)
	}
}

// TestRun_RootContainerRejected_NilConfig confirms nil Config is treated as root.
func TestRun_RootContainerRejected_NilConfig(t *testing.T) {
	inspector := &fakeInspector{
		response: image.InspectResponse{Config: nil},
	}
	ex := newExecutorForTest(minimalAllowlist(), inspector, permissiveOptOutStore())
	spec := ContainerSpec{Image: allowedImage}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrRootContainerNotAllowed) {
		t.Errorf("expected ErrRootContainerNotAllowed for nil Config, got %v", err)
	}
}

// TestRun_RootUserForms covers all canonical root-user representations.
func TestRun_RootUserForms(t *testing.T) {
	cases := []struct {
		name string
		user string
	}{
		{"empty", ""},
		{"uid_zero", "0"},
		{"uid_zero_gid_zero", "0:0"},
		{"root", "root"},
		{"root_with_group", "root:wheel"},
		{"root_with_zero_group", "root:0"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			inspector := &fakeInspector{
				response: image.InspectResponse{
					Config: &dockerspec.DockerOCIImageConfig{
						ImageConfig: ocispec.ImageConfig{User: tc.user},
					},
				},
			}
			ex := newExecutorForTest(minimalAllowlist(), inspector, permissiveOptOutStore())
			spec := ContainerSpec{Image: allowedImage}
			_, err := ex.Run(context.Background(), spec)
			if !errors.Is(err, ErrRootContainerNotAllowed) {
				t.Errorf("user=%q: expected ErrRootContainerNotAllowed, got %v", tc.user, err)
			}
		})
	}
}

// TestRun_NonRootPassesUserCheck confirms that a non-root user clears both
// the allowlist and root-user checks. The test does not reach ContainerCreate
// — there is no Docker daemon — so we skip it in CI environments that lack one.
// Full end-to-end coverage lives in the integration test suite (commit 2).
func TestRun_NonRootPassesUserCheck(t *testing.T) {
	t.Skip("requires Docker daemon — covered by integration tests in commit 2")
}

// --- buildHostConfig tests ---

func entryWith(da ...DeviceAccess) *AllowlistEntry {
	return &AllowlistEntry{
		Name:         "worker",
		Digest:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Type:         WorkloadCompute,
		Egress:       EgressNone,
		DeviceAccess: da,
	}
}

func TestBuildHostConfig_AppliesSecurityBaseline(t *testing.T) {
	spec := ContainerSpec{Image: allowedImage}
	hc := buildHostConfig(spec, entryWith())

	if !hc.ReadonlyRootfs {
		t.Error("expected ReadonlyRootfs = true")
	}
	if len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("expected CapDrop=[ALL], got %v", hc.CapDrop)
	}
	if len(hc.SecurityOpt) != 1 || hc.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("expected SecurityOpt=[no-new-privileges:true], got %v", hc.SecurityOpt)
	}
	if hc.Privileged {
		t.Error("expected Privileged = false")
	}
}

func TestBuildHostConfig_TmpfsScratchPresent(t *testing.T) {
	spec := ContainerSpec{Image: allowedImage}
	hc := buildHostConfig(spec, entryWith())

	var found bool
	for _, m := range hc.Mounts {
		if m.Type == mount.TypeTmpfs && m.Target == "/tmp" {
			found = true
			if m.TmpfsOptions == nil {
				t.Fatal("tmpfs mount at /tmp has nil TmpfsOptions")
			}
			if m.TmpfsOptions.SizeBytes != tmpfsScratchSize {
				t.Errorf("tmpfs SizeBytes = %d, want %d", m.TmpfsOptions.SizeBytes, tmpfsScratchSize)
			}
		}
	}
	if !found {
		t.Error("no tmpfs mount at /tmp found in HostConfig.Mounts")
	}
}

func TestBuildHostConfig_PreservesResourceCaps(t *testing.T) {
	spec := ContainerSpec{
		Image: allowedImage,
		Caps: CapProfile{
			CPUEnabled:   true,
			CPUCores:     4,
			RAMBytes:     2 * 1024 * 1024 * 1024,
			StorageBytes: 10 * 1024 * 1024 * 1024,
		},
	}
	hc := buildHostConfig(spec, entryWith())

	if hc.Resources.NanoCPUs != 4*1e9 {
		t.Errorf("NanoCPUs = %d, want %d", hc.Resources.NanoCPUs, int64(4*1e9))
	}
	if hc.Resources.Memory != spec.Caps.RAMBytes {
		t.Errorf("Memory = %d, want %d", hc.Resources.Memory, spec.Caps.RAMBytes)
	}
	if hc.StorageOpt["size"] != "10737418240" {
		t.Errorf("StorageOpt[size] = %q, want %q", hc.StorageOpt["size"], "10737418240")
	}
}

func TestBuildHostConfig_CUPSDeviceAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CUPS bind mount not applicable on Windows")
	}
	spec := ContainerSpec{Image: allowedImage}
	hc := buildHostConfig(spec, entryWith(DeviceCUPSSocket))

	var found bool
	for _, m := range hc.Mounts {
		if m.Type == mount.TypeBind && m.Source == cupsSocketTestPath && m.Target == cupsSocketTestPath {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bind mount for %s, got mounts: %v", cupsSocketTestPath, hc.Mounts)
	}
}

func TestBuildHostConfig_NoCUPSOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only stub test")
	}
	spec := ContainerSpec{Image: allowedImage}
	hc := buildHostConfig(spec, entryWith(DeviceCUPSSocket))

	for _, m := range hc.Mounts {
		if m.Type == mount.TypeBind {
			t.Errorf("expected no bind mounts on Windows, got: %v", m)
		}
	}
}

// --- opt-out gate tests ---

func TestRun_ComputeOptedOut(t *testing.T) {
	store := NewOptOutStore(DefaultOptOut()) // all disabled
	ex := newExecutorForTest(minimalAllowlist(), &fakeInspector{}, store)
	spec := ContainerSpec{Image: allowedImage}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrWorkloadOptedOut) {
		t.Errorf("expected ErrWorkloadOptedOut, got %v", err)
	}
}

func TestRun_StorageOptedOut(t *testing.T) {
	al := &Allowlist{
		Version: 1,
		Entries: []AllowlistEntry{{
			Name:   "storage-worker",
			Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Type:   WorkloadStorage,
			Egress: EgressNone,
		}},
	}
	store := NewOptOutStore(DefaultOptOut())
	ex := newExecutorForTest(al, &fakeInspector{}, store)
	spec := ContainerSpec{Image: "soholink/storage-worker@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrWorkloadOptedOut) {
		t.Errorf("expected ErrWorkloadOptedOut, got %v", err)
	}
}

func TestRun_PrintOptedOutNoPrinter(t *testing.T) {
	al := &Allowlist{
		Version: 1,
		Entries: []AllowlistEntry{{
			Name:   "print-worker",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			Type:   WorkloadPrintTraditional,
			Egress: EgressNone,
		}},
	}
	// PrintingEnabled true but no printer opted in — printerID "" must fail.
	store := NewOptOutStore(ResourceOptOut{
		PrintingEnabled: true,
		EnabledPrinters: map[string]bool{},
	})
	ex := newExecutorForTest(al, &fakeInspector{}, store)
	spec := ContainerSpec{Image: "soholink/print-worker@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrWorkloadOptedOut) {
		t.Errorf("expected ErrWorkloadOptedOut, got %v", err)
	}
}

func TestRun_OptOutGateAfterAllowlist(t *testing.T) {
	// Unknown digest — allowlist rejects before opt-out is consulted.
	store := NewOptOutStore(DefaultOptOut()) // opted out
	ex := newExecutorForTest(minimalAllowlist(), &fakeInspector{}, store)
	spec := ContainerSpec{Image: "soholink/worker@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Errorf("expected ErrImageNotAllowed (allowlist fires first), got %v", err)
	}
}

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

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

// TestNewExecutor_NilAllowlist confirms fail-closed construction.
func TestNewExecutor_NilAllowlist(t *testing.T) {
	_, err := NewExecutor(nil)
	if err == nil {
		t.Fatal("expected error for nil allowlist, got nil")
	}
	if !strings.Contains(err.Error(), "allowlist required") {
		t.Errorf("error %q does not mention 'allowlist required'", err)
	}
}

// TestRun_TagOnlyImageRejected confirms Lookup rejects tag-only references
// before any Docker call is made.
func TestRun_TagOnlyImageRejected(t *testing.T) {
	ex := newExecutorForTest(minimalAllowlist(), &fakeInspector{})
	spec := ContainerSpec{Image: "soholink/worker:latest"}
	_, err := ex.Run(context.Background(), spec)
	if !errors.Is(err, ErrImageNotAllowed) {
		t.Errorf("expected ErrImageNotAllowed, got %v", err)
	}
}

// TestRun_DigestNotInAllowlist confirms Lookup rejects an unknown digest.
func TestRun_DigestNotInAllowlist(t *testing.T) {
	ex := newExecutorForTest(minimalAllowlist(), &fakeInspector{})
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
	ex := newExecutorForTest(minimalAllowlist(), inspector)
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
	ex := newExecutorForTest(minimalAllowlist(), inspector)
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
			ex := newExecutorForTest(minimalAllowlist(), inspector)
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

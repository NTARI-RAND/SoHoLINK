package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
)

var ErrRootContainerNotAllowed = errors.New("container runs as root")

// ContainerSpec describes the workload container to run.
type ContainerSpec struct {
	Image    string
	JobID    string
	JobToken string
	Caps     CapProfile
	EnvVars  map[string]string
}

// ExecutionResult carries the outcome of a completed container run.
type ExecutionResult struct {
	JobID    string
	ExitCode int
	Error    string
}

// imageInspector is the subset of the Docker client used for image
// inspection. Extracted so tests can fake it without a Docker daemon.
type imageInspector interface {
	ImageInspect(ctx context.Context, ref string, opts ...dockerclient.ImageInspectOption) (image.InspectResponse, error)
}

// Executor manages Docker container lifecycle for SoHoLINK workloads.
type Executor struct {
	client    *dockerclient.Client
	inspector imageInspector
	allowlist *Allowlist
}

// NewExecutor creates an Executor using the Docker socket and environment
// variables (DOCKER_HOST, DOCKER_TLS_VERIFY, etc.). allowlist must be
// non-nil; a nil allowlist causes a fail-closed error at construction.
func NewExecutor(allowlist *Allowlist) (*Executor, error) {
	if allowlist == nil {
		return nil, fmt.Errorf("new executor: allowlist required")
	}
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("new executor: %w", err)
	}
	return &Executor{
		client:    cli,
		inspector: cli,
		allowlist: allowlist,
	}, nil
}

// newExecutorForTest builds an Executor with a caller-supplied inspector
// and no real Docker client. Tests that don't start containers can pass
// nil for the client field.
func newExecutorForTest(allowlist *Allowlist, inspector imageInspector) *Executor {
	return &Executor{
		client:    nil,
		inspector: inspector,
		allowlist: allowlist,
	}
}

// Run pulls the image if not locally present, creates and starts the container
// with resource caps from spec.Caps, waits for it to exit, removes it, and
// returns the exit code.
//
// Resource mapping:
//   - Memory:  container.HostConfig.Resources.Memory   (bytes)
//   - CPU:     container.HostConfig.Resources.NanoCPUs (cores × 1e9)
//   - Storage: container.HostConfig.StorageOpt["size"] (bytes as string)
//     The Docker API has no StorageSizeBytes field; StorageOpt is the correct
//     mechanism for overlay2 storage quotas.
func (e *Executor) Run(ctx context.Context, spec ContainerSpec) (ExecutionResult, error) {
	// Allowlist check — must be the first action, before any Docker call.
	if _, err := e.allowlist.Lookup(spec.Image); err != nil {
		return ExecutionResult{}, fmt.Errorf("run: %w", err)
	}

	// Inspect; pull if missing, then re-inspect to read the image metadata.
	inspect, err := e.inspector.ImageInspect(ctx, spec.Image)
	if err != nil {
		if !dockerclient.IsErrNotFound(err) {
			return ExecutionResult{}, fmt.Errorf("run: image inspect: %w", err)
		}
		reader, perr := e.client.ImagePull(ctx, spec.Image, image.PullOptions{})
		if perr != nil {
			return ExecutionResult{}, fmt.Errorf("run: image pull: %w", perr)
		}
		_, _ = io.Copy(io.Discard, reader)
		reader.Close()
		inspect, err = e.inspector.ImageInspect(ctx, spec.Image)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("run: image inspect after pull: %w", err)
		}
	}

	// Root-user check. A nil Config means no USER directive was set, which
	// is equivalent to uid 0.
	var user string
	if inspect.Config != nil {
		user = inspect.Config.User
	}
	if isRootUser(user) {
		return ExecutionResult{}, fmt.Errorf("run: %w", ErrRootContainerNotAllowed)
	}

	// Build env slice: caller-supplied vars plus the two SoHoLINK injections.
	env := make([]string, 0, len(spec.EnvVars)+2)
	for k, v := range spec.EnvVars {
		env = append(env, k+"="+v)
	}
	env = append(env, "SOHOLINK_JOB_ID="+spec.JobID)
	env = append(env, "SOHOLINK_JOB_TOKEN="+spec.JobToken)

	// Build resource limits.
	var nanoCPUs int64
	if spec.Caps.CPUEnabled {
		nanoCPUs = int64(spec.Caps.CPUCores) * 1e9
	}

	storageOpt := map[string]string{}
	if spec.Caps.StorageBytes > 0 {
		storageOpt["size"] = fmt.Sprintf("%d", spec.Caps.StorageBytes)
	}

	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			Memory:   spec.Caps.RAMBytes,
			NanoCPUs: nanoCPUs,
		},
		StorageOpt: storageOpt,
	}

	resp, err := e.client.ContainerCreate(ctx,
		&container.Config{
			Image: spec.Image,
			Env:   env,
		},
		hostCfg,
		nil, // networkingConfig — use Docker default
		nil, // platform — use host platform
		"",  // auto-generate container name
	)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("run: container create: %w", err)
	}
	containerID := resp.ID

	// Always remove the container when Run returns, regardless of outcome.
	defer e.client.ContainerRemove( //nolint:errcheck
		context.Background(), containerID,
		container.RemoveOptions{Force: true},
	)

	if err := e.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return ExecutionResult{}, fmt.Errorf("run: container start: %w", err)
	}

	statusCh, errCh := e.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return ExecutionResult{JobID: spec.JobID, Error: err.Error()}, nil
		}
		return ExecutionResult{JobID: spec.JobID}, nil
	case waitResp := <-statusCh:
		result := ExecutionResult{
			JobID:    spec.JobID,
			ExitCode: int(waitResp.StatusCode),
		}
		if waitResp.Error != nil {
			result.Error = waitResp.Error.Message
		}
		return result, nil
	}
}

// Stop gracefully stops the container then removes it.
func (e *Executor) Stop(ctx context.Context, containerID string) error {
	if err := e.client.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		return fmt.Errorf("stop container %s: %w", containerID, err)
	}
	if err := e.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove container %s: %w", containerID, err)
	}
	return nil
}

// isRootUser reports whether the image's USER directive resolves to uid 0.
// Empty, "0", "0:0", "root", and "root:<group>" all count as root.
func isRootUser(user string) bool {
	if user == "" {
		return true
	}
	u := user
	if i := strings.Index(u, ":"); i >= 0 {
		u = u[:i]
	}
	return u == "0" || u == "root"
}

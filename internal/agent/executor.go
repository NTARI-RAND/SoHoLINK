package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
)

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

// Executor manages Docker container lifecycle for SoHoLINK workloads.
type Executor struct {
	client *dockerclient.Client
}

// NewExecutor creates an Executor using the Docker socket and environment
// variables (DOCKER_HOST, DOCKER_TLS_VERIFY, etc.).
func NewExecutor() (*Executor, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("new executor: %w", err)
	}
	return &Executor{client: cli}, nil
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
	// Pull image only if not already present locally.
	if _, err := e.client.ImageInspect(ctx, spec.Image); err != nil {
		if !dockerclient.IsErrNotFound(err) {
			return ExecutionResult{}, fmt.Errorf("run: image inspect: %w", err)
		}
		reader, err := e.client.ImagePull(ctx, spec.Image, image.PullOptions{})
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("run: image pull: %w", err)
		}
		_, _ = io.Copy(io.Discard, reader)
		reader.Close()
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

package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
)

var (
	ErrRootContainerNotAllowed = errors.New("container runs as root")
	ErrUnknownEgressTier       = errors.New("unknown egress tier")
	ErrWorkloadOptedOut        = errors.New("workload type opted out by contributor")
)

const (
	tmpfsScratchSize = 256 * 1024 * 1024
	jobNetworkPrefix = "soholink-job-"
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
	JobID          string
	ExitCode       int
	Error          string
	TmpfsExhausted bool
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
	optout    *OptOutStore
	log       *slog.Logger
}

// NewExecutor creates an Executor using the Docker socket and environment
// variables (DOCKER_HOST, DOCKER_TLS_VERIFY, etc.). Both allowlist and optout
// must be non-nil; either being nil causes a fail-closed error at construction.
func NewExecutor(allowlist *Allowlist, optout *OptOutStore) (*Executor, error) {
	if allowlist == nil {
		return nil, fmt.Errorf("new executor: allowlist required")
	}
	if optout == nil {
		return nil, fmt.Errorf("new executor: optout store required")
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
		optout:    optout,
		log:       slog.Default(),
	}, nil
}

// newExecutorForTest builds an Executor with a caller-supplied inspector
// and no real Docker client. Tests that don't start containers can pass
// nil for the client field.
func newExecutorForTest(allowlist *Allowlist, inspector imageInspector, optout *OptOutStore) *Executor {
	return &Executor{
		client:    nil,
		inspector: inspector,
		allowlist: allowlist,
		optout:    optout,
		log:       slog.Default(),
	}
}

// Run executes the workload described by spec: allowlist check, root-user
// check, per-job network creation, hardened container start, wait, and
// cleanup. See buildHostConfig for the security baseline applied.
func (e *Executor) Run(ctx context.Context, spec ContainerSpec) (ExecutionResult, error) {
	// Allowlist check — must be the first action, before any Docker call.
	entry, err := e.allowlist.Lookup(spec.Image)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("run: %w", err)
	}

	// Opt-out gate — consult contributor consent before any Docker interaction.
	if !e.optout.IsResourceEnabled(entry.Type, "") {
		return ExecutionResult{}, fmt.Errorf("run: %w: %s", ErrWorkloadOptedOut, entry.Type)
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

	// Create per-job network. Defer registered FIRST so it runs LAST (after
	// container removal — Docker requires the network be empty before removal).
	networkID, err := e.createJobNetwork(ctx, spec.JobID, entry.Egress)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("run: %w", err)
	}
	defer func() {
		if rmErr := e.client.NetworkRemove(context.Background(), networkID); rmErr != nil {
			e.log.Warn("network remove failed",
				"job_id", spec.JobID, "network_id", networkID, "error", rmErr)
		}
	}()

	hostCfg := buildHostConfig(spec, entry)
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			jobNetworkPrefix + spec.JobID: {},
		},
	}

	resp, err := e.client.ContainerCreate(ctx,
		&container.Config{Image: spec.Image, Env: env},
		hostCfg,
		netCfg,
		nil, // platform — use host platform
		"",  // auto-generate container name
	)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("run: container create: %w", err)
	}
	containerID := resp.ID

	// Container removal — registered SECOND so it runs FIRST (before network removal).
	defer func() {
		if rmErr := e.client.ContainerRemove(context.Background(), containerID,
			container.RemoveOptions{Force: true}); rmErr != nil {
			e.log.Warn("container remove failed",
				"job_id", spec.JobID, "container_id", containerID, "error", rmErr)
		}
	}()

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
		if waitResp.StatusCode != 0 {
			result.TmpfsExhausted = e.scanStderrForENOSPC(ctx, containerID)
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

// createJobNetwork creates a dedicated Docker network for a single job.
// EgressNone produces an internal network (no host routing, no internet);
// EgressOutbound produces a standard bridge with outbound enabled.
// Per-destination egress filtering (AllowedDestinations) is not consumed here.
func (e *Executor) createJobNetwork(ctx context.Context, jobID string, tier EgressTier) (string, error) {
	opts := network.CreateOptions{Driver: "bridge"}
	switch tier {
	case EgressNone:
		opts.Internal = true
	case EgressOutbound:
		// standard bridge
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownEgressTier, tier)
	}
	resp, err := e.client.NetworkCreate(ctx, jobNetworkPrefix+jobID, opts)
	if err != nil {
		return "", fmt.Errorf("network create: %w", err)
	}
	return resp.ID, nil
}

// scanStderrForENOSPC reads the container's recent stderr and returns true
// if a "no space left on device" or ENOSPC marker is present. Best-effort
// diagnostic — read failures are logged at debug and treated as "not exhausted".
func (e *Executor) scanStderrForENOSPC(ctx context.Context, containerID string) bool {
	rc, err := e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStderr: true,
		Tail:       "100",
	})
	if err != nil {
		e.log.Debug("scan stderr: container logs read failed",
			"container_id", containerID, "error", err)
		return false
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		if strings.Contains(line, "no space left on device") || strings.Contains(line, "enospc") {
			return true
		}
	}
	return false
}

// buildHostConfig assembles the HostConfig for a job, applying the SoHoLINK
// security baseline (ReadonlyRootfs, CapDrop ALL, no-new-privileges) plus
// per-job tmpfs scratch and any device mappings declared by the allowlist
// entry. The default seccomp profile is preserved automatically by Docker —
// verified in the seccomp spike (Seccomp_filters: 2 with no-new-privileges,
// vs. 1 for seccomp=unconfined).
func buildHostConfig(spec ContainerSpec, entry *AllowlistEntry) *container.HostConfig {
	var nanoCPUs int64
	if spec.Caps.CPUEnabled {
		nanoCPUs = int64(spec.Caps.CPUCores) * 1e9
	}

	storageOpt := map[string]string{}
	if spec.Caps.StorageBytes > 0 {
		storageOpt["size"] = fmt.Sprintf("%d", spec.Caps.StorageBytes)
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeTmpfs,
			Target: "/tmp",
			TmpfsOptions: &mount.TmpfsOptions{
				SizeBytes: tmpfsScratchSize,
				Mode:      0o1777,
			},
		},
	}

	devices := deviceMountsFor(entry.DeviceAccess)
	mounts = append(mounts, devices.mounts...)

	return &container.HostConfig{
		Resources: container.Resources{
			Memory:   spec.Caps.RAMBytes,
			NanoCPUs: nanoCPUs,
			Devices:  devices.deviceMappings,
		},
		StorageOpt:     storageOpt,
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Mounts:         mounts,
	}
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

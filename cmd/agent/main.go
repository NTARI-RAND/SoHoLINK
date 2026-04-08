package main

import (
	"context"
	"encoding/hex"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var tokenSecret []byte
	if s := os.Getenv("AGENT_TOKEN_SECRET"); s != "" {
		var err error
		tokenSecret, err = hex.DecodeString(s)
		if err != nil {
			log.Fatalf("AGENT_TOKEN_SECRET: hex decode: %v", err)
		}
	}

	cfg := agent.AgentConfig{
		NodeID:           mustEnv("AGENT_NODE_ID"),
		ProviderID:       mustEnv("AGENT_PROVIDER_ID"),
		NodeClass:        mustEnv("AGENT_NODE_CLASS"),
		CountryCode:      mustEnv("AGENT_COUNTRY_CODE"),
		ControlPlaneAddr: mustEnv("AGENT_CONTROL_PLANE_ADDR"),
		SPIFFESocketPath: mustEnv("SPIFFE_ENDPOINT_SOCKET"),
		Region:           os.Getenv("AGENT_REGION"),
		TokenSecret:      tokenSecret,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	hw, err := agent.Detect(ctx)
	if err != nil {
		slog.Error("hardware detection failed", "error", err)
		os.Exit(1)
	}
	slog.Info("hardware detected",
		"cpu_cores", hw.CPUCores,
		"ram_mb", hw.RAMMB,
		"platform", hw.Platform,
	)

	heartbeatAgent, err := agent.NewHeartbeatAgent(ctx, cfg, hw)
	if err != nil {
		slog.Error("heartbeat agent init failed", "error", err)
		os.Exit(1)
	}

	executor, err := agent.NewExecutor()
	if err != nil {
		slog.Error("executor init failed", "error", err)
		os.Exit(1)
	}

	go func() {
		if err := agent.StartHeartbeatLoop(ctx, heartbeatAgent, 30*time.Second); err != nil {
			slog.Error("heartbeat loop exited", "error", err)
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		case <-ticker.C:
			jobs, err := heartbeatAgent.PollJobs(ctx)
			if err != nil {
				slog.Warn("poll jobs failed", "error", err)
				continue
			}
			for _, job := range jobs {
				go runJob(ctx, executor, cfg.ControlPlaneAddr, cfg.NodeID, tokenSecret, hw, job)
			}
		}
	}
}

// runJob executes a single job assignment. It runs the container and
// concurrently emits signed telemetry every 30 seconds until the container exits.
func runJob(
	ctx context.Context,
	executor *agent.Executor,
	controlPlaneAddr, nodeID string,
	tokenSecret []byte,
	hw agent.HardwareProfile,
	job agent.JobAssignment,
) {
	slog.Info("starting job", "job_id", job.JobID)

	// TODO(Phase 3): Replace with an mTLS client built via identity.TLSClientConfig.
	// The control plane API server wraps every route with identity.RequireSPIFFE,
	// which terminates the TLS handshake and returns 401 if the client does not
	// present a valid SPIRE-issued X.509 SVID. A plain http.Client has no client
	// certificate and will be rejected before reaching the telemetry handler.
	// Use identity.NewSource(ctx, spiffeSocketPath) + identity.TLSClientConfig to
	// build a transport that presents the node's SVID to the control plane.
	telemetryClient := &http.Client{Timeout: 15 * time.Second}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				payload, err := agent.CollectTelemetry(ctx, nodeID, job.JobID, tokenSecret)
				if err != nil {
					slog.Warn("collect telemetry failed", "job_id", job.JobID, "error", err)
					continue
				}
				if err := agent.EmitTelemetry(ctx, telemetryClient, controlPlaneAddr, payload); err != nil {
					slog.Warn("emit telemetry failed", "job_id", job.JobID, "error", err)
				}
			}
		}
	}()

	spec := agent.ContainerSpec{
		Image:    "alpine:latest", // placeholder — real image supplied by job assignment in Phase 3
		JobID:    job.JobID,
		JobToken: job.JobToken,
		Caps: agent.CapProfile{
			CPUEnabled: true,
			CPUCores:   hw.CPUCores,
			RAMBytes:   hw.RAMMB * 1024 * 1024,
		},
	}

	result, err := executor.Run(ctx, spec)
	close(done)

	if err != nil {
		slog.Error("job execution error", "job_id", job.JobID, "error", err)
		return
	}
	slog.Info("job complete",
		"job_id", result.JobID,
		"exit_code", result.ExitCode,
		"error", result.Error,
	)
}

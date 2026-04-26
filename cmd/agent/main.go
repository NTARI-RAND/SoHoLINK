package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
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

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--install":
			if err := installService(); err != nil {
				fmt.Fprintf(os.Stderr, "install service: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("SoHoLINKAgent service installed.")
			return
		case "--uninstall":
			if err := removeService(); err != nil {
				fmt.Fprintf(os.Stderr, "remove service: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("SoHoLINKAgent service removed.")
			return
		case "--service":
			if err := runAsService(); err != nil {
				log.Fatalf("service: %v", err)
			}
			return
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	runMain(ctx)
}

func runMain(ctx context.Context) {

	controlPlaneAddr := mustEnv("AGENT_CONTROL_PLANE_ADDR")
	spiffeSocket := mustEnv("SPIFFE_ENDPOINT_SOCKET")

	// First-run: if agent.conf does not exist, claim a node using the
	// registration token supplied by the installer (AGENT_REGISTER_TOKEN).
	confPath := agent.DefaultConfigPath()
	nodeCfg, err := agent.LoadConfig(confPath)
	if err != nil {
		regToken := mustEnv("AGENT_REGISTER_TOKEN")
		countryCode := mustEnv("AGENT_COUNTRY_CODE")

		hostname, _ := os.Hostname()

		// Need a temporary mTLS client to reach the control plane for claiming.
		idSrc, idErr := identity.NewSource(ctx, spiffeSocket)
		if idErr != nil {
			log.Fatalf("claim: identity source: %v", idErr)
		}
		serverID := spiffeid.RequireFromString("spiffe://soholink.org/orchestrator")
		claimClient := &http.Client{
			Transport: &http.Transport{TLSClientConfig: identity.TLSClientConfig(idSrc, serverID)},
			Timeout:   30 * time.Second,
		}

		hw0, hwErr := agent.Detect(ctx)
		if hwErr != nil {
			log.Fatalf("claim: hardware detection: %v", hwErr)
		}

		nodeCfg, err = agent.ClaimNode(ctx, claimClient, controlPlaneAddr, regToken,
			hw0, hostname, countryCode, os.Getenv("AGENT_REGION"))
		if err != nil {
			log.Fatalf("claim node: %v", err)
		}
		if err := agent.SaveConfig(confPath, nodeCfg); err != nil {
			log.Fatalf("save config: %v", err)
		}
		slog.Info("node claimed and config saved", "node_id", nodeCfg.NodeID, "path", confPath)

		// Write SPIRE agent config if the control plane returned a join token.
		if nodeCfg.SpireJoinToken != "" {
			spireDataDir := filepath.Join(filepath.Dir(confPath), "spire", "data")
			spireKeysDir := filepath.Join(filepath.Dir(confPath), "spire", "keys")
			spireConf := fmt.Sprintf(`agent {
    data_dir = %q
    log_level = "INFO"
    server_address = "spire.soholink.org"
    server_port = "8081"
    socket_path = %q
    trust_domain = "soholink.org"
    insecure_bootstrap = true
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {
            join_token = %q
        }
    }
    KeyManager "disk" {
        plugin_data {
            directory = %q
        }
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
`, spireDataDir, spiffeSocket, nodeCfg.SpireJoinToken, spireKeysDir)

			spireConfPath := filepath.Join(filepath.Dir(confPath), "spire-agent.conf")
			if mkErr := os.MkdirAll(filepath.Dir(spireConfPath), 0o700); mkErr == nil {
				if writeErr := os.WriteFile(spireConfPath, []byte(spireConf), 0o600); writeErr == nil {
					slog.Info("wrote SPIRE agent config", "path", spireConfPath)
				} else {
					slog.Warn("failed to write SPIRE agent config", "error", writeErr)
				}
			}
		}
	}

	var tokenSecret []byte
	if nodeCfg.TokenSecret != "" {
		var hexErr error
		tokenSecret, hexErr = hex.DecodeString(nodeCfg.TokenSecret)
		if hexErr != nil {
			log.Fatalf("token_secret: hex decode: %v", hexErr)
		}
	} else if s := os.Getenv("AGENT_TOKEN_SECRET"); s != "" {
		var hexErr error
		tokenSecret, hexErr = hex.DecodeString(s)
		if hexErr != nil {
			log.Fatalf("AGENT_TOKEN_SECRET: hex decode: %v", hexErr)
		}
	}

	cfg := agent.AgentConfig{
		NodeID:           nodeCfg.NodeID,
		ProviderID:       mustEnv("AGENT_PROVIDER_ID"),
		NodeClass:        mustEnv("AGENT_NODE_CLASS"),
		CountryCode:      mustEnv("AGENT_COUNTRY_CODE"),
		ControlPlaneAddr: controlPlaneAddr,
		SPIFFESocketPath: spiffeSocket,
		Region:           os.Getenv("AGENT_REGION"),
		TokenSecret:      tokenSecret,
	}

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

	// TODO(B1): use mTLS client for allowlist fetch. The signed allowlist
	// protects integrity, so plain HTTP is acceptable for now, but this
	// should be tightened when mTLS is wired more broadly.
	allowlist, err := agent.LoadAllowlistFromURL(controlPlaneAddr+"/allowlist", nil)
	if err != nil {
		slog.Error("allowlist load failed", "error", err)
		os.Exit(1)
	}

	executor, err := agent.NewExecutor(allowlist)
	if err != nil {
		slog.Error("executor init failed", "error", err)
		os.Exit(1)
	}

	telemetryClient := heartbeatAgent.NewTelemetryClient()

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
				go runJob(ctx, executor, telemetryClient, cfg.ControlPlaneAddr, cfg.NodeID, tokenSecret, hw, job)
			}
		}
	}
}

// runJob executes a single job assignment. It runs the container and
// concurrently emits signed telemetry every 30 seconds until the container exits.
func runJob(
	ctx context.Context,
	executor *agent.Executor,
	telemetryClient *http.Client,
	controlPlaneAddr, nodeID string,
	tokenSecret []byte,
	hw agent.HardwareProfile,
	job agent.JobAssignment,
) {
	slog.Info("starting job", "job_id", job.JobID)

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

	if job.Image == "" {
		slog.Warn("job has no container image — skipping execution", "job_id", job.JobID)
		return
	}

	spec := agent.ContainerSpec{
		Image:    job.Image,
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

	// Signal job completion to the control plane so it can set completed_at
	// and trigger metering. Only called on successful execution.
	completeURL := controlPlaneAddr + "/jobs/" + job.JobID + "/complete"
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, completeURL, http.NoBody)
	if reqErr == nil {
		resp, doErr := telemetryClient.Do(req)
		if doErr != nil {
			slog.Warn("complete job signal failed", "job_id", job.JobID, "error", doErr)
		} else {
			resp.Body.Close()
		}
	} else {
		slog.Warn("complete job request build failed", "job_id", job.JobID, "error", reqErr)
	}

	slog.Info("job complete",
		"job_id", result.JobID,
		"exit_code", result.ExitCode,
		"error", result.Error,
	)
}

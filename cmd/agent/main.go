package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"os/exec"
	"runtime"
	"strings"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
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

		// First-run: plain HTTPS client. /nodes/claim is token-authenticated
		// and plain-accessible — SPIRE is not running yet on a fresh device.
		claimClient := &http.Client{Timeout: 30 * time.Second}

		hw0 := detectHW(ctx)

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
			// SPIRE agent config uses the Win32 named pipe path; go-spiffe uses the npipe: URI.
			spireSocketPath := spiffeSocket
			if strings.HasPrefix(spiffeSocket, "npipe:") {
				spireSocketPath = `\\.\pipe\` + strings.TrimPrefix(spiffeSocket, "npipe:")
			}
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
`, spireDataDir, spireSocketPath, nodeCfg.SpireJoinToken, spireKeysDir)

			spireConfPath := filepath.Join(filepath.Dir(confPath), "spire-agent.conf")
			if mkErr := os.MkdirAll(filepath.Dir(spireConfPath), 0o700); mkErr == nil {
				if writeErr := os.WriteFile(spireConfPath, []byte(spireConf), 0o600); writeErr == nil {
					slog.Info("wrote SPIRE agent config", "path", spireConfPath)
				} else {
					slog.Warn("failed to write SPIRE agent config", "error", writeErr)
				}
			}
		}

		// The SoHoLINKSPIREAgent service may have failed at install time because
		// spire-agent.conf did not exist yet. Now that we have written it, start
		// the service. Ignore errors — on reinstall it may already be running.
		if runtime.GOOS == "windows" {
			out, startErr := exec.CommandContext(ctx, "sc", "start", "SoHoLINKSPIREAgent").CombinedOutput()
			slog.Info("starting SPIRE agent service",
				"output", strings.TrimSpace(string(out)),
				"error", startErr,
			)
		}
	}

	// Wait for the SPIRE Workload API socket before constructing HeartbeatAgent.
	// On first run the SPIRE agent needs time to attest to the server.
	slog.Info("waiting for SPIRE agent socket", "path", spiffeSocket)
	if err := waitForSPIRE(ctx, spiffeSocket, 90*time.Second); err != nil {
		log.Fatalf("SPIRE socket: %v", err)
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

	hw := detectHW(ctx)
	slog.Info("hardware detected",
		"cpu_cores", hw.CPUCores,
		"ram_mb", hw.RAMMB,
		"platform", hw.Platform,
	)

	// TODO(B1): use mTLS client for allowlist fetch. The signed allowlist
	// protects integrity, so plain HTTP is acceptable for now, but this
	// should be tightened when mTLS is wired more broadly.
	allowlist, err := agent.LoadAllowlistFromURL(controlPlaneAddr+"/allowlist", nil)
	if err != nil {
		slog.Error("allowlist load failed", "error", err)
		os.Exit(1)
	}

	oo, ooErr := agent.LoadOptOutFromFile(agent.OptOutCachePath())
	if ooErr != nil {
		slog.Warn("opt-out load failed, defaulting to all disabled", "error", ooErr)
		oo = agent.DefaultOptOut()
	}
	optOutStore := agent.NewOptOutStore(oo)

	heartbeatAgent, err := agent.NewHeartbeatAgent(ctx, cfg, hw, optOutStore)
	if err != nil {
		slog.Error("heartbeat agent init failed", "error", err)
		os.Exit(1)
	}

	executor, err := agent.NewExecutor(allowlist, optOutStore)
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

// waitForSPIRE polls the SPIRE Workload API socket until it accepts connections
// or the timeout elapses. Returns nil when ready.
func waitForSPIRE(ctx context.Context, socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for SPIRE socket at %s", timeout, socketPath)
		}
		tryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		src, err := identity.NewSource(tryCtx, socketPath)
		cancel()
		if err == nil {
			identity.Close(src)
			return nil
		}
		slog.Debug("SPIRE socket not yet ready", "path", socketPath, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// detectHW runs agent.Detect with a hard 15-second timeout. On timeout or
// error a minimal HardwareProfile is returned so the agent can still claim
// and heartbeat. Hung WMI or gopsutil goroutines complete in the background.
func detectHW(ctx context.Context) agent.HardwareProfile {
	type result struct {
		hw  agent.HardwareProfile
		err error
	}
	ch := make(chan result, 1)
	go func() {
		hw, err := agent.Detect(ctx)
		ch <- result{hw, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			slog.Warn("hardware detection error", "error", r.err)
		}
		return r.hw
	case <-time.After(15 * time.Second):
		slog.Warn("hardware detection timed out; using minimal profile")
		return agent.HardwareProfile{
			Platform: runtime.GOOS,
			Arch:     runtime.GOARCH,
		}
	case <-ctx.Done():
		return agent.HardwareProfile{
			Platform: runtime.GOOS,
			Arch:     runtime.GOARCH,
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
	// Validate inputs before starting the telemetry goroutine so early returns
	// cannot leak it.
	if job.Image == "" {
		slog.Warn("job has no container image — skipping execution", "job_id", job.JobID)
		return
	}

	connectionPath, err := agent.ResolveConnectionPath(job.PrinterID, hw.Printers)
	if err != nil {
		slog.Warn("assigned printer not found locally — skipping print job",
			"job_id", job.JobID, "printer_id", job.PrinterID, "error", err)
		return
	}

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

	spec := agent.ContainerSpec{
		Image:          job.Image,
		JobID:          job.JobID,
		JobToken:       job.JobToken,
		ConnectionPath: connectionPath,
		Caps: agent.CapProfile{
			CPUEnabled: true,
			CPUCores:   hw.CPUCores,
			RAMBytes:   hw.RAMMB * 1024 * 1024,
		},
	}

	// Start the container. On error, stop the telemetry goroutine and bail.
	// Note: if /started is later rejected (409), Stop tears down the container;
	// a docker-test harness is needed for proper test coverage of this path.
	ec, err := executor.Start(ctx, spec)
	if err != nil {
		slog.Error("executor start failed", "job_id", job.JobID, "error", err)
		close(done)
		return
	}

	// POST /jobs/{id}/started — confirms to orchestrator the container actually
	// started. 409 means the orchestrator has reclaimed the job (reaper fired or
	// another agent claimed it); stop the local container and bail. No /complete
	// call — orchestrator state is already reconciled.
	startedURL := controlPlaneAddr + "/jobs/" + job.JobID + "/started"
	startedReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, startedURL, nil)
	if reqErr != nil {
		slog.Error("started request build failed", "job_id", job.JobID, "error", reqErr)
		_ = executor.Stop(ctx, ec)
		close(done)
		return
	}
	startedResp, doErr := telemetryClient.Do(startedReq)
	if doErr != nil || (startedResp != nil && startedResp.StatusCode >= 300) {
		code := 0
		if startedResp != nil {
			code = startedResp.StatusCode
			startedResp.Body.Close()
		}
		slog.Warn("started rejected — stopping container",
			"job_id", job.JobID, "status", code, "error", doErr)
		_ = executor.Stop(ctx, ec)
		close(done)
		return
	}
	startedResp.Body.Close()

	result, err := executor.Wait(ctx, ec)
	close(done)

	if err != nil {
		slog.Error("job execution error", "job_id", job.JobID, "error", err)
		return
	}

	// Signal job completion to the control plane so it can set completed_at
	// and trigger metering. Only called on successful execution.
	completeURL := controlPlaneAddr + "/jobs/" + job.JobID + "/complete"
	completeBody, _ := json.Marshal(map[string]any{
		"tmpfs_exhausted": result.TmpfsExhausted,
	})
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, completeURL, bytes.NewReader(completeBody))
	if reqErr == nil {
		req.Header.Set("Content-Type", "application/json")
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

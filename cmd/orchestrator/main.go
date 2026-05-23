package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/api"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/scheduler"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func mustHex(key string) []byte {
	raw := mustEnv(key)
	b, err := hex.DecodeString(raw)
	if err != nil {
		log.Fatalf("%s: invalid hex: %v", key, err)
	}
	return b
}

func main() {
	orchestrator.MustValidateWorkloadMapping()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbURL        := mustEnv("DATABASE_URL")
	tokenSecret  := mustHex("ORCHESTRATOR_TOKEN_SECRET")
	apiAddr      := mustEnv("API_ADDR")
	metricsAddr  := mustEnv("METRICS_ADDR")
	internalAddr := mustEnv("INTERNAL_ADDR")
	spiffeSocket := mustEnv("SPIFFE_ENDPOINT_SOCKET")

	allowlistPath := os.Getenv("ALLOWLIST_PATH")
	if allowlistPath == "" {
		allowlistPath = "/etc/soholink/allowlist.json"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := store.Connect(ctx, dbURL)
	if err != nil {
		slog.Error("database connect failed", "error", err)
		os.Exit(1)
	}

	if err := store.RunMigrations(db); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")

	// Try to obtain a SPIFFE identity source with a bounded timeout.
	// If the SPIRE Workload API socket is unreachable, continue in degraded
	// mode: the API server binds plain HTTP, /health and /allowlist stay
	// reachable, SPIFFE-protected routes return 503. See TODO 12.
	idCtx, idCancel := context.WithTimeout(ctx, 5*time.Second)
	idSource, err := identity.NewSource(idCtx, spiffeSocket)
	idCancel()
	if err != nil {
		slog.Warn("SPIFFE identity source unavailable; continuing in degraded mode",
			"error", err,
			"socket", spiffeSocket,
		)
		idSource = nil
	}

	printConfirmEnabled, _ := strconv.ParseBool(os.Getenv("PRINT_CONFIRMATION_ENABLED"))

	registry := orchestrator.NewNodeRegistry()
	orch     := orchestrator.New(db, registry, tokenSecret, scheduler.Schedule, allowlistPath, printConfirmEnabled, 4*time.Hour)

	orchestrator.StartEvictionLoop(ctx, registry, 5*time.Minute)
	orch.StartDeclineRerouteLoop(ctx)

	srv         := api.New(db, registry, idSource, apiAddr, metricsAddr, allowlistPath)
	internalSrv := api.NewInternal(orch, internalAddr)

	go func() {
		slog.Info("API server listening", "addr", apiAddr)
		if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API server error", "error", err)
		}
	}()

	go func() {
		slog.Info("internal API server listening", "addr", internalAddr)
		if err := internalSrv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("internal API server error", "error", err)
		}
	}()

	go func() {
		if err := srv.StartMetrics(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server error", "error", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := internalSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("internal API server shutdown error", "error", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("API server shutdown error", "error", err)
	}
	slog.Info("orchestrator shutdown complete")
}

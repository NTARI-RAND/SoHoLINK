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

	idSource, err := identity.NewSource(ctx, spiffeSocket)
	if err != nil {
		slog.Error("SPIFFE identity source failed", "error", err)
		os.Exit(1)
	}

	registry := orchestrator.NewNodeRegistry()
	orch     := orchestrator.New(db, registry, tokenSecret, scheduler.Schedule, allowlistPath)
	_ = orch  // orchestrator used by api.New via registry; available for future direct use

	srv := api.New(db, registry, idSource, apiAddr, metricsAddr, allowlistPath)

	go func() {
		slog.Info("API server listening", "addr", apiAddr)
		if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API server error", "error", err)
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("API server shutdown error", "error", err)
	}
	slog.Info("orchestrator shutdown complete")
}

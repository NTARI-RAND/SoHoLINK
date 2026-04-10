package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/portal"
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

func mustEd25519Key(key string) ed25519.PrivateKey {
	raw := mustEnv(key)
	b, err := hex.DecodeString(raw)
	if err != nil {
		log.Fatalf("%s: invalid hex: %v", key, err)
	}
	if len(b) != ed25519.PrivateKeySize {
		log.Fatalf("%s: must be exactly %d bytes (%d hex chars), got %d bytes",
			key, ed25519.PrivateKeySize, ed25519.PrivateKeySize*2, len(b))
	}
	return ed25519.PrivateKey(b)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbURL        := mustEnv("DATABASE_URL")
	sessionPrivKey := mustEd25519Key("SESSION_PRIVATE_KEY")
	stripeKey    := mustEnv("STRIPE_SECRET_KEY")
	addr         := mustEnv("PORTAL_ADDR")
	baseURL      := mustEnv("PORTAL_BASE_URL")
	templatesDir := mustEnv("PORTAL_TEMPLATES_DIR")
	tokenSecret     := mustHex("ORCHESTRATOR_TOKEN_SECRET")
	metricsAddr     := mustEnv("METRICS_ADDR")
	webhookSecret   := mustEnv("STRIPE_WEBHOOK_SECRET")

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

	paymentClient := payment.New(stripeKey)

	registry := orchestrator.NewNodeRegistry()
	orch     := orchestrator.New(db, registry, tokenSecret, scheduler.Schedule)

	ps, err := portal.New(db, addr, sessionPrivKey, templatesDir, paymentClient, baseURL, orch, metricsAddr, webhookSecret)
	if err != nil {
		slog.Error("portal init failed", "error", err)
		os.Exit(1)
	}

	go func() {
		slog.Info("portal listening", "addr", addr)
		if err := ps.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("portal server error", "error", err)
		}
	}()

	go func() {
		if err := store.RunUptimeScorer(ctx, db, 10*time.Minute); err != nil {
			slog.Error("uptime scorer exited", "error", err)
		}
	}()

	go func() {
		if err := store.RunPayoutReleaser(ctx, db, paymentClient, time.Hour); err != nil {
			slog.Error("payout releaser exited", "error", err)
		}
	}()

	go func() {
		if err := ps.StartMetrics(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server error", "error", err)
		}
	}()

	go portal.RunNodeGauge(ctx, db)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := ps.Shutdown(shutdownCtx); err != nil {
		slog.Error("portal shutdown error", "error", err)
	}
	slog.Info("portal shutdown complete")
}

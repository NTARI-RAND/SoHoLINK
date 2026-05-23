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

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchclient"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/portal"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
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
	priv := ed25519.PrivateKey(b)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		log.Fatalf("%s: could not derive public key from private key", key)
	}
	const probe = "soholink-key-self-test-v1"
	sig := ed25519.Sign(priv, []byte(probe))
	if !ed25519.Verify(pub, []byte(probe), sig) {
		log.Fatalf("%s: sign/verify roundtrip failed; key bytes are internally inconsistent", key)
	}
	return priv
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbURL        := mustEnv("DATABASE_URL")
	sessionPrivKey := mustEd25519Key("SESSION_PRIVATE_KEY")
	stripeKey    := mustEnv("STRIPE_SECRET_KEY")
	addr         := mustEnv("PORTAL_ADDR")
	baseURL      := mustEnv("PORTAL_BASE_URL")
	templatesDir := mustEnv("PORTAL_TEMPLATES_DIR")
	metricsAddr     := mustEnv("METRICS_ADDR")
	webhookSecret   := mustEnv("STRIPE_WEBHOOK_SECRET")
	internalOrchURL := mustEnv("ORCHESTRATOR_INTERNAL_URL")

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

	orch := orchclient.New(internalOrchURL)

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

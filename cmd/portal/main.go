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

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/api"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/notify"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchclient"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/portal"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// buildNotifier constructs the mail Notifier from SMTP_* env. When SMTP_HOST is
// unset it falls back to the log/stub notifier (never dials; records what would
// be sent) so a dev/CI instance can run without a mail server. This is the seam
// wired for operator email-2FA and governance messaging.
func buildNotifier() notify.Notifier {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		slog.Warn("SMTP_HOST unset; using log/stub notifier (no mail will be sent)")
		return notify.NewLogNotifier()
	}
	return notify.NewSMTPNotifier(notify.SMTPConfig{
		Host:     host,
		Port:     os.Getenv("SMTP_PORT"),
		From:     os.Getenv("SMTP_FROM"),
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
	})
}

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

	dbURL := mustEnv("DATABASE_URL")
	sessionPrivKey := mustEd25519Key("SESSION_PRIVATE_KEY")
	stripeKey := mustEnv("STRIPE_SECRET_KEY")
	addr := mustEnv("PORTAL_ADDR")
	baseURL := mustEnv("PORTAL_BASE_URL")
	templatesDir := mustEnv("PORTAL_TEMPLATES_DIR")
	metricsAddr := mustEnv("METRICS_ADDR")
	webhookSecret := mustEnv("STRIPE_WEBHOOK_SECRET")
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

	// Public operator console (Stage 4). Construct the OnboardingServer over the
	// operator repository + mail notifier, enable its GET pages against the same
	// web/templates dir, and mount its routes on the portal mux via the option.
	// This re-parents GET / to the operator landing and exposes /operators/* and
	// /fees publicly, alongside the still-live member routes (design §11). No
	// privileged lifecycle action is here — activation/revoke/fee-signing live on
	// the local-only :8090 governance surface (governance-separation invariant).
	coordinatorID := os.Getenv("COORDINATOR_ID")
	if coordinatorID == "" {
		coordinatorID = "soholink"
	}
	operatorRepo := operator.NewRepository(db.Pool)
	onboarding := api.NewOnboardingServer(operatorRepo, buildNotifier(), nil)
	if err := onboarding.ConfigureConsole(operatorRepo, templatesDir, coordinatorID); err != nil {
		slog.Error("operator console init failed", "error", err)
		os.Exit(1)
	}

	ps, err := portal.New(db, addr, sessionPrivKey, templatesDir, paymentClient, baseURL, orch, metricsAddr, webhookSecret,
		portal.WithOperatorConsole(onboarding.RegisterRoutes))
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

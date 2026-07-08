// Command governance runs the SoHoLINK LOCAL-ONLY governance console on
// 127.0.0.1:8090. It is architecturally separated from the public portal
// (governance-separation invariant, CLAUDE.md / design §1): it binds loopback
// only, enforces a loopback-source guard on every request, holds the coordinator
// signing key (loaded from env, never hardcoded, never reachable from a public
// handler), and serves the admin operator queue / detail, fee compose+sign, and
// messaging pages plus their POST action routes.
//
// It is a separate binary from cmd/portal so the public surface can never link
// the coordinator signing key into its address space. Run it on the host and
// reach it over an SSH tunnel — the tunnel terminates as 127.0.0.1, which the
// loopback-source guard accepts.
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
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/sounding"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

// mustEd25519Key loads a 64-byte Ed25519 private key from a hex env var and
// proves the public half matches the seed via a sign-then-verify roundtrip
// (house rule: every asymmetric-key loader self-tests before returning).
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
	const probe = "soholink-coordinator-key-self-test-v1"
	if !ed25519.Verify(pub, []byte(probe), ed25519.Sign(priv, []byte(probe))) {
		log.Fatalf("%s: sign/verify roundtrip failed; key bytes are internally inconsistent", key)
	}
	return priv
}

// buildNotifier constructs the mail Notifier from SMTP_* env. When SMTP_HOST is
// unset it falls back to the log/stub notifier so governance messaging can run
// without a mail server (records what would be sent; never dials).
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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbURL := mustEnv("DATABASE_URL")
	coordKey := mustEd25519Key("COORDINATOR_SIGNING_KEY")
	templatesDir := mustEnv("GOVERNANCE_TEMPLATES_DIR")

	// The bind address MUST be loopback; NewGovernanceServer rejects anything
	// else. Default to 127.0.0.1:8090 (design §1) when unset.
	addr := os.Getenv("GOVERNANCE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8090"
	}
	coordinatorID := os.Getenv("COORDINATOR_ID")
	if coordinatorID == "" {
		coordinatorID = "soholink"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := store.Connect(ctx, dbURL)
	if err != nil {
		slog.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	defer db.Pool.Close()

	repo := operator.NewRepository(db.Pool)
	notifier := buildNotifier()

	gov, err := api.NewGovernanceServer(repo, notifier, api.GovernanceConfig{
		Addr:           addr,
		CoordinatorID:  coordinatorID,
		CoordinatorKey: coordKey,
	})
	if err != nil {
		slog.Error("governance server init failed", "error", err)
		os.Exit(1)
	}
	if err := gov.ConfigureConsole(repo, templatesDir); err != nil {
		slog.Error("governance console init failed", "error", err)
		os.Exit(1)
	}
	// Demand-sounding dashboard read model over the migration-025 hypertables.
	// LOCAL-ONLY, like the rest of the :8090 surface.
	gov.ConfigureSounding(sounding.NewReader(db))

	go func() {
		slog.Info("governance server listening (loopback only)", "addr", addr)
		if err := gov.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("governance server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := gov.Shutdown(shutdownCtx); err != nil {
		slog.Error("governance server shutdown error", "error", err)
	}
	slog.Info("governance shutdown complete")
}

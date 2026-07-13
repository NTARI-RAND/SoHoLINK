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
	"strconv"
	"syscall"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/api"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/identity"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/operator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/protocoladapter"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/scheduler"
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

func mustHex(key string) []byte {
	raw := mustEnv(key)
	b, err := hex.DecodeString(raw)
	if err != nil {
		log.Fatalf("%s: invalid hex: %v", key, err)
	}
	return b
}

// mustEd25519Key loads a 64-byte Ed25519 private key from a hex env var and
// proves the public half matches the seed via a sign-then-verify roundtrip
// (house rule: every asymmetric-key loader self-tests before returning).
// Mirrors cmd/governance/main.go's loader.
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

func main() {
	orchestrator.MustValidateWorkloadMapping()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbURL := mustEnv("DATABASE_URL")
	tokenSecret := mustHex("ORCHESTRATOR_TOKEN_SECRET")
	apiAddr := mustEnv("API_ADDR")
	metricsAddr := mustEnv("METRICS_ADDR")
	internalAddr := mustEnv("INTERNAL_ADDR")
	spiffeSocket := mustEnv("SPIFFE_ENDPOINT_SOCKET")

	// Coordinator identity for the sohocloud-protocol /v0 surface. Default
	// matches cmd/governance/main.go. NOTE (deliberate posture decision): the
	// coordinator SIGNING key enters the orchestrator process because
	// employment.Assignment offers are coordinator-signed on PollJobs; fee
	// declaration SIGNING remains exclusively on the loopback :8090
	// governance surface. The alternative — unsigned assignments — violates
	// the protocol; a signing sidecar is the only other option.
	coordinatorID := os.Getenv("COORDINATOR_ID")
	if coordinatorID == "" {
		coordinatorID = "soholink"
	}
	coordKey := mustEd25519Key("COORDINATOR_SIGNING_KEY")

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
	orch := orchestrator.New(db, registry, tokenSecret, scheduler.Schedule, allowlistPath, printConfirmEnabled, 4*time.Hour)

	// Demand-sounding telemetry (step 2): async, fire-and-forget, fail-open.
	// The sink drains until ctx is cancelled; the rung ladder is loaded once
	// (fail-open — a load error leaves an empty ladder that degrades to
	// no_capacity classification with a zero footprint). The capacity sampler
	// periodically snapshots the heartbeat-refreshed registry, keeping the
	// heartbeat hot path free of telemetry DB writes.
	demandSink := sounding.NewSink(ctx, db, sounding.DefaultConfig())
	ladder, err := sounding.LoadLadder(ctx, db)
	if err != nil {
		slog.Warn("demand-sounding: rung ladder load failed; classification degraded", "error", err)
	}
	orch.AttachDemandSounding(demandSink, ladder)
	sounding.StartCapacitySampler(ctx, registry.CapacityInputs, demandSink, time.Minute)

	orchestrator.StartEvictionLoop(ctx, registry, 5*time.Minute)
	orch.StartDeclineRerouteLoop(ctx)

	// sohocloud-protocol /v0 surface (B4 milestone): the adapter delegates to
	// the same store/orchestrator logic as the bespoke routes; the handler
	// carries its own SPIFFE middleware and mirrors degraded mode.
	repo := operator.NewRepository(db.Pool)
	adapter := protocoladapter.New(db, registry, repo, coordinatorID, coordKey, tokenSecret)
	// /v0 accepts EITHER an operator transmission (Cloudy-as-operator relays its
	// members' nodes) OR a direct node's SPIFFE SVID — selected by the operator
	// header. The selector lives in the api package; injected here so
	// protocoladapter needs no api import. Node authenticity in both paths comes
	// from each message's own signature (the adapter verifies it).
	v0Gate := func(bare, spiffeGated http.Handler) http.Handler {
		return api.OperatorOrSPIFFE(repo, coordinatorID, spiffeGated, bare)
	}
	protocolV0 := protocoladapter.NewHandler(adapter, idSource, idSource == nil, v0Gate)

	srv := api.New(db, registry, idSource, apiAddr, metricsAddr, allowlistPath, protocolV0, repo, coordinatorID)
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

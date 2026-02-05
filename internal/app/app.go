package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/accounting"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/merkle"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/radius"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/verifier"
)

// App is the main application lifecycle manager.
// It wires together all subsystems and manages startup/shutdown.
type App struct {
	Config     *config.Config
	Store      *store.Store
	Verifier   *verifier.Verifier
	PolicyEng  *policy.Engine
	Accounting *accounting.Collector
	Batcher    *merkle.Batcher
	Radius     *radius.Server

	cancelFunc context.CancelFunc
}

// New creates a new App instance, initializing all subsystems.
func New(cfg *config.Config) (*App, error) {
	app := &App{Config: cfg}

	// Initialize store
	s, err := store.NewStore(cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}
	app.Store = s

	// Initialize verifier
	credTTL := time.Duration(cfg.Auth.CredentialTTL) * time.Second
	maxNonceAge := time.Duration(cfg.Auth.MaxNonceAge) * time.Second
	app.Verifier = verifier.NewVerifier(s, credTTL, maxNonceAge)

	// Initialize policy engine
	pe, err := policy.NewEngine(cfg.Policy.Directory)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to initialize policy engine: %w", err)
	}
	app.PolicyEng = pe

	// Initialize accounting collector
	ac, err := accounting.NewCollector(cfg.AccountingDir())
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to initialize accounting collector: %w", err)
	}
	app.Accounting = ac

	// Initialize Merkle batcher
	batchInterval, err := time.ParseDuration(cfg.Merkle.BatchInterval)
	if err != nil {
		batchInterval = 1 * time.Hour
	}
	app.Batcher = merkle.NewBatcher(cfg.AccountingDir(), cfg.MerkleDir(), batchInterval)

	// Initialize RADIUS server
	app.Radius = radius.NewServer(
		cfg.Radius.AuthAddress,
		cfg.Radius.AcctAddress,
		cfg.Radius.SharedSecret,
		app.Verifier,
		app.PolicyEng,
		app.Accounting,
	)

	return app, nil
}

// Start begins all services and blocks until a shutdown signal is received.
func (a *App) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelFunc = cancel

	// Start RADIUS server
	if err := a.Radius.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start RADIUS server: %w", err)
	}

	// Start Merkle batcher in background
	go a.Batcher.Start(ctx)

	// Start nonce pruner in background
	go a.startNoncePruner(ctx)

	// Start log compressor in background
	go a.startLogCompressor(ctx)

	log.Printf("[app] SoHoLINK AAA node started")
	log.Printf("[app]   Auth:       %s", a.Radius.AuthAddr())
	log.Printf("[app]   Accounting: %s", a.Radius.AcctAddr())
	log.Printf("[app]   Data dir:   %s", a.Config.Storage.BasePath)
	log.Printf("[app]   Policies:   %s", a.Config.Policy.Directory)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("[app] received signal: %v, shutting down...", sig)

	return a.Shutdown()
}

// Shutdown performs an orderly shutdown of all subsystems.
func (a *App) Shutdown() error {
	if a.cancelFunc != nil {
		a.cancelFunc()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Stop accepting new RADIUS packets
	if err := a.Radius.Shutdown(shutdownCtx); err != nil {
		log.Printf("[app] RADIUS shutdown error: %v", err)
	}

	// 2. Flush accounting logs
	if err := a.Accounting.Close(); err != nil {
		log.Printf("[app] accounting close error: %v", err)
	}

	// 3. Build final Merkle batch
	if err := a.Batcher.BuildBatch(); err != nil {
		log.Printf("[app] final Merkle batch error: %v", err)
	}

	// 4. Close database
	if err := a.Store.Close(); err != nil {
		log.Printf("[app] store close error: %v", err)
	}

	log.Printf("[app] shutdown complete")
	return nil
}

// startNoncePruner runs periodically to clean up expired nonces.
func (a *App) startNoncePruner(ctx context.Context) {
	maxAge := time.Duration(a.Config.Auth.MaxNonceAge) * time.Second
	if maxAge == 0 {
		maxAge = 5 * time.Minute
	}

	ticker := time.NewTicker(maxAge)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruned, err := a.Store.PruneExpiredNonces(ctx, maxAge)
			if err != nil {
				log.Printf("[app] nonce pruner error: %v", err)
			} else if pruned > 0 {
				log.Printf("[app] pruned %d expired nonces", pruned)
			}
		}
	}
}

// startLogCompressor runs periodically to compress old accounting logs.
func (a *App) startLogCompressor(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maxAge := time.Duration(a.Config.Accounting.CompressAfterDays) * 24 * time.Hour
			if maxAge == 0 {
				maxAge = 7 * 24 * time.Hour
			}
			compressed, err := accounting.CompressOldLogs(a.Config.AccountingDir(), maxAge)
			if err != nil {
				log.Printf("[app] log compressor error: %v", err)
			} else if compressed > 0 {
				log.Printf("[app] compressed %d old log files", compressed)
			}
		}
	}
}

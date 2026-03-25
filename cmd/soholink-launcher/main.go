// Command soholink-launcher is the consumer-facing entry point for SoHoLINK.
//
// Double-click this exe and SoHoLINK "just works":
//   1. First run → silently installs (creates dirs, keypair, database, default config)
//   2. Starts the node (HTTP API, RADIUS, federation, scheduler — everything)
//   3. Opens the default browser to the dashboard
//   4. Stays running in the background (no console window on Windows)
//
// Build (Windows, no console window):
//
//	go build -ldflags "-H windowsgui" -o SoHoLINK.exe ./cmd/soholink-launcher/
//
// Build (any OS, with console for debugging):
//
//	go build -o soholink-launcher ./cmd/soholink-launcher/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	soholink "github.com/NetworkTheoryAppliedResearchInstitute/soholink"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/app"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/cli"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

var (
	version   = "1.0.0"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// Wire embedded defaults before anything else.
	config.SetDefaultConfig(soholink.DefaultConfigYAML)
	cli.SetDefaultPolicy(soholink.DefaultPolicyRego)

	// Set up logging to a file (no console on Windows GUI builds).
	logPath := filepath.Join(config.DefaultDataDir(), "launcher.log")
	os.MkdirAll(filepath.Dir(logPath), 0750)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Printf("[launcher] SoHoLINK %s starting", version)

	// Step 1: Load or create config.
	cfg, err := config.Load("")
	if err != nil {
		log.Printf("[launcher] no config found, performing first-time setup")
		if setupErr := firstTimeSetup(); setupErr != nil {
			fatal("First-time setup failed: %v", setupErr)
		}
		// Reload after setup.
		cfg, err = config.Load("")
		if err != nil {
			fatal("Config still unreadable after setup: %v", err)
		}
	}

	// Step 2: Check if install is needed (no database = first run).
	if _, statErr := os.Stat(cfg.DatabasePath()); os.IsNotExist(statErr) {
		log.Printf("[launcher] database not found, running install")
		if setupErr := firstTimeSetup(); setupErr != nil {
			fatal("Install failed: %v", setupErr)
		}
		// Reload config after install may have written defaults.
		cfg, err = config.Load("")
		if err != nil {
			fatal("Config unreadable after install: %v", err)
		}
	}

	// Step 3: Initialize the full application.
	application, err := app.New(cfg)
	if err != nil {
		fatal("Application init failed: %v", err)
	}
	application.SetVersion(version, commit, buildTime)

	// Step 4: Start in background, open browser, wait for shutdown signal.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- application.Start()
	}()

	// Give the server a moment to bind, then open the browser.
	go func() {
		time.Sleep(2 * time.Second)
		addr := cfg.ResourceSharing.HTTPAPIAddress
		if addr == "" {
			addr = "0.0.0.0:8080"
		}
		// Convert 0.0.0.0 to localhost for the browser.
		host, port := splitHostPort(addr)
		if host == "0.0.0.0" || host == "" {
			host = "localhost"
		}
		url := fmt.Sprintf("http://%s:%s", host, port)
		log.Printf("[launcher] opening browser: %s", url)
		openBrowser(url)
	}()

	// Wait for interrupt or fatal error.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("[launcher] received signal %v, shutting down", sig)
		cancel()
	case err := <-errCh:
		if err != nil {
			log.Printf("[launcher] application error: %v", err)
		}
	case <-ctx.Done():
	}

	log.Printf("[launcher] shutdown complete")
}

// firstTimeSetup performs silent installation: directories, keypair, database, config.
func firstTimeSetup() error {
	cfg, err := config.Load("")
	if err != nil {
		// Even defaults should give us a config — this is a fallback.
		return fmt.Errorf("cannot load even default config: %w", err)
	}

	// Guard: ensure paths are populated even if config defaults didn't apply.
	if cfg.Storage.BasePath == "" {
		cfg.Storage.BasePath = config.DefaultDataDir()
		log.Printf("[launcher] storage.base_path was empty, using default: %s", cfg.Storage.BasePath)
	}

	// Pre-create the base data directory (the parent may not exist on a fresh machine).
	if err := os.MkdirAll(cfg.Storage.BasePath, 0750); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.Storage.BasePath, err)
	}

	// Also pre-create the config directory so config.yaml can be written later.
	configDir := config.DefaultConfigDir()
	if err := os.MkdirAll(configDir, 0750); err != nil {
		return fmt.Errorf("create config dir %s: %w", configDir, err)
	}

	// Create all remaining directories.
	if err := config.EnsureDirectories(cfg); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}

	// Generate keypair if needed.
	keyPath := cfg.NodeKeyPath()
	var nodeDID string
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		pub, priv, err := did.GenerateKeypair()
		if err != nil {
			return fmt.Errorf("generate keypair: %w", err)
		}
		if err := did.SavePrivateKey(keyPath, priv); err != nil {
			return fmt.Errorf("save key: %w", err)
		}
		nodeDID = did.EncodeDIDKey(pub)
		cfg.Node.DID = nodeDID
		log.Printf("[launcher] generated node DID: %s", nodeDID)
	} else {
		pub, err := did.LoadPublicKey(keyPath)
		if err != nil {
			return fmt.Errorf("load existing key: %w", err)
		}
		nodeDID = did.EncodeDIDKey(pub)
	}

	// Initialize database.
	s, err := store.NewStore(cfg.DatabasePath())
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	ctx := context.Background()
	s.SetNodeInfo(ctx, "node_did", nodeDID)
	s.Close()

	// Write config if missing.
	configPath := filepath.Join(config.DefaultConfigDir(), "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(configPath), 0750)
		content := fmt.Sprintf(`node:
  did: "%s"
  name: ""
  location: ""

resource_sharing:
  enabled: true
  http_api_address: "0.0.0.0:8080"

radius:
  auth_address: "0.0.0.0:1812"
  acct_address: "0.0.0.0:1813"
  shared_secret: "testing123"

storage:
  base_path: "%s"

auth:
  credential_ttl: 3600
  max_nonce_age: 300

logging:
  level: "info"
  format: "json"
`, nodeDID, filepath.ToSlash(cfg.Storage.BasePath))
		os.WriteFile(configPath, []byte(content), 0640)
		log.Printf("[launcher] wrote config: %s", configPath)
	}

	// Write default policy if missing.
	policyDir := cfg.Policy.Directory
	if policyDir != "" {
		policyPath := filepath.Join(policyDir, "default.rego")
		if _, err := os.Stat(policyPath); os.IsNotExist(err) {
			if defaultRego := soholink.DefaultPolicyRego; len(defaultRego) > 0 {
				os.MkdirAll(policyDir, 0750)
				os.WriteFile(policyPath, defaultRego, 0640)
			}
		}
	}

	log.Printf("[launcher] first-time setup complete")
	return nil
}

// openBrowser opens the given URL in the user's default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[launcher] could not open browser: %v", err)
	}
}

// splitHostPort splits "host:port" without importing net (which adds weight).
func splitHostPort(addr string) (string, string) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	return addr, "8080"
}

// fatal logs an error and shows a Windows message box (or stderr on other OS).
func fatal(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[launcher] FATAL: %s", msg)

	if runtime.GOOS == "windows" {
		// Show a message box so the user sees the error even without a console.
		showWindowsError(msg)
	} else {
		fmt.Fprintf(os.Stderr, "SoHoLINK Error: %s\n", msg)
	}
	os.Exit(1)
}

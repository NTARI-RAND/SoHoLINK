package cli

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// defaultPolicyRego is embedded at build time from the main package.
var defaultPolicyRego []byte

// SetDefaultPolicy sets the embedded default Rego policy.
func SetDefaultPolicy(data []byte) {
	defaultPolicyRego = data
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Initialize a new SoHoLINK AAA node",
	Long: `Bootstrap a new SoHoLINK node by creating directories,
generating cryptographic keys, initializing the database,
and writing default configuration and policy files.`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	fmt.Println("=== SoHoLINK AAA Node Installation ===")
	fmt.Println()

	// Load configuration (with defaults)
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override data dir if specified
	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	// Step 1: Create directories
	fmt.Printf("Creating directories...\n")
	if err := config.EnsureDirectories(cfg); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}
	fmt.Printf("  Data directory: %s\n", cfg.Storage.BasePath)
	fmt.Printf("  Policy directory: %s\n", cfg.Policy.Directory)
	fmt.Printf("  Accounting directory: %s\n", cfg.AccountingDir())
	fmt.Printf("  Merkle directory: %s\n", cfg.MerkleDir())

	// Step 2: Generate node keypair
	fmt.Printf("\nGenerating node keypair...\n")
	keyPath := cfg.NodeKeyPath()

	if _, err := os.Stat(keyPath); err == nil {
		fmt.Printf("  Node key already exists at %s (skipping)\n", keyPath)
		pub, err := did.LoadPublicKey(keyPath)
		if err != nil {
			return fmt.Errorf("failed to load existing key: %w", err)
		}
		nodeDID := did.EncodeDIDKey(pub)
		fmt.Printf("  Node DID: %s\n", nodeDID)
	} else {
		pub, priv, err := did.GenerateKeypair()
		if err != nil {
			return fmt.Errorf("failed to generate keypair: %w", err)
		}
		if err := did.SavePrivateKey(keyPath, priv); err != nil {
			return fmt.Errorf("failed to save private key: %w", err)
		}
		nodeDID := did.EncodeDIDKey(pub)
		fmt.Printf("  Private key saved to: %s\n", keyPath)
		fmt.Printf("  Node DID: %s\n", nodeDID)

		// Store DID in config for future reference
		cfg.Node.DID = nodeDID
	}

	// Step 3: Initialize database
	fmt.Printf("\nInitializing database...\n")
	s, err := store.NewStore(cfg.DatabasePath())
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	    ctx := context.Background()
    // Load existing public key and handle errors
    pub, err := did.LoadPublicKey(keyPath)
    if err != nil {
        return fmt.Errorf("failed to load public key: %w", err)
    }
    nodeDID := did.EncodeDIDKey(pub)
    // Persist node_did in database; ensure closure via defer
    defer s.Close()
    if err := s.SetNodeInfo(ctx, "node_did", nodeDID); err != nil {
        return fmt.Errorf("failed to set node_did: %w", err)
    }
    if cfg.Node.Name != "" {
        if err := s.SetNodeInfo(ctx, "node_name", cfg.Node.Name); err != nil {
            return fmt.Errorf("failed to set node_name: %w", err)
        }
    }
                fmt.Printf("  Database: %s\n", cfg.DatabasePath())
    // Check for existing config; handle errors
    if _, err := os.Stat(configPath); err != nil {
        if !os.IsNotExist(err) {
            return fmt.Errorf("failed to stat config file: %w", err)
        }
        fmt.Printf("\nWriting default configuration...\n")
        if mkErr := os.MkdirAll(filepath.Dir(configPath), 0750); mkErr != nil {
            return fmt.Errorf("failed to create config directory: %w", mkErr)
        }
        // proceed to write default config below


		configContent := fmt.Sprintf(`node:
  did: "%s"
  name: "%s"
  location: "%s"

radius:
  auth_address: "0.0.0.0:1812"
  acct_address: "0.0.0.0:1813"
  shared_secret: "testing123"

storage:
  base_path: "%s"
policy:
  directory: "%s"
  default_policy: "default.rego"

auth:
  credential_ttl: 3600
  max_nonce_age: 300

accounting:
  rotation_interval: "24h"
  compress_after_days: 7

merkle:
  batch_interval: "1h"

logging:
  level: "info"
  format: "json"
`, nodeDID, cfg.Node.Name, cfg.Node.Location,
			filepath.ToSlash(cfg.Storage.BasePath),
			filepath.ToSlash(cfg.Policy.Directory))

		if err := os.WriteFile(configPath, []byte(configContent), 0640); err != nil {
			fmt.Printf("  Warning: could not write config: %v\n", err)
		} else {
			fmt.Printf("  Config: %s\n", configPath)
		}
	} else {
		fmt.Printf("\nConfiguration already exists at %s (skipping)\n", configPath)
	}

	// Step 5: Write default policy if it doesn't exist
	policyPath := filepath.Join(cfg.Policy.Directory, "default.rego")
	if _, err := os.Stat(policyPath); os.IsNotExist(err) {
		fmt.Printf("\nWriting default policy...\n")
		if defaultPolicyRego != nil && len(defaultPolicyRego) > 0 {
			if err := os.WriteFile(policyPath, defaultPolicyRego, 0640); err != nil {
				return fmt.Errorf("failed to write default policy: %w", err)
			}
		} else {
			// Fallback inline policy (OPA v1 syntax)
			fallbackPolicy := `package soholink.authz

default allow = false

allow if {
    input.user != ""
    input.did != ""
    input.authenticated == true
}
`
			if err := os.WriteFile(policyPath, []byte(fallbackPolicy), 0640); err != nil {
				return fmt.Errorf("failed to write default policy: %w", err)
			}
		}
		fmt.Printf("  Policy: %s\n", policyPath)
	} else {
		fmt.Printf("\nDefault policy already exists (skipping)\n")
	}

	// Done
	fmt.Println()
	fmt.Println("=== Installation Complete ===")
	fmt.Println()
	fmt.Printf("Node DID:  %s\n", nodeDID)
	fmt.Printf("Data dir:  %s\n", cfg.Storage.BasePath)
	fmt.Printf("Config:    %s\n", configPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit the config file to set your RADIUS shared secret")
	fmt.Println("  2. Add users:  fedaaa users add <username>")
	fmt.Println("  3. Start node: fedaaa start")

	return nil
}

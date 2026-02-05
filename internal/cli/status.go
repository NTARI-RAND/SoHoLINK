package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/merkle"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show node status and health",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	fmt.Println("=== SoHoLINK AAA Node Status ===")
	fmt.Println()

	// Node info
	keyPath := cfg.NodeKeyPath()
	if _, err := os.Stat(keyPath); err == nil {
		pub, err := did.LoadPublicKey(keyPath)
		if err != nil {
			fmt.Printf("Node DID:       ERROR (%v)\n", err)
		} else {
			fmt.Printf("Node DID:       %s\n", did.EncodeDIDKey(pub))
		}
	} else {
		fmt.Printf("Node DID:       NOT CONFIGURED (run 'fedaaa install')\n")
	}

	fmt.Printf("Node Name:      %s\n", cfg.Node.Name)
	fmt.Printf("Data Directory: %s\n", cfg.Storage.BasePath)

	// Database stats
	fmt.Println()
	dbPath := cfg.DatabasePath()
	if _, err := os.Stat(dbPath); err == nil {
		s, err := store.NewStore(dbPath)
		if err != nil {
			fmt.Printf("Database:       ERROR (%v)\n", err)
		} else {
			ctx := context.Background()
			userCount, _ := s.UserCount(ctx)
			activeCount, _ := s.ActiveUserCount(ctx)
			revCount, _ := s.RevocationCount(ctx)

			fmt.Printf("Database:       %s\n", dbPath)
			fmt.Printf("Users:          %d total, %d active\n", userCount, activeCount)
			fmt.Printf("Revocations:    %d\n", revCount)
			s.Close()
		}
	} else {
		fmt.Printf("Database:       NOT FOUND (run 'fedaaa install')\n")
	}

	// RADIUS server status
	fmt.Println()
	authUp := checkPort(cfg.Radius.AuthAddress)
	acctUp := checkPort(cfg.Radius.AcctAddress)

	if authUp {
		fmt.Printf("RADIUS Auth:    LISTENING on %s\n", cfg.Radius.AuthAddress)
	} else {
		fmt.Printf("RADIUS Auth:    NOT RUNNING (%s)\n", cfg.Radius.AuthAddress)
	}
	if acctUp {
		fmt.Printf("RADIUS Acct:    LISTENING on %s\n", cfg.Radius.AcctAddress)
	} else {
		fmt.Printf("RADIUS Acct:    NOT RUNNING (%s)\n", cfg.Radius.AcctAddress)
	}

	// Latest Merkle batch
	fmt.Println()
	batcher := merkle.NewBatcher(cfg.AccountingDir(), cfg.MerkleDir(), 0)
	batch, err := batcher.LatestBatch()
	if err != nil {
		fmt.Printf("Latest Merkle:  ERROR (%v)\n", err)
	} else if batch == nil {
		fmt.Printf("Latest Merkle:  NO BATCHES YET\n")
	} else {
		fmt.Printf("Latest Merkle:  %s\n", batch.RootHash[:32]+"...")
		fmt.Printf("  Timestamp:    %s\n", batch.Timestamp.Format(time.RFC3339))
		fmt.Printf("  Leaves:       %d\n", batch.LeafCount)
		fmt.Printf("  Source:       %s\n", batch.SourceFile)
	}

	// Policy info
	fmt.Println()
	fmt.Printf("Policy Dir:     %s\n", cfg.Policy.Directory)
	policyFiles, _ := os.ReadDir(cfg.Policy.Directory)
	regoCount := 0
	for _, f := range policyFiles {
		if !f.IsDir() && len(f.Name()) > 5 && f.Name()[len(f.Name())-5:] == ".rego" {
			regoCount++
		}
	}
	fmt.Printf("Policy Files:   %d\n", regoCount)

	return nil
}

// checkPort tries to connect to a UDP port to see if something is listening.
func checkPort(addr string) bool {
	conn, err := net.DialTimeout("udp", addr, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	// UDP "connect" always succeeds — we can't truly test without sending a packet.
	// For now, just check if the port could be opened.
	return false // Conservative: show NOT RUNNING unless we have better detection
}

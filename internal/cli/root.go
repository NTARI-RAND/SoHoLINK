package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	dataDir string
	verbose bool
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "fedaaa",
	Short: "SoHoLINK Federated Edge AAA Platform",
	Long: `SoHoLINK Federated Edge AAA (Authentication, Authorization, Accounting)
is a sovereign, offline-first network access control system for SOHO,
community, and cooperative networks.

It provides RADIUS authentication with Ed25519 credential verification,
OPA-based policy evaluation, tamper-evident accounting logs, and
Merkle tree integrity verification.`,
}

// Execute runs the root command.
func Execute(version, commit, buildTime string) {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildTime)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: platform-specific)")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "data directory (default: platform-specific)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

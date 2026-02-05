package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/app"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the SoHoLINK AAA node",
	Long: `Start the RADIUS authentication and accounting servers,
along with the Merkle batcher, nonce pruner, and log compressor.
The node runs in the foreground until interrupted (Ctrl+C).`,
	RunE: runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	application, err := app.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}

	return application.Start()
}

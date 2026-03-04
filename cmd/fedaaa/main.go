package main

import (
	"io/fs"
	"log"

	soholink "github.com/NetworkTheoryAppliedResearchInstitute/soholink"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/cli"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
)

// Build-time variables set via -ldflags
var (
	version   = "0.1.0-dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	config.SetDefaultConfig(soholink.DefaultConfigYAML)
	cli.SetDefaultPolicy(soholink.DefaultPolicyRego)

	// Register embedded OPA policies so the engine works with zero external files.
	// fs.Sub strips the "configs/policies" prefix; the engine sees "*.rego" directly.
	policySub, err := fs.Sub(soholink.PoliciesFS, "configs/policies")
	if err != nil {
		log.Fatalf("failed to sub embedded policies FS: %v", err)
	}
	policy.SetEmbeddedFS(policySub)

	// Register embedded dashboard assets so /dashboard is served from the binary.
	// fs.Sub strips the "ui/dashboard" prefix; the handler sees index.html directly.
	dashSub, err := fs.Sub(soholink.DashboardFS, "ui/dashboard")
	if err != nil {
		log.Fatalf("failed to sub embedded dashboard FS: %v", err)
	}
	cli.SetDashboardFS(dashSub)

	cli.Execute(version, commit, buildTime)
}

package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"golang.org/x/crypto/bcrypt"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	dbURL := mustEnv("DATABASE_URL")

	ctx := context.Background()

	db, err := store.Connect(ctx, dbURL)
	if err != nil {
		slog.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	defer db.Pool.Close()

	if err := store.RunMigrations(db); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}

	hwProfile := `{"CPUCores":8,"RAMMB":16384,"GPUPresent":false,"GPUModel":"","StorageGB":500,"BandwidthMbps":1000,"Platform":"linux","Arch":"amd64"}`

	for i := 1; i <= 10; i++ {
		email       := fmt.Sprintf("provider-%02d@seed.internal", i)
		displayName := fmt.Sprintf("Seed Provider %02d", i)
		stripeAcct  := fmt.Sprintf("acct_seed_%02d", i)
		hostname    := fmt.Sprintf("seed-node-%02d", i)

		var providerID string
		if err := db.Pool.QueryRow(ctx, `
			INSERT INTO providers (email, display_name, stripe_account_id, stripe_onboarding_complete, onboarding_complete, isp_tier)
			VALUES ($1, $2, $3, true, true, 'business')
			ON CONFLICT (email) DO UPDATE SET updated_at = NOW()
			RETURNING id`,
			email, displayName, stripeAcct,
		).Scan(&providerID); err != nil {
			slog.Error("insert provider failed", "i", i, "error", err)
			os.Exit(1)
		}

		var nodeID string
		if err := db.Pool.QueryRow(ctx, `
			INSERT INTO nodes (provider_id, node_class, hostname, country_code, status, uptime_pct, hardware_profile)
			VALUES ($1, 'A', $2, 'US', 'online', 99.5, $3)
			ON CONFLICT (provider_id, hostname) DO UPDATE SET updated_at = NOW()
			RETURNING id`,
			providerID, hostname, hwProfile,
		).Scan(&nodeID); err != nil {
			slog.Error("insert node failed", "i", i, "error", err)
			os.Exit(1)
		}

		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO resource_profiles (node_id, name, is_default, cpu_enabled, ram_pct, storage_gb, bandwidth_mbps, price_multiplier)
			VALUES ($1, 'default', true, true, 80, 400, 500, 1.0)
			ON CONFLICT DO NOTHING`,
			nodeID,
		); err != nil {
			slog.Error("insert resource profile failed", "i", i, "error", err)
			os.Exit(1)
		}
	}

	for i := 1; i <= 10; i++ {
		email       := fmt.Sprintf("consumer-%02d@seed.internal", i)
		displayName := fmt.Sprintf("Seed Consumer %02d", i)

		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO consumers (email, display_name)
			VALUES ($1, $2)
			ON CONFLICT (email) DO NOTHING`,
			email, displayName,
		); err != nil {
			slog.Error("insert consumer failed", "i", i, "error", err)
			os.Exit(1)
		}
	}

	// Set password_hash for all seed consumers so load tests work out of the box.
	hash, err := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt failed", "error", err)
		os.Exit(1)
	}
	for i := 1; i <= 10; i++ {
		email := fmt.Sprintf("consumer-%02d@seed.internal", i)
		if _, err := db.Pool.Exec(ctx,
			`UPDATE consumers SET password_hash = $1 WHERE email = $2`,
			string(hash), email,
		); err != nil {
			slog.Error("set consumer password failed", "i", i, "error", err)
			os.Exit(1)
		}
	}

	slog.Info("seed complete", "providers", 10, "consumers", 10, "nodes", 10)
}

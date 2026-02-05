package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/sha3"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage authorization policies",
}

var policyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active policies",
	RunE:  runPolicyList,
}

var policyTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test policy evaluation",
	Long:  `Evaluate the authorization policy with test input and show the result.`,
	RunE:  runPolicyTest,
}

var (
	testUser     string
	testDID      string
	testRole     string
	testResource string
	testInput    string
)

func init() {
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(policyListCmd)
	policyCmd.AddCommand(policyTestCmd)

	policyTestCmd.Flags().StringVar(&testUser, "user", "testuser", "test username")
	policyTestCmd.Flags().StringVar(&testDID, "did", "did:key:z6MkTestUser", "test user DID")
	policyTestCmd.Flags().StringVar(&testRole, "role", "basic", "test user role")
	policyTestCmd.Flags().StringVar(&testResource, "resource", "network_access", "test resource")
	policyTestCmd.Flags().StringVar(&testInput, "input", "", "custom JSON input (overrides other flags)")
}

func runPolicyList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	policyDir := cfg.Policy.Directory
	fmt.Printf("Policy directory: %s\n\n", policyDir)

	entries, err := os.ReadDir(policyDir)
	if err != nil {
		return fmt.Errorf("failed to read policy directory: %w", err)
	}

	found := false
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".rego" {
			continue
		}

		found = true
		path := filepath.Join(policyDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("  %s  ERROR: %v\n", entry.Name(), err)
			continue
		}

		// Compute SHA3-256 hash
		h := sha3.New256()
		h.Write(data)
		hash := hex.EncodeToString(h.Sum(nil))

		info, _ := entry.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}

		fmt.Printf("  %s\n", entry.Name())
		fmt.Printf("    SHA3-256: %s\n", hash[:32]+"...")
		fmt.Printf("    Size:     %d bytes\n", size)
	}

	if !found {
		fmt.Println("No .rego policy files found.")
	}

	return nil
}

func runPolicyTest(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	// Initialize policy engine
	engine, err := policy.NewEngine(cfg.Policy.Directory)
	if err != nil {
		return fmt.Errorf("failed to initialize policy engine: %w", err)
	}

	// Build input
	var input *policy.AuthzInput

	if testInput != "" {
		// Parse custom JSON input
		input = &policy.AuthzInput{}
		if err := json.Unmarshal([]byte(testInput), input); err != nil {
			return fmt.Errorf("failed to parse input JSON: %w", err)
		}
	} else {
		input = &policy.AuthzInput{
			User:          testUser,
			DID:           testDID,
			Role:          testRole,
			Authenticated: true,
			Resource:      testResource,
			Timestamp:     time.Now(),
		}
	}

	fmt.Println("Policy Evaluation Test")
	fmt.Println("======================")
	fmt.Println()
	fmt.Println("Input:")
	inputJSON, _ := json.MarshalIndent(input, "  ", "  ")
	fmt.Printf("  %s\n\n", string(inputJSON))

	// Evaluate
	result, err := engine.Evaluate(context.Background(), input)
	if err != nil {
		return fmt.Errorf("policy evaluation failed: %w", err)
	}

	fmt.Println("Result:")
	if result.Allow {
		fmt.Println("  Decision: ALLOW")
	} else {
		fmt.Println("  Decision: DENY")
		if len(result.DenyReasons) > 0 {
			fmt.Println("  Reasons:")
			for _, r := range result.DenyReasons {
				fmt.Printf("    - %s\n", r)
			}
		}
	}

	return nil
}

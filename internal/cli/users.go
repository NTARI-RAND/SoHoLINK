package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/did"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/verifier"
)

var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "Manage users",
}

var usersAddCmd = &cobra.Command{
	Use:   "add <username>",
	Short: "Add a new user",
	Args:  cobra.ExactArgs(1),
	RunE:  runUsersAdd,
}

var usersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	RunE:  runUsersList,
}

var usersRevokeCmd = &cobra.Command{
	Use:   "revoke <username>",
	Short: "Revoke a user's access",
	Args:  cobra.ExactArgs(1),
	RunE:  runUsersRevoke,
}

var userRole string
var revokeReason string

func init() {
	rootCmd.AddCommand(usersCmd)
	usersCmd.AddCommand(usersAddCmd)
	usersCmd.AddCommand(usersListCmd)
	usersCmd.AddCommand(usersRevokeCmd)

	usersAddCmd.Flags().StringVar(&userRole, "role", "basic", "user role (basic, premium, admin)")
	usersRevokeCmd.Flags().StringVar(&revokeReason, "reason", "manual revocation", "reason for revocation")
}

func openStore() (*store.Store, *config.Config, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}
	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	s, err := store.NewStore(cfg.DatabasePath())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	return s, cfg, nil
}

func runUsersAdd(cmd *cobra.Command, args []string) error {
	username := args[0]

	s, cfg, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	ctx := context.Background()

	// Check if user already exists
	existing, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("failed to check existing user: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("user '%s' already exists (DID: %s)", username, existing.DID)
	}

	// Generate keypair for user
	pub, priv, err := did.GenerateKeypair()
	if err != nil {
		return fmt.Errorf("failed to generate keypair: %w", err)
	}

	userDID := did.EncodeDIDKey(pub)

	// Store user
	if err := s.AddUser(ctx, username, userDID, pub, userRole); err != nil {
		return fmt.Errorf("failed to add user: %w", err)
	}

	// Save user's private key
	keyPath := fmt.Sprintf("%s/keys/%s.pem", cfg.Storage.BasePath, username)
	os.MkdirAll(fmt.Sprintf("%s/keys", cfg.Storage.BasePath), 0700)
	if err := did.SavePrivateKey(keyPath, priv); err != nil {
		return fmt.Errorf("failed to save user key: %w", err)
	}

	// Create a sample credential token (now includes username for security)
	token, err := verifier.CreateCredential(username, priv)
	if err != nil {
		return fmt.Errorf("failed to create credential: %w", err)
	}

	fmt.Println("User created successfully!")
	fmt.Println()
	fmt.Printf("Username:    %s\n", username)
	fmt.Printf("DID:         %s\n", userDID)
	fmt.Printf("Role:        %s\n", userRole)
	fmt.Printf("Private Key: %s\n", keyPath)
	fmt.Println()
	fmt.Println("Sample credential token (for testing):")
	fmt.Printf("  %s\n", token)
	fmt.Println()
	fmt.Println("Test with radclient:")
	fmt.Printf("  echo \"User-Name=%s,User-Password=%s\" | radclient -x localhost:1812 auth testing123\n", username, token)

	return nil
}

func runUsersList(cmd *cobra.Command, args []string) error {
	s, _, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	users, err := s.ListUsers(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(users) == 0 {
		fmt.Println("No users found. Add one with: fedaaa users add <username>")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Username", "DID", "Role", "Status", "Created")

	for _, u := range users {
		status := "active"
		if u.RevokedAt.Valid {
			status = "REVOKED"
		}

		// Truncate DID for display
		displayDID := u.DID
		if len(displayDID) > 24 {
			displayDID = displayDID[:24] + "..."
		}

		// Parse and format created time
		created := u.CreatedAt
		if len(created) > 19 {
			created = created[:19]
		}

		table.Append(u.Username, displayDID, u.Role, status, created)
	}

	table.Render()
	fmt.Printf("\nTotal: %d users\n", len(users))
	return nil
}

func runUsersRevoke(cmd *cobra.Command, args []string) error {
	username := args[0]

	s, _, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	ctx := context.Background()

	// Check user exists
	user, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user '%s' not found", username)
	}
	if user.RevokedAt.Valid {
		return fmt.Errorf("user '%s' is already revoked", username)
	}

	if err := s.RevokeUser(ctx, username, revokeReason); err != nil {
		return fmt.Errorf("failed to revoke user: %w", err)
	}

	fmt.Printf("User '%s' has been revoked.\n", username)
	fmt.Printf("Reason: %s\n", revokeReason)
	fmt.Printf("DID: %s\n", user.DID)
	fmt.Println()
	fmt.Println("The revocation takes effect immediately for new authentication attempts.")

	return nil
}

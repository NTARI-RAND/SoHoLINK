package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCmdExists(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd should not be nil")
	}
}

func TestRootCmdUse(t *testing.T) {
	if rootCmd.Use != "fedaaa" {
		t.Errorf("rootCmd.Use = %q, want %q", rootCmd.Use, "fedaaa")
	}
}

func TestRootCmdShort(t *testing.T) {
	if rootCmd.Short == "" {
		t.Error("rootCmd.Short should not be empty")
	}
}

func TestRootCmdLong(t *testing.T) {
	if rootCmd.Long == "" {
		t.Error("rootCmd.Long should not be empty")
	}
}

func TestRootCmdHasPersistentFlags(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
	}{
		{name: "config flag", flagName: "config"},
		{name: "data-dir flag", flagName: "data-dir"},
		{name: "verbose flag", flagName: "verbose"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := rootCmd.PersistentFlags().Lookup(tc.flagName)
			if f == nil {
				t.Errorf("persistent flag %q not found", tc.flagName)
			}
		})
	}
}

func TestVerboseFlagDefault(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("verbose")
	if f == nil {
		t.Fatal("verbose flag not found")
	}
	if f.DefValue != "false" {
		t.Errorf("verbose default = %q, want %q", f.DefValue, "false")
	}
}

func TestVerboseFlagShorthand(t *testing.T) {
	f := rootCmd.PersistentFlags().ShorthandLookup("v")
	if f == nil {
		t.Fatal("verbose shorthand 'v' not found")
	}
	if f.Name != "verbose" {
		t.Errorf("shorthand 'v' maps to %q, want %q", f.Name, "verbose")
	}
}

func TestStartCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "start" {
			found = true
			break
		}
	}
	if !found {
		t.Error("start command should be registered as subcommand of root")
	}
}

func TestStartCmdShort(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "start" {
			if cmd.Short == "" {
				t.Error("start command Short should not be empty")
			}
			return
		}
	}
	t.Error("start command not found")
}

func TestUsersCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "users" {
			found = true
			break
		}
	}
	if !found {
		t.Error("users command should be registered as subcommand of root")
	}
}

func TestUsersSubcommands(t *testing.T) {
	var usersCmdRef = findCommand("users")
	if usersCmdRef == nil {
		t.Fatal("users command not found")
	}

	tests := []struct {
		name string
		use  string
	}{
		{name: "add subcommand", use: "add <username>"},
		{name: "list subcommand", use: "list"},
		{name: "revoke subcommand", use: "revoke <username>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, sub := range usersCmdRef.Commands() {
				if sub.Use == tc.use {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("subcommand %q not found under users", tc.use)
			}
		})
	}
}

func TestUsersAddHasRoleFlag(t *testing.T) {
	usersCmdRef := findCommand("users")
	if usersCmdRef == nil {
		t.Fatal("users command not found")
	}

	var addCmd = findSubcommand(usersCmdRef, "add <username>")
	if addCmd == nil {
		t.Fatal("users add command not found")
	}

	f := addCmd.Flags().Lookup("role")
	if f == nil {
		t.Fatal("role flag not found on users add")
	}
	if f.DefValue != "basic" {
		t.Errorf("role flag default = %q, want %q", f.DefValue, "basic")
	}
}

func TestUsersRevokeHasReasonFlag(t *testing.T) {
	usersCmdRef := findCommand("users")
	if usersCmdRef == nil {
		t.Fatal("users command not found")
	}

	var revokeCmd = findSubcommand(usersCmdRef, "revoke <username>")
	if revokeCmd == nil {
		t.Fatal("users revoke command not found")
	}

	f := revokeCmd.Flags().Lookup("reason")
	if f == nil {
		t.Fatal("reason flag not found on users revoke")
	}
	if f.DefValue != "manual revocation" {
		t.Errorf("reason flag default = %q, want %q", f.DefValue, "manual revocation")
	}
}

func TestUsersAddRequiresExactlyOneArg(t *testing.T) {
	usersCmdRef := findCommand("users")
	if usersCmdRef == nil {
		t.Fatal("users command not found")
	}

	addCmd := findSubcommand(usersCmdRef, "add <username>")
	if addCmd == nil {
		t.Fatal("users add command not found")
	}

	if addCmd.Args == nil {
		t.Error("users add should have Args validator set")
	}
}

func TestUsersRevokeRequiresExactlyOneArg(t *testing.T) {
	usersCmdRef := findCommand("users")
	if usersCmdRef == nil {
		t.Fatal("users command not found")
	}

	revokeCmd := findSubcommand(usersCmdRef, "revoke <username>")
	if revokeCmd == nil {
		t.Fatal("users revoke command not found")
	}

	if revokeCmd.Args == nil {
		t.Error("users revoke should have Args validator set")
	}
}

func TestRootCmdHasSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()
	if len(cmds) == 0 {
		t.Error("rootCmd should have subcommands registered")
	}
}

func findCommand(use string) *cobra.Command {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == use {
			return cmd
		}
	}
	return nil
}

func findSubcommand(parent *cobra.Command, use string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Use == use {
			return cmd
		}
	}
	return nil
}

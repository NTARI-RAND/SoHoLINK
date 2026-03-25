package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// minimalYAML is a minimal valid YAML config for testing.
const minimalYAML = `
node:
  name: "test-node"
radius:
  enabled: false
  auth_address: "0.0.0.0:1812"
  acct_address: "0.0.0.0:1813"
  shared_secret: "secret123"
storage:
  base_path: "/tmp/soholink-test"
logging:
  level: "info"
  format: "text"
`

func TestDefaultDataDir(t *testing.T) {
	dir := DefaultDataDir()
	if dir == "" {
		t.Fatal("DefaultDataDir returned empty string")
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(dir, "SoHoLINK") {
			t.Errorf("expected Windows data dir to contain 'SoHoLINK', got %q", dir)
		}
		if !strings.HasSuffix(dir, filepath.Join("SoHoLINK", "data")) {
			t.Errorf("expected Windows data dir to end with SoHoLINK\\data, got %q", dir)
		}
	} else {
		if dir != "/var/lib/soholink" {
			t.Errorf("expected /var/lib/soholink on non-Windows, got %q", dir)
		}
	}
}

func TestDefaultConfigDir(t *testing.T) {
	dir := DefaultConfigDir()
	if dir == "" {
		t.Fatal("DefaultConfigDir returned empty string")
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(dir, "SoHoLINK") {
			t.Errorf("expected Windows config dir to contain 'SoHoLINK', got %q", dir)
		}
	} else {
		if dir != "/etc/soholink" {
			t.Errorf("expected /etc/soholink on non-Windows, got %q", dir)
		}
	}
}

func TestDefaultPolicyDir(t *testing.T) {
	dir := DefaultPolicyDir()
	expected := filepath.Join(DefaultConfigDir(), "policies")
	if dir != expected {
		t.Errorf("DefaultPolicyDir() = %q, want %q", dir, expected)
	}
}

func TestLoad_NoDefaultConfigSet(t *testing.T) {
	// Reset the global to ensure Load fails without SetDefaultConfig.
	old := defaultConfigYAML
	defaultConfigYAML = nil
	defer func() { defaultConfigYAML = old }()

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error when defaultConfigYAML is nil")
	}
	if !strings.Contains(err.Error(), "default config not initialized") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoad_WithDefaults(t *testing.T) {
	SetDefaultConfig([]byte(minimalYAML))
	defer func() { defaultConfigYAML = nil }()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load('') failed: %v", err)
	}
	if cfg.Node.Name != "test-node" {
		t.Errorf("Node.Name = %q, want %q", cfg.Node.Name, "test-node")
	}
	if cfg.Radius.SharedSecret != "secret123" {
		t.Errorf("Radius.SharedSecret = %q, want %q", cfg.Radius.SharedSecret, "secret123")
	}
}

func TestLoad_WithConfigFile(t *testing.T) {
	SetDefaultConfig([]byte(minimalYAML))
	defer func() { defaultConfigYAML = nil }()

	// Write a config file that overrides node name.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "override.yaml")
	override := `
node:
  name: "overridden-node"
`
	if err := os.WriteFile(cfgPath, []byte(override), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(%q) failed: %v", cfgPath, err)
	}
	if cfg.Node.Name != "overridden-node" {
		t.Errorf("Node.Name = %q, want %q", cfg.Node.Name, "overridden-node")
	}
	// SharedSecret should still come from defaults.
	if cfg.Radius.SharedSecret != "secret123" {
		t.Errorf("Radius.SharedSecret = %q, want %q (from defaults)", cfg.Radius.SharedSecret, "secret123")
	}
}

func TestLoad_InvalidConfigPath(t *testing.T) {
	SetDefaultConfig([]byte(minimalYAML))
	defer func() { defaultConfigYAML = nil }()

	_, err := Load("/nonexistent/path/to/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent config file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	old := defaultConfigYAML
	defer func() { defaultConfigYAML = old }()

	SetDefaultConfig([]byte("{{{{invalid yaml"))
	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "failed to parse default config") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSave_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "subdir", "config.yaml")

	cfg := &Config{
		Node: NodeConfig{Name: "my-node"},
		Radius: RadiusConfig{
			AuthAddress:  "0.0.0.0:1812",
			AcctAddress:  "0.0.0.0:1813",
			SharedSecret: "test-secret",
		},
		Storage: StorageConfig{BasePath: "/tmp/test-data"},
		Updates: UpdatesConfig{Enabled: true},
	}

	if err := Save(cfg, cfgPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file was created with correct permissions.
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("saved file does not exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("saved file is empty")
	}

	// Read it back with Load.
	SetDefaultConfig([]byte(minimalYAML))
	defer func() { defaultConfigYAML = nil }()

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load round-trip failed: %v", err)
	}
	if loaded.Node.Name != "my-node" {
		t.Errorf("round-trip Node.Name = %q, want %q", loaded.Node.Name, "my-node")
	}
	if loaded.Storage.BasePath != "/tmp/test-data" {
		t.Errorf("round-trip Storage.BasePath = %q, want %q", loaded.Storage.BasePath, "/tmp/test-data")
	}
}

func TestSave_CreatesParentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "deep", "nested", "dir", "config.yaml")

	cfg := &Config{Node: NodeConfig{Name: "nested"}}
	if err := Save(cfg, cfgPath); err != nil {
		t.Fatalf("Save failed to create parent dirs: %v", err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("file not created at nested path: %v", err)
	}
}

func TestEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "data")
	policyDir := filepath.Join(tmpDir, "policies")

	cfg := &Config{
		Storage: StorageConfig{BasePath: basePath},
		Policy:  PolicyConfig{Directory: policyDir},
	}

	if err := EnsureDirectories(cfg); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	expected := []string{
		basePath,
		filepath.Join(basePath, "accounting"),
		filepath.Join(basePath, "merkle"),
		policyDir,
	}
	for _, dir := range expected {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %q to be a directory", dir)
		}
	}
}

func TestEnsureDirectories_EmptyPolicyDir(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "data")

	cfg := &Config{
		Storage: StorageConfig{BasePath: basePath},
		Policy:  PolicyConfig{Directory: ""}, // empty should be skipped
	}

	if err := EnsureDirectories(cfg); err != nil {
		t.Fatalf("EnsureDirectories failed with empty policy dir: %v", err)
	}
}

func TestConfigPathMethods(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{BasePath: "/data/soholink"},
	}

	tests := []struct {
		name   string
		got    string
		want   string
	}{
		{"DatabasePath", cfg.DatabasePath(), filepath.Join("/data/soholink", "soholink.db")},
		{"NodeKeyPath", cfg.NodeKeyPath(), filepath.Join("/data/soholink", "node_key.pem")},
		{"AccountingDir", cfg.AccountingDir(), filepath.Join("/data/soholink", "accounting")},
		{"MerkleDir", cfg.MerkleDir(), filepath.Join("/data/soholink", "merkle")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestComputeWorkDir_Default(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{BasePath: "/data"},
	}
	got := cfg.ComputeWorkDir()
	want := filepath.Join("/data", "compute")
	if got != want {
		t.Errorf("ComputeWorkDir() = %q, want %q", got, want)
	}
}

func TestComputeWorkDir_Custom(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{BasePath: "/data"},
		ResourceSharing: ResourceSharingConfig{
			Compute: ComputeConfig{WorkDir: "/custom/work"},
		},
	}
	got := cfg.ComputeWorkDir()
	if got != "/custom/work" {
		t.Errorf("ComputeWorkDir() with custom = %q, want %q", got, "/custom/work")
	}
}

func TestStoragePoolDir_Default(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{BasePath: "/data"},
	}
	got := cfg.StoragePoolDir()
	want := filepath.Join("/data", "storage_pool")
	if got != want {
		t.Errorf("StoragePoolDir() = %q, want %q", got, want)
	}
}

func TestStoragePoolDir_Custom(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{BasePath: "/data"},
		ResourceSharing: ResourceSharingConfig{
			StoragePool: StoragePoolConfig{BaseDir: "/custom/pool"},
		},
	}
	got := cfg.StoragePoolDir()
	if got != "/custom/pool" {
		t.Errorf("StoragePoolDir() with custom = %q, want %q", got, "/custom/pool")
	}
}

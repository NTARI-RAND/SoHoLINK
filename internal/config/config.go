package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"
)

// defaultConfigYAML is the embedded default configuration.
// It is set by the SetDefaultConfig function from the main package
// which has access to the configs directory via go:embed.
var defaultConfigYAML []byte

// SetDefaultConfig sets the embedded default configuration YAML.
// This must be called before Load().
func SetDefaultConfig(data []byte) {
	defaultConfigYAML = data
}

// Config holds all configuration for the SoHoLINK node.
type Config struct {
	Node       NodeConfig       `mapstructure:"node"`
	Radius     RadiusConfig     `mapstructure:"radius"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Policy     PolicyConfig     `mapstructure:"policy"`
	Accounting AccountingConfig `mapstructure:"accounting"`
	Merkle     MerkleConfig     `mapstructure:"merkle"`
	Logging    LoggingConfig    `mapstructure:"logging"`
}

type NodeConfig struct {
	DID      string `mapstructure:"did"`
	Name     string `mapstructure:"name"`
	Location string `mapstructure:"location"`
}

type RadiusConfig struct {
	AuthAddress  string `mapstructure:"auth_address"`
	AcctAddress  string `mapstructure:"acct_address"`
	SharedSecret string `mapstructure:"shared_secret"`
}

type AuthConfig struct {
	CredentialTTL      int `mapstructure:"credential_ttl"`
	MaxNonceAge        int `mapstructure:"max_nonce_age"`
	ClockSkewTolerance int `mapstructure:"clock_skew_tolerance"` // seconds, default 300 (5 minutes)
}

type StorageConfig struct {
	BasePath string `mapstructure:"base_path"`
}

type PolicyConfig struct {
	Directory     string `mapstructure:"directory"`
	DefaultPolicy string `mapstructure:"default_policy"`
}

type AccountingConfig struct {
	RotationInterval string `mapstructure:"rotation_interval"`
	CompressAfterDays int  `mapstructure:"compress_after_days"`
}

type MerkleConfig struct {
	BatchInterval string `mapstructure:"batch_interval"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// DefaultDataDir returns the platform-specific default data directory.
func DefaultDataDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(appData, "SoHoLINK", "data")
	default:
		return "/var/lib/soholink"
	}
}

// DefaultConfigDir returns the platform-specific default config directory.
func DefaultConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "SoHoLINK")
	default:
		return "/etc/soholink"
	}
}

// DefaultPolicyDir returns the platform-specific default policy directory.
func DefaultPolicyDir() string {
	return filepath.Join(DefaultConfigDir(), "policies")
}

// Load reads configuration from file, environment, and defaults.
// configFile can be empty to use platform defaults.
func Load(configFile string) (*Config, error) {
	v := viper.New()

	// Load embedded defaults
	if defaultConfigYAML == nil {
		return nil, fmt.Errorf("default config not initialized; call SetDefaultConfig first")
	}
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(defaultConfigYAML))); err != nil {
		return nil, fmt.Errorf("failed to parse default config: %w", err)
	}

	// Set platform-aware defaults for paths
	v.SetDefault("storage.base_path", DefaultDataDir())
	v.SetDefault("policy.directory", DefaultPolicyDir())

	// Load config file if specified or exists at default location
	if configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.MergeInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", configFile, err)
		}
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(DefaultConfigDir())
		v.AddConfigPath(".")
		// Silently ignore if no config file found (use defaults)
		_ = v.MergeInConfig()
	}

	// Environment variable overrides (prefix: SOHOLINK_)
	v.SetEnvPrefix("SOHOLINK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// EnsureDirectories creates all required directories for the node.
func EnsureDirectories(cfg *Config) error {
	dirs := []string{
		cfg.Storage.BasePath,
		filepath.Join(cfg.Storage.BasePath, "accounting"),
		filepath.Join(cfg.Storage.BasePath, "merkle"),
		cfg.Policy.Directory,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// DatabasePath returns the full path to the SQLite database.
func (c *Config) DatabasePath() string {
	return filepath.Join(c.Storage.BasePath, "soholink.db")
}

// NodeKeyPath returns the full path to the node's private key.
func (c *Config) NodeKeyPath() string {
	return filepath.Join(c.Storage.BasePath, "node_key.pem")
}

// AccountingDir returns the accounting log directory.
func (c *Config) AccountingDir() string {
	return filepath.Join(c.Storage.BasePath, "accounting")
}

// MerkleDir returns the Merkle batch directory.
func (c *Config) MerkleDir() string {
	return filepath.Join(c.Storage.BasePath, "merkle")
}

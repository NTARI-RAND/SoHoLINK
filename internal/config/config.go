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
	Node            NodeConfig            `mapstructure:"node"`
	Radius          RadiusConfig          `mapstructure:"radius"`
	Auth            AuthConfig            `mapstructure:"auth"`
	Storage         StorageConfig         `mapstructure:"storage"`
	Policy          PolicyConfig          `mapstructure:"policy"`
	Accounting      AccountingConfig      `mapstructure:"accounting"`
	Merkle          MerkleConfig          `mapstructure:"merkle"`
	Logging         LoggingConfig         `mapstructure:"logging"`
	ResourceSharing ResourceSharingConfig `mapstructure:"resource_sharing"`
	Payment         PaymentConfig         `mapstructure:"payment"`
	LBTAS           LBTASConfig           `mapstructure:"lbtas"`
	Central         CentralConfig         `mapstructure:"central"`
	P2P             P2PConfig             `mapstructure:"p2p"`
	Rental          RentalConfig          `mapstructure:"rental"`
	Orchestration   OrchestrationConfig   `mapstructure:"orchestration"`
	Services        ManagedServicesConfig `mapstructure:"services"`
	CDN             CDNConfig             `mapstructure:"cdn"`
	SLA             SLAConfig             `mapstructure:"sla"`
	Hypervisor      HypervisorConfig      `mapstructure:"hypervisor"`
	Blockchain      BlockchainConfig      `mapstructure:"blockchain"`
	Federation      FederationConfig      `mapstructure:"federation"`
	Updates         UpdatesConfig         `mapstructure:"updates"`
}

// BlockchainConfig holds settings for the local blockchain anchoring.
type BlockchainConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type NodeConfig struct {
	DID      string `mapstructure:"did"`
	Name     string `mapstructure:"name"`
	Location string `mapstructure:"location"`
}

type RadiusConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
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
	BasePath    string `mapstructure:"base_path"`
	IPFSAPIAddr string `mapstructure:"ipfs_api_addr"` // e.g. "http://127.0.0.1:5001"; "" disables IPFS announcements
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

// ResourceSharingConfig holds settings for all resource sharing subsystems.
type ResourceSharingConfig struct {
	Enabled        bool                `mapstructure:"enabled"`
	HTTPAPIAddress string              `mapstructure:"http_api_address"`
	PortalAddress  string              `mapstructure:"portal_address"`
	Compute        ComputeConfig       `mapstructure:"compute"`
	StoragePool    StoragePoolConfig   `mapstructure:"storage_pool"`
	Printer        PrinterConfig       `mapstructure:"printer"`
	Portal         PortalConfig        `mapstructure:"portal"`

	// TLS: paths to PEM-encoded certificate and private key.
	// When both are set, the API server uses HTTPS (ListenAndServeTLS).
	TLSCertFile string `mapstructure:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file"`

	// AllowedOrigins controls the CORS Access-Control-Allow-Origin header.
	// Use ["*"] for open-access (default); restrict to specific origins in production.
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// ComputeConfig holds settings for the compute sharing subsystem.
type ComputeConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	Workers         int    `mapstructure:"workers"`
	WorkDir         string `mapstructure:"work_dir"`
	MaxCPUPerJob    int    `mapstructure:"max_cpu_per_job"`
	MaxMemoryPerJob int    `mapstructure:"max_memory_per_job"`
	MaxTimeout      int    `mapstructure:"max_timeout"`
}

// StoragePoolConfig holds settings for the shared storage subsystem.
type StoragePoolConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	BaseDir         string `mapstructure:"base_dir"`
	MaxFileSize     int64  `mapstructure:"max_file_size"`
	ContentScanning bool   `mapstructure:"content_scanning"`
	ClamAVSocket    string `mapstructure:"clamav_socket"`
}

// PrinterConfig holds settings for the printer spooling subsystem.
type PrinterConfig struct {
	Enabled        bool `mapstructure:"enabled"`
	MaxHotendTemp  int  `mapstructure:"max_hotend_temp"`
	MaxBedTemp     int  `mapstructure:"max_bed_temp"`
	MaxFeedRate    int  `mapstructure:"max_feedrate"`
	GCodeValidation bool `mapstructure:"gcode_validation"`
}

// PortalConfig holds settings for the captive portal subsystem.
type PortalConfig struct {
	Enabled        bool `mapstructure:"enabled"`
	SessionTimeout int  `mapstructure:"session_timeout"`
}

// PaymentConfig holds settings for the payment processing subsystem.
type PaymentConfig struct {
	Enabled                   bool                    `mapstructure:"enabled"`
	Processors                []PaymentProcessorEntry `mapstructure:"processors"`
	OfflineSettlementInterval string                  `mapstructure:"offline_settlement_interval"`
	MaxOfflineQueue           int                     `mapstructure:"max_offline_queue"`

	// StripeWebhookSecret is the signing secret from the Stripe Dashboard
	// (Settings → Webhooks). Used to verify the Stripe-Signature header on
	// incoming webhook events.  Set via env SOHOLINK_PAYMENT_STRIPE_WEBHOOK_SECRET.
	StripeWebhookSecret string `mapstructure:"stripe_webhook_secret"`
}

// PaymentProcessorEntry describes a configured payment processor.
type PaymentProcessorEntry struct {
	Type           string `mapstructure:"type"`
	Priority       int    `mapstructure:"priority"`
	FederationOnly bool   `mapstructure:"federation_only"`
	SecretKeyEnv   string `mapstructure:"secret_key_env"`
	LNDHost        string `mapstructure:"lnd_host"`
	LNDMacaroonEnv string `mapstructure:"lnd_macaroon_env"` // env var holding the hex macaroon
	LNDTLSCertPath string `mapstructure:"lnd_tls_cert_path"` // path to LND tls.cert for certificate pinning
	Contract       string `mapstructure:"contract"`
}

// LBTASConfig holds settings for the LBTAS reputation system.
type LBTASConfig struct {
	Enabled              bool   `mapstructure:"enabled"`
	RatingDeadline       string `mapstructure:"rating_deadline"`
	TimeoutCheckInterval string `mapstructure:"timeout_check_interval"`
}

// CentralConfig holds settings for the central SOHO operator subsystem.
type CentralConfig struct {
	Enabled                bool    `mapstructure:"enabled"`
	CenterDID              string  `mapstructure:"center_did"`
	TransactionFeePercent  float64 `mapstructure:"transaction_fee_percent"`  // e.g. 0.01 = 1%
	CapacityCheckInterval  string  `mapstructure:"capacity_check_interval"`
	CPUAlertThreshold      float64 `mapstructure:"cpu_alert_threshold"`      // 0.0-1.0
	StorageAlertThreshold  float64 `mapstructure:"storage_alert_threshold"`  // 0.0-1.0
}

// P2PConfig holds settings for the thin-client P2P mesh fallback.
type P2PConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	ListenAddr string `mapstructure:"listen_addr"`

	// AllowedNodeDIDs is an optional allowlist of peer DIDs.
	// When non-empty, the mesh will silently drop announcements from any DID
	// not in this list, preventing unknown nodes from appearing in the
	// federation registry.  Leave empty to allow all verified peers (default).
	AllowedNodeDIDs []string `mapstructure:"allowed_node_dids"`
}

// RentalConfig holds settings for the rental management subsystem.
type RentalConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// OrchestrationConfig holds settings for the elastic orchestration system.
type OrchestrationConfig struct {
	Enabled       bool `mapstructure:"enabled"`
	QueueSize     int  `mapstructure:"queue_size"`
	ScaleInterval string `mapstructure:"scale_interval"`
}

// ManagedServicesConfig holds settings for the managed services layer.
type ManagedServicesConfig struct {
	Enabled  bool `mapstructure:"enabled"`
	Postgres bool `mapstructure:"postgres"`
	ObjectStore bool `mapstructure:"object_store"`
	MessageQueue bool `mapstructure:"message_queue"`
}

// CDNConfig holds settings for the CDN edge caching layer.
type CDNConfig struct {
	Enabled       bool  `mapstructure:"enabled"`
	CacheCapacityMB int64 `mapstructure:"cache_capacity_mb"`
	DefaultTTL    string `mapstructure:"default_ttl"`
}

// SLAConfig holds settings for the SLA management subsystem.
type SLAConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	DefaultTier string `mapstructure:"default_tier"`
	CheckInterval string `mapstructure:"check_interval"`
}

// FederationConfig holds settings for the federation layer.
// Provider nodes announce themselves to a coordinator; coordinator nodes
// serve the registry and earn the platform fee on facilitated transactions.
type FederationConfig struct {
	// ── Provider settings ─────────────────────────────────────────────────
	// CoordinatorURL is the base URL of the coordinator this node registers
	// with on startup. Empty means standalone (no federation).
	CoordinatorURL string `mapstructure:"coordinator_url"`

	// Region is a geographic hint used for workload placement decisions.
	// Follows AWS-style naming, e.g. "us-east-1", "eu-west-1".
	Region string `mapstructure:"region"`

	// PricePerCPUHourSats is the advertised price for 1 CPU-core-hour in sats.
	PricePerCPUHourSats int64 `mapstructure:"price_per_cpu_hour_sats"`

	// HeartbeatInterval controls how often the node re-announces resource
	// availability to its coordinator. Default: 30s.
	HeartbeatInterval string `mapstructure:"heartbeat_interval"`

	// ── Coordinator settings ───────────────────────────────────────────────
	// IsCoordinator enables the coordinator API endpoints on this node.
	// Any node can act as a coordinator; smaller networks may use the same
	// machine for both provider and coordinator roles.
	IsCoordinator bool `mapstructure:"is_coordinator"`

	// FeePercent is the percentage of each transaction the coordinator earns.
	// Default 1.0 (1%). Set lower to attract more providers.
	FeePercent float64 `mapstructure:"fee_percent"`
}

// HypervisorConfig holds settings for the bare-metal hypervisor isolation.
type HypervisorConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	PreferBackend string `mapstructure:"prefer_backend"` // "kvm", "hyperv", "auto"
	SEVDefault    bool   `mapstructure:"sev_default"`
	TPMDefault    bool   `mapstructure:"tpm_default"`
}

// UpdatesConfig holds settings for the secure auto-update subsystem.
// When enabled, the node polls the GitHub releases API at CheckInterval,
// downloads and SHA-256 verifies the new binary, then performs an atomic swap.
type UpdatesConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	CheckInterval string `mapstructure:"check_interval"` // e.g. "24h"
	ReleaseURL    string `mapstructure:"release_url"`
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
	}
	// Policy.Directory is empty when using embedded FS (the default).
	// os.MkdirAll("") fails on Windows — skip it.
	if cfg.Policy.Directory != "" {
		dirs = append(dirs, cfg.Policy.Directory)
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

// ComputeWorkDir returns the compute job work directory.
func (c *Config) ComputeWorkDir() string {
	if c.ResourceSharing.Compute.WorkDir != "" {
		return c.ResourceSharing.Compute.WorkDir
	}
	return filepath.Join(c.Storage.BasePath, "compute")
}

// StoragePoolDir returns the shared storage pool directory.
func (c *Config) StoragePoolDir() string {
	if c.ResourceSharing.StoragePool.BaseDir != "" {
		return c.ResourceSharing.StoragePool.BaseDir
	}
	return filepath.Join(c.Storage.BasePath, "storage_pool")
}

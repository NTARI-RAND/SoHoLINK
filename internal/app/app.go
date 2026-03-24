package app

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crypto/ed25519"

	soholink "github.com/NetworkTheoryAppliedResearchInstitute/soholink"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/logging"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/accounting"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/blockchain"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/cdn"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/central"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/compute"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/federation"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/httpapi"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/lbtas"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/merkle"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestration"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/payment"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/portal"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/printer"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/radius"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/rental"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/services"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/sla"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/storage"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/p2p"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/thinclient"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/updater"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/verifier"
)

// App is the main application lifecycle manager.
// It wires together all subsystems and manages startup/shutdown.
type App struct {
	Config     *config.Config
	Store      *store.Store
	Verifier   *verifier.Verifier
	PolicyEng  *policy.Engine
	Accounting *accounting.Collector
	Batcher    *merkle.Batcher
	Radius     *radius.Server

	// Resource sharing subsystems (nil when disabled)
	LBTASManager    *lbtas.Manager
	PaymentLedger   *payment.Ledger
	ComputeSched    *compute.Scheduler
	StoragePool     *storage.Pool
	PrintSpooler    *printer.Spooler
	PortalServer    *portal.Server
	HTTPAPIServer   *httpapi.Server
	TimeoutResolver *lbtas.TimeoutResolver

	// Central SOHO subsystems (nil when disabled)
	Notifier           *central.Notifier
	CapacityMonitor    *central.CapacityMonitor
	TenantManager      *central.TenantManager
	RatingMonitor      *central.RatingMonitor
	DisputeManager     *central.DisputeManager
	CenterRatingMgr    *central.CenterRatingManager

	// P2P mesh fallback (nil when disabled)
	P2PNetwork *thinclient.P2PNetwork

	// P2PMesh is the small-world LAN peer discovery mesh (nil when disabled).
	// It auto-discovers federation peers via signed multicast UDP.
	P2PMesh *p2p.Mesh

	// FederationAnnouncer registers this node with a coordinator and sends
	// periodic heartbeats (nil when federation.coordinator_url is unset).
	FederationAnnouncer *federation.Announcer

	// Rental management (nil when disabled)
	RentalEngine *rental.AutoAcceptEngine

	// Blockchain anchoring (nil when disabled)
	LocalChain *blockchain.LocalChain

	// Enterprise architecture subsystems (nil when disabled)
	FedScheduler    *orchestration.FedScheduler
	ServiceCatalog  *services.Catalog
	CDNRouter       *cdn.Router
	SLAMonitor      *sla.Monitor
	SLARecommender  *sla.Recommender
	HypervisorMgr   *compute.HypervisorManager

	// Auto-updater (nil when updates.enabled=false)
	Updater *updater.Updater

	// Build-time version metadata (set via SetVersion after New).
	Version   string
	Commit    string
	BuildTime string

	cancelFunc context.CancelFunc
}

// New creates a new App instance, initializing all subsystems.
func New(cfg *config.Config) (*App, error) {
	// Initialize structured logging before anything else.
	// All existing log.Printf calls are automatically bridged through slog.
	logFormat := "text"
	logLevel := "info"
	if os.Getenv("SOHOLINK_LOG_FORMAT") != "" {
		logFormat = os.Getenv("SOHOLINK_LOG_FORMAT")
	}
	if os.Getenv("SOHOLINK_LOG_LEVEL") != "" {
		logLevel = os.Getenv("SOHOLINK_LOG_LEVEL")
	}
	logging.Init(logFormat, logLevel, os.Stderr)

	app := &App{Config: cfg}

	// Initialize store
	s, err := store.NewStore(cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}
	app.Store = s

	// Initialize verifier
	credTTL := time.Duration(cfg.Auth.CredentialTTL) * time.Second
	maxNonceAge := time.Duration(cfg.Auth.MaxNonceAge) * time.Second
	if maxNonceAge == 0 {
		maxNonceAge = credTTL
	}
	if credTTL > 0 && maxNonceAge < credTTL {
		log.Printf("[app] auth.max_nonce_age (%s) is shorter than credential_ttl (%s); extending nonce cache duration to match TTL",
			maxNonceAge, credTTL)
		maxNonceAge = credTTL
	}
	if maxNonceAge > 0 {
		cfg.Auth.MaxNonceAge = int(maxNonceAge.Seconds())
	}
	app.Verifier = verifier.NewVerifier(s, credTTL, maxNonceAge)

	// Initialize policy engine.
	// The embedded .rego files (configs/policies/*.rego) are always available as
	// a fallback so the binary works without an external policy directory.
	// fs.Sub strips the "configs/policies" prefix so *.rego files sit at the
	// root of the sub-FS, matching what fs.Glob(fallback, "*.rego") expects.
	var policyFallback fs.FS
	if sub, subErr := fs.Sub(soholink.PoliciesFS, "configs/policies"); subErr == nil {
		policyFallback = sub
	}
	pe, err := policy.NewEngine(cfg.Policy.Directory, policyFallback)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to initialize policy engine: %w", err)
	}
	app.PolicyEng = pe

	// Initialize accounting collector
	ac, err := accounting.NewCollector(cfg.AccountingDir())
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to initialize accounting collector: %w", err)
	}
	app.Accounting = ac

	// Initialize Merkle batcher
	batchInterval, err := time.ParseDuration(cfg.Merkle.BatchInterval)
	if err != nil {
		batchInterval = 1 * time.Hour
	}
	app.Batcher = merkle.NewBatcher(cfg.AccountingDir(), cfg.MerkleDir(), batchInterval)

	// Initialize RADIUS server
	app.Radius = radius.NewServer(
		cfg.Radius.AuthAddress,
		cfg.Radius.AcctAddress,
		cfg.Radius.SharedSecret,
		app.Verifier,
		app.PolicyEng,
		app.Accounting,
	)

	// Initialize resource sharing subsystems (if enabled)
	if cfg.ResourceSharing.Enabled {
		if err := app.initResourceSharing(cfg); err != nil {
			log.Printf("[app] resource sharing init error (non-fatal): %v", err)
		}
	}

	// Initialize central SOHO subsystems (if enabled)
	if cfg.Central.Enabled {
		app.initCentral(cfg)
	}

	// Initialize P2P mesh (if enabled)
	if cfg.P2P.Enabled {
		app.initP2P(cfg)
	}

	// Initialize rental management (if enabled)
	if cfg.Rental.Enabled {
		app.initRental()
	}

	// Initialize enterprise architecture subsystems
	if cfg.Orchestration.Enabled {
		app.initOrchestration()
	}
	if cfg.Services.Enabled {
		app.initManagedServices(cfg)
	}
	if cfg.CDN.Enabled {
		app.initCDN()
	}
	if cfg.SLA.Enabled {
		app.initSLA()
	}
	if cfg.Hypervisor.Enabled {
		app.initHypervisor()
	}
	if cfg.Blockchain.Enabled {
		app.initBlockchain(cfg)
	}

	// Initialize federation layer (provider announcer + coordinator API).
	// Runs even when resource sharing is partially configured; the HTTP API
	// server must already be set up (initResourceSharing must have run first).
	if cfg.Federation.CoordinatorURL != "" || cfg.Federation.IsCoordinator {
		app.initFederation(cfg)
	}

	// Auto-updater: created here so GUI can inspect LatestRelease() immediately.
	// The actual polling goroutine is started in Start().
	// SetVersion() is called from main.go after New() to inject ldflags version.
	if cfg.Updates.Enabled {
		app.Updater = updater.New(updater.Config{
			CheckInterval: cfg.Updates.CheckInterval,
			ReleaseURL:    cfg.Updates.ReleaseURL,
		}, "0.1.0-dev") // version placeholder; overwritten by SetVersion()
	}

	return app, nil
}

// SetVersion stores the build-time version, commit hash, and build timestamp
// on the App and propagates them to the HTTP API server and updater.
// Call this immediately after New(), before RunDashboard or Start().
func (a *App) SetVersion(version, commit, buildTime string) {
	a.Version = version
	a.Commit = commit
	a.BuildTime = buildTime
	if a.Updater != nil {
		a.Updater.SetCurrentVersion(version)
	}
	if a.HTTPAPIServer != nil {
		a.HTTPAPIServer.SetVersionInfo(version, commit, buildTime)
	}
}

// initFederation sets up the provider-side federation announcer and, when this
// node is configured as a coordinator, registers the coordinator metadata with
// the HTTP API server so the federation endpoints return live data.
func (a *App) initFederation(cfg *config.Config) {
	log.Printf("[app] initializing federation layer")
	ctx := context.Background()

	// Ensure the node has a persistent signing keypair (machine identity).
	if _, err := a.Store.EnsureNodeSigningKeypair(ctx); err != nil {
		log.Printf("[app] federation: could not ensure signing keypair: %v", err)
		return
	}
	privSeedHex, pubKeyB64, err := a.Store.GetNodeSigningKey(ctx)
	if err != nil || privSeedHex == "" {
		log.Printf("[app] federation: could not read signing keypair: %v", err)
		return
	}

	// Determine the heartbeat interval (default 30 s).
	heartbeat := 30 * time.Second
	if cfg.Federation.HeartbeatInterval != "" {
		if d, parseErr := time.ParseDuration(cfg.Federation.HeartbeatInterval); parseErr == nil {
			heartbeat = d
		}
	}

	// Resolve the advertised API address.
	apiAddr := cfg.ResourceSharing.HTTPAPIAddress
	if apiAddr == "" {
		apiAddr = "0.0.0.0:8080"
	}

	// Provider mode: announce this node to its configured coordinator.
	if cfg.Federation.CoordinatorURL != "" {
		fedCfg := federation.Config{
			CoordinatorURL:      cfg.Federation.CoordinatorURL,
			NodeDID:             cfg.Node.DID,
			Address:             apiAddr,
			Region:              cfg.Federation.Region,
			PricePerCPUHourSats: cfg.Federation.PricePerCPUHourSats,
			PrivSeedHex:         privSeedHex,
			PubKeyB64:           pubKeyB64,
			HeartbeatInterval:   heartbeat,
		}
		a.FederationAnnouncer = federation.New(fedCfg)
		log.Printf("[app] federation announcer ready (coordinator=%s region=%s)",
			cfg.Federation.CoordinatorURL, cfg.Federation.Region)
	}

	// Coordinator mode: expose the coordinator metadata on the HTTP API.
	if cfg.Federation.IsCoordinator && a.HTTPAPIServer != nil {
		region := cfg.Federation.Region
		if region == "" {
			region = "global"
		}
		feePct := cfg.Federation.FeePercent
		if feePct == 0 {
			feePct = 1.0
		}
		a.HTTPAPIServer.SetCoordinatorMeta(cfg.Node.DID, feePct, []string{region})
		log.Printf("[app] federation coordinator enabled (DID=%s fee=%.1f%%)",
			cfg.Node.DID, feePct)
	}
}

// initBlockchain initializes the local blockchain for Merkle root anchoring.
func (a *App) initBlockchain(cfg *config.Config) {
	log.Printf("[app] initializing local blockchain")

	// Load node private key for block signing
	var privKey ed25519.PrivateKey
	keyPath := cfg.NodeKeyPath()
	keyData, err := os.ReadFile(keyPath)
	if err == nil && len(keyData) == ed25519.PrivateKeySize {
		privKey = ed25519.PrivateKey(keyData)
	}

	a.LocalChain = blockchain.NewLocalChain(a.Store, cfg.Node.DID, privKey)
	log.Printf("[app] local blockchain initialized (DID=%s)", cfg.Node.DID)
}

// initResourceSharing initializes all resource sharing subsystems.
func (a *App) initResourceSharing(cfg *config.Config) error {
	log.Printf("[app] initializing resource sharing subsystems")

	// Initialize LBTAS manager
	a.LBTASManager = lbtas.NewManager(a.Store, a.Accounting)

	// Initialize payment processors
	var processors []payment.PaymentProcessor
	for _, pc := range cfg.Payment.Processors {
		switch pc.Type {
		case "barter":
			processors = append(processors, payment.NewBarterProcessor(a.Store, pc.FederationOnly))
		case "stripe":
			secretKey := os.Getenv(pc.SecretKeyEnv)
			processors = append(processors, payment.NewStripeProcessor(secretKey, ""))
		case "lightning":
			macaroon := ""
			if pc.LNDMacaroonEnv != "" {
				macaroon = os.Getenv(pc.LNDMacaroonEnv)
			}
			processors = append(processors, payment.NewLightningProcessor(pc.LNDHost, macaroon, pc.LNDTLSCertPath))
		case "federation_token":
			processors = append(processors, payment.NewFederationTokenProcessor(a.Store, pc.Contract, ""))
		}
	}
	a.PaymentLedger = payment.NewLedger(a.Store, processors)

	// Initialize timeout resolver
	checkInterval := 1 * time.Hour
	if cfg.LBTAS.TimeoutCheckInterval != "" {
		if parsed, err := time.ParseDuration(cfg.LBTAS.TimeoutCheckInterval); err == nil {
			checkInterval = parsed
		}
	}
	a.TimeoutResolver = lbtas.NewTimeoutResolver(a.Store, a.Accounting, checkInterval)

	// Initialize compute scheduler
	if cfg.ResourceSharing.Compute.Enabled {
		workers := cfg.ResourceSharing.Compute.Workers
		if workers <= 0 {
			workers = 2
		}
		sandbox := compute.NewSandbox(cfg.ComputeWorkDir())
		a.ComputeSched = compute.NewScheduler(a.Store, a.Accounting, a.LBTASManager, sandbox, workers)
	}

	// Initialize storage pool
	if cfg.ResourceSharing.StoragePool.Enabled {
		var scanner *storage.ContentScanner
		if cfg.ResourceSharing.StoragePool.ContentScanning {
			scanner = storage.NewContentScanner(cfg.ResourceSharing.StoragePool.ClamAVSocket)
		}
		maxFileSize := cfg.ResourceSharing.StoragePool.MaxFileSize
		if maxFileSize <= 0 {
			maxFileSize = 10 * 1024 * 1024 * 1024 // 10 GB default
		}
		a.StoragePool = storage.NewPool(a.Store, a.Accounting, scanner, cfg.StoragePoolDir(), maxFileSize)
	}

	// Initialize printer spooler
	if cfg.ResourceSharing.Printer.Enabled {
		validator := printer.NewGCodeValidator()
		if cfg.ResourceSharing.Printer.MaxHotendTemp > 0 {
			validator.MaxTemp = cfg.ResourceSharing.Printer.MaxHotendTemp
		}
		if cfg.ResourceSharing.Printer.MaxBedTemp > 0 {
			validator.MaxBedTemp = cfg.ResourceSharing.Printer.MaxBedTemp
		}
		if cfg.ResourceSharing.Printer.MaxFeedRate > 0 {
			validator.MaxFeedRate = cfg.ResourceSharing.Printer.MaxFeedRate
		}
		a.PrintSpooler = printer.NewSpooler(a.Store, a.Accounting, validator)
	}

	// Initialize captive portal
	if cfg.ResourceSharing.Portal.Enabled {
		portalAddr := cfg.ResourceSharing.PortalAddress
		if portalAddr == "" {
			portalAddr = "0.0.0.0:8081"
		}
		a.PortalServer = portal.NewServer(a.Store, a.Accounting, a.Verifier, portalAddr)
	}

	// Initialize HTTP API server
	apiAddr := cfg.ResourceSharing.HTTPAPIAddress
	if apiAddr == "" {
		apiAddr = "0.0.0.0:8080"
	}
	a.HTTPAPIServer = httpapi.NewServer(a.Store, a.LBTASManager, apiAddr)

	// Expose build-time version via GET /api/version.
	// Version is populated later by SetVersion(); an empty string is safe
	// (the endpoint always responds with whatever is set at query time).
	a.HTTPAPIServer.SetVersionInfo(a.Version, a.Commit, a.BuildTime)

	// Wire TLS, CORS, and webhook security settings.
	a.HTTPAPIServer.SetTLSConfig(cfg.ResourceSharing.TLSCertFile, cfg.ResourceSharing.TLSKeyFile)
	if len(cfg.ResourceSharing.AllowedOrigins) > 0 {
		a.HTTPAPIServer.SetAllowedOrigins(cfg.ResourceSharing.AllowedOrigins)
	}
	if cfg.Payment.StripeWebhookSecret != "" {
		a.HTTPAPIServer.SetStripeWebhookSecret(cfg.Payment.StripeWebhookSecret)
	}

	// Stripe processor: pass the webhook secret so NewStripeProcessor can verify events.
	for i, p := range cfg.Payment.Processors {
		if p.Type == "stripe" && cfg.Payment.StripeWebhookSecret != "" {
			_ = i // processors slice is already built; webhook verification is handled in the HTTP handler
			break
		}
	}

	// Gap 2: attach the payment ledger so payout endpoints use real processors.
	if a.PaymentLedger != nil {
		a.HTTPAPIServer.SetPaymentLedger(a.PaymentLedger)
	}

	log.Printf("[app] resource sharing subsystems initialized")
	return nil
}

// initCentral initializes central SOHO operator subsystems:
// capacity monitoring, tenant management, rating alerts, disputes, center rating.
func (a *App) initCentral(cfg *config.Config) {
	log.Printf("[app] initializing central SOHO subsystems")

	a.Notifier = central.NewNotifier()
	a.TenantManager = central.NewTenantManager(a.Store)

	// Capacity monitor
	interval := 5 * time.Minute
	if cfg.Central.CapacityCheckInterval != "" {
		if parsed, err := time.ParseDuration(cfg.Central.CapacityCheckInterval); err == nil {
			interval = parsed
		}
	}
	a.CapacityMonitor = central.NewCapacityMonitor(a.Store, interval)

	// Dispute manager (needs payment ledger)
	a.DisputeManager = central.NewDisputeManager(a.Store, a.Notifier, a.PaymentLedger)

	// Rating monitor (needs dispute manager)
	a.RatingMonitor = central.NewRatingMonitor(a.Store, a.Notifier, a.DisputeManager, cfg.Central.CenterDID)

	// Center rating manager
	a.CenterRatingMgr = central.NewCenterRatingManager(a.Store, a.Notifier)

	log.Printf("[app] central SOHO subsystems initialized (center DID=%s)", cfg.Central.CenterDID)
}

// initP2P initializes the thin-client P2P mesh networking fallback and the
// small-world LAN discovery mesh.
func (a *App) initP2P(cfg *config.Config) {
	log.Printf("[app] initializing P2P mesh network")

	listenAddr := cfg.P2P.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0:9090"
	}

	a.P2PNetwork = thinclient.NewP2PNetwork(a.Store, cfg.Node.DID, listenAddr)
	log.Printf("[app] P2P mesh initialized (listen=%s)", listenAddr)

	// Small-world LAN discovery mesh: load node private key (same key used by
	// the blockchain subsystem) and start announcing this node's capabilities.
	var privKey ed25519.PrivateKey
	keyPath := cfg.NodeKeyPath()
	if keyData, err := os.ReadFile(keyPath); err == nil && len(keyData) == ed25519.PrivateKeySize {
		privKey = ed25519.PrivateKey(keyData)
	}
	if privKey == nil {
		log.Printf("[app] p2p mesh: no node key at %s; LAN discovery disabled", keyPath)
		return
	}

	apiAddr := cfg.ResourceSharing.HTTPAPIAddress
	if apiAddr == "" {
		apiAddr = "0.0.0.0:8080"
	}

	meshCfg := p2p.Config{
		DID:             cfg.Node.DID,
		PrivateKey:      privKey,
		APIAddr:         apiAddr,
		IPFSAddr:        cfg.Storage.IPFSAPIAddr,
		Region:          cfg.Node.Location,
		Store:           a.Store,
		AllowedNodeDIDs: cfg.P2P.AllowedNodeDIDs, // optional allowlist; empty = accept all verified peers
	}
	a.P2PMesh = p2p.New(meshCfg)
	log.Printf("[app] small-world mesh initialized (multicast 239.255.42.99:7946)")
}

// initRental initializes the rental management and auto-accept engine.
func (a *App) initRental() {
	log.Printf("[app] initializing rental management")
	a.RentalEngine = rental.NewAutoAcceptEngine(a.Store)
	log.Printf("[app] rental engine initialized")
}

// initOrchestration initializes the elastic orchestration system (FedScheduler).
func (a *App) initOrchestration() {
	log.Printf("[app] initializing elastic orchestration")
	a.FedScheduler = orchestration.NewFedScheduler(a.Store)
	log.Printf("[app] FedScheduler initialized")
}

// initManagedServices initializes the managed services catalog and provisioners.
func (a *App) initManagedServices(cfg *config.Config) {
	log.Printf("[app] initializing managed services catalog")
	a.ServiceCatalog = services.NewCatalog(a.Store)

	if cfg.Services.Postgres {
		a.ServiceCatalog.RegisterProvisioner(services.ServiceTypePostgres, services.NewPostgresProvisioner("unix:///var/run/docker.sock"))
	}
	if cfg.Services.ObjectStore {
		a.ServiceCatalog.RegisterProvisioner(services.ServiceTypeObjectStore, services.NewObjectStoreProvisioner("unix:///var/run/docker.sock"))
	}
	if cfg.Services.MessageQueue {
		a.ServiceCatalog.RegisterProvisioner(services.ServiceTypeMessageQueue, services.NewQueueProvisioner("unix:///var/run/docker.sock"))
	}

	// Expose the catalog on the HTTP API for marketplace service endpoints.
	if a.HTTPAPIServer != nil {
		a.HTTPAPIServer.SetServiceCatalog(a.ServiceCatalog)
	}
	log.Printf("[app] managed services catalog initialized")
}

// initCDN initializes the CDN routing and edge cache layer.
func (a *App) initCDN() {
	log.Printf("[app] initializing CDN router")
	a.CDNRouter = cdn.NewRouter()
	log.Printf("[app] CDN router initialized")
}

// initSLA initializes the SLA monitoring and recommendation engine.
func (a *App) initSLA() {
	log.Printf("[app] initializing SLA subsystem")
	a.SLAMonitor = sla.NewMonitor(a.Store)
	a.SLARecommender = sla.NewRecommender()
	log.Printf("[app] SLA monitor and recommender initialized")
}

// initHypervisor initializes the bare-metal hypervisor manager.
func (a *App) initHypervisor() {
	log.Printf("[app] initializing hypervisor manager")
	a.HypervisorMgr = compute.NewHypervisorManager()
	if a.HypervisorMgr.Available() {
		log.Printf("[app] hypervisor available: %v", a.HypervisorMgr.ListBackends())
	} else {
		log.Printf("[app] no hypervisor backends available on this platform")
	}
}

// Start begins all services and blocks until a shutdown signal is received.
func (a *App) Start() error {
	// Validate config first — returns error on fatal misconfigurations.
	if err := a.validateConfig(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancelFunc = cancel

	// Start RADIUS server (only when enabled in config).
	if a.Config.Radius.Enabled {
		if err := a.Radius.Start(); err != nil {
			cancel()
			return fmt.Errorf("failed to start RADIUS server: %w", err)
		}
	}

	// Start Merkle batcher in background with blockchain anchoring callback
	if a.LocalChain != nil {
		a.Batcher.SetAnchorFunc(func(rootHash []byte, sourceFile string, leafCount, treeHeight int) {
			_, _, err := a.LocalChain.SubmitBatch(ctx, rootHash, blockchain.BatchMetadata{
				SourceFile: sourceFile,
				LeafCount:  leafCount,
				TreeHeight: treeHeight,
				NodeDID:    a.Config.Node.DID,
			})
			if err != nil {
				log.Printf("[app] blockchain anchor failed: %v", err)
			}
		})
	}
	go a.Batcher.Start(ctx)

	// Start nonce pruner in background
	go a.startNoncePruner(ctx)

	// Start log compressor in background
	go a.startLogCompressor(ctx)

	// Start resource sharing subsystems (if enabled)
	if a.Config.ResourceSharing.Enabled {
		a.startResourceSharing(ctx)
	}

	// Start central SOHO subsystems (if enabled)
	if a.Config.Central.Enabled {
		a.startCentral(ctx)
	}

	// Start P2P mesh (if enabled)
	if a.P2PNetwork != nil {
		a.P2PNetwork.Start(ctx)
		log.Printf("[app]   P2P mesh:   %s", a.Config.P2P.ListenAddr)
	}

	// Start small-world LAN discovery mesh (if initialised).
	if a.P2PMesh != nil {
		if a.HTTPAPIServer != nil {
			a.HTTPAPIServer.SetP2PMesh(a.P2PMesh)
		}
		go func() {
			if err := a.P2PMesh.Start(ctx); err != nil {
				log.Printf("[app] p2p mesh stopped: %v", err)
			}
		}()
		log.Printf("[app]   Small-world mesh: multicast 239.255.42.99:7946")
	}

	// Load rental rules (if enabled)
	if a.RentalEngine != nil {
		if err := a.RentalEngine.LoadRules(ctx); err != nil {
			log.Printf("[app] rental rules load error: %v", err)
		}
		log.Printf("[app]   Rental:     enabled")
	}

	// Start enterprise architecture subsystems
	if a.FedScheduler != nil {
		a.FedScheduler.Start(ctx)
		log.Printf("[app]   Orchestrator: FedScheduler started")
	}
	if a.ServiceCatalog != nil {
		go a.ServiceCatalog.HealthCheckLoop(ctx)
		log.Printf("[app]   Services:   catalog started")
	}
	if a.CDNRouter != nil {
		done := make(chan struct{})
		go func() {
			<-ctx.Done()
			close(done)
		}()
		go a.CDNRouter.HealthCheckLoop(done)
		log.Printf("[app]   CDN:        router started")
	}
	if a.SLAMonitor != nil {
		go a.SLAMonitor.MonitorLoop(ctx)
		log.Printf("[app]   SLA:        monitor started")
	}

	// Start federation announcer (provider mode — sends heartbeats until shutdown)
	if a.FederationAnnouncer != nil {
		go a.FederationAnnouncer.Start(ctx)
		log.Printf("[app]   Federation: announcer started (coordinator=%s)",
			a.Config.Federation.CoordinatorURL)
	}

	// Start auto-updater (polls GitHub releases at the configured interval)
	if a.Updater != nil {
		go a.Updater.Start(ctx)
		log.Printf("[app]   Updater:    enabled (interval=%s)", a.Config.Updates.CheckInterval)
	}

	log.Printf("[app] SoHoLINK AAA node started")
	log.Printf("[app]   Auth:       %s", a.Radius.AuthAddr())
	log.Printf("[app]   Accounting: %s", a.Radius.AcctAddr())
	log.Printf("[app]   Data dir:   %s", a.Config.Storage.BasePath)
	log.Printf("[app]   Policies:   %s", a.PolicyEng.PolicyDir())
	if a.Config.ResourceSharing.Enabled {
		log.Printf("[app]   HTTP API:   %s", a.Config.ResourceSharing.HTTPAPIAddress)
		if a.Config.ResourceSharing.Compute.Enabled {
			log.Printf("[app]   Compute:    %d workers", a.Config.ResourceSharing.Compute.Workers)
		}
		if a.Config.ResourceSharing.StoragePool.Enabled {
			log.Printf("[app]   Storage:    %s", a.Config.StoragePoolDir())
		}
		if a.Config.ResourceSharing.Printer.Enabled {
			log.Printf("[app]   Printer:    enabled")
		}
		if a.Config.ResourceSharing.Portal.Enabled {
			log.Printf("[app]   Portal:     %s", a.Config.ResourceSharing.PortalAddress)
		}
	}
	if a.Config.Central.Enabled {
		log.Printf("[app]   Central:    enabled (DID=%s)", a.Config.Central.CenterDID)
	}
	if a.Config.Orchestration.Enabled {
		log.Printf("[app]   Orchestration: enabled")
	}
	if a.Config.Services.Enabled {
		log.Printf("[app]   Services:   managed (pg=%v, s3=%v, mq=%v)",
			a.Config.Services.Postgres, a.Config.Services.ObjectStore, a.Config.Services.MessageQueue)
	}
	if a.Config.CDN.Enabled {
		log.Printf("[app]   CDN:        enabled (cache=%dMB)", a.Config.CDN.CacheCapacityMB)
	}
	if a.Config.SLA.Enabled {
		log.Printf("[app]   SLA:        enabled (tier=%s)", a.Config.SLA.DefaultTier)
	}
	if a.Config.Hypervisor.Enabled {
		log.Printf("[app]   Hypervisor: enabled (backend=%s)", a.Config.Hypervisor.PreferBackend)
	}
	if a.Config.Federation.IsCoordinator {
		log.Printf("[app]   Federation: coordinator (fee=%.1f%%)", a.Config.Federation.FeePercent)
	}
	if a.Config.Federation.CoordinatorURL != "" {
		log.Printf("[app]   Federation: provider (url=%s)", a.Config.Federation.CoordinatorURL)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("[app] received signal: %v, shutting down...", sig)

	return a.Shutdown()
}

// Shutdown performs an orderly shutdown of all subsystems.
func (a *App) Shutdown() error {
	if a.cancelFunc != nil {
		a.cancelFunc()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Stop resource sharing subsystems
	if a.HTTPAPIServer != nil {
		if err := a.HTTPAPIServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("[app] HTTP API shutdown error: %v", err)
		}
	}
	if a.ComputeSched != nil {
		a.ComputeSched.Stop()
	}

	// 1b. Stop enterprise subsystems
	if a.FedScheduler != nil {
		a.FedScheduler.Stop()
	}

	// 2. Stop accepting new RADIUS packets (only if it was started).
	if a.Config.Radius.Enabled {
		if err := a.Radius.Shutdown(shutdownCtx); err != nil {
			log.Printf("[app] RADIUS shutdown error: %v", err)
		}
	}

	// 3. Flush accounting logs
	if err := a.Accounting.Close(); err != nil {
		log.Printf("[app] accounting close error: %v", err)
	}

	// 4. Build final Merkle batch
	if err := a.Batcher.BuildBatch(); err != nil {
		log.Printf("[app] final Merkle batch error: %v", err)
	}

	// 5. Close database
	if err := a.Store.Close(); err != nil {
		log.Printf("[app] store close error: %v", err)
	}

	log.Printf("[app] shutdown complete")
	return nil
}

// startResourceSharing launches all enabled resource sharing background services.
func (a *App) startResourceSharing(ctx context.Context) {
	// Start LBTAS timeout resolver
	if a.TimeoutResolver != nil {
		go a.TimeoutResolver.Run(ctx)
		log.Printf("[app] LBTAS timeout resolver started")
	}

	// Start compute scheduler
	if a.ComputeSched != nil {
		a.ComputeSched.Start(ctx)
	}

	// Gap 4: wire usage metering — bills running placements every hour.
	// FedScheduler is the PlacementSource; PaymentLedger routes the charges.
	if a.FedScheduler != nil && a.PaymentLedger != nil {
		meter := payment.NewUsageMeter(a.FedScheduler, a.PaymentLedger, payment.MeterConfig{
			BillingInterval:    time.Hour,
			MinBillableSeconds: 60,
		})
		go meter.Run(ctx)
		log.Printf("[app]   Meter: usage billing started (interval=1h)")
	}

	// Start printer spooler
	if a.PrintSpooler != nil {
		go a.PrintSpooler.Run(ctx)
	}

	// Start captive portal
	if a.PortalServer != nil {
		go func() {
			if err := a.PortalServer.Start(ctx); err != nil {
				log.Printf("[app] portal server error: %v", err)
			}
		}()
	}

	// Start HTTP API server
	if a.HTTPAPIServer != nil {
		go func() {
			if err := a.HTTPAPIServer.Start(ctx); err != nil {
				log.Printf("[app] HTTP API server error: %v", err)
			}
		}()
	}
}

// startCentral launches all central SOHO background services.
func (a *App) startCentral(ctx context.Context) {
	// Start capacity monitor
	if a.CapacityMonitor != nil {
		go a.CapacityMonitor.Run(ctx)
		log.Printf("[app] capacity monitor started")
	}

	// Start capacity alert consumer
	if a.CapacityMonitor != nil && a.Notifier != nil {
		go func() {
			for {
				select {
				case alert := <-a.CapacityMonitor.AlertChan():
					a.Notifier.Send(central.Notification{
						Type:     "capacity_alert",
						Severity: alert.Severity,
						Title:    alert.Resource,
						Message:  alert.Message,
					})
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

// startNoncePruner runs periodically to clean up expired nonces.
func (a *App) startNoncePruner(ctx context.Context) {
	maxAge := time.Duration(a.Config.Auth.MaxNonceAge) * time.Second
	if maxAge == 0 {
		maxAge = 5 * time.Minute
	}

	ticker := time.NewTicker(maxAge)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruned, err := a.Store.PruneExpiredNonces(ctx, maxAge)
			if err != nil {
				log.Printf("[app] nonce pruner error: %v", err)
			} else if pruned > 0 {
				log.Printf("[app] pruned %d expired nonces", pruned)
			}
		}
	}
}

// startLogCompressor runs periodically to compress old accounting logs.
func (a *App) startLogCompressor(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maxAge := time.Duration(a.Config.Accounting.CompressAfterDays) * 24 * time.Hour
			if maxAge == 0 {
				maxAge = 7 * 24 * time.Hour
			}
			compressed, err := accounting.CompressOldLogs(a.Config.AccountingDir(), maxAge)
			if err != nil {
				log.Printf("[app] log compressor error: %v", err)
			} else if compressed > 0 {
				log.Printf("[app] compressed %d old log files", compressed)
			}
		}
	}
}

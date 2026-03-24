package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database for SoHoLINK credential and user management.
type Store struct {
	db *sql.DB
}

// User represents a registered user in the system.
type User struct {
	ID        int64
	Username  string
	DID       string
	PublicKey []byte
	Role      string
	CreatedAt string
	RevokedAt sql.NullString
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT UNIQUE NOT NULL,
	did TEXT UNIQUE NOT NULL,
	public_key BLOB NOT NULL,
	role TEXT NOT NULL DEFAULT 'basic',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	revoked_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_users_did ON users(did);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

CREATE TABLE IF NOT EXISTS revocations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	did TEXT NOT NULL,
	reason TEXT,
	revoked_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_revocations_did ON revocations(did);
CREATE INDEX IF NOT EXISTS idx_revocations_revoked_at ON revocations(revoked_at);

CREATE TABLE IF NOT EXISTS nonce_cache (
	nonce TEXT PRIMARY KEY,
	seen_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_nonce_cache_seen_at ON nonce_cache(seen_at);

CREATE TABLE IF NOT EXISTS node_info (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS resource_transactions (
	transaction_id TEXT PRIMARY KEY,
	user_did TEXT NOT NULL,
	provider_did TEXT NOT NULL,
	resource_type TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	state TEXT NOT NULL DEFAULT 'initiated',
	payment_amount INTEGER DEFAULT 0,
	payment_currency TEXT DEFAULT 'USD',
	payment_escrowed INTEGER DEFAULT 0,
	payment_proof BLOB,
	results_ready INTEGER DEFAULT 0,
	results_hash BLOB,
	results_path TEXT,
	results_key BLOB,
	provider_rating_id TEXT,
	user_rating_id TEXT,
	rating_deadline DATETIME,
	dispute_id TEXT,
	dispute_reason TEXT,
	blockchain_block INTEGER,
	blockchain_hash BLOB,
	user_signature BLOB,
	provider_signature BLOB,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_resource_transactions_user ON resource_transactions(user_did);
CREATE INDEX IF NOT EXISTS idx_resource_transactions_provider ON resource_transactions(provider_did);
CREATE INDEX IF NOT EXISTS idx_resource_transactions_state ON resource_transactions(state);
CREATE INDEX IF NOT EXISTS idx_resource_transactions_deadline ON resource_transactions(rating_deadline);

CREATE TABLE IF NOT EXISTS lbtas_scores (
	did TEXT PRIMARY KEY,
	overall_score INTEGER NOT NULL DEFAULT 50,
	payment_reliability REAL DEFAULT 0,
	execution_quality REAL DEFAULT 0,
	communication REAL DEFAULT 0,
	resource_usage REAL DEFAULT 0,
	total_transactions INTEGER DEFAULT 0,
	completed_transactions INTEGER DEFAULT 0,
	disputed_transactions INTEGER DEFAULT 0,
	score_history TEXT,
	last_anchor_block INTEGER,
	last_anchor_hash BLOB,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS lbtas_ratings (
	rating_id TEXT PRIMARY KEY,
	transaction_id TEXT NOT NULL,
	rater_did TEXT NOT NULL,
	ratee_did TEXT NOT NULL,
	rater_role TEXT NOT NULL,
	score INTEGER NOT NULL CHECK(score >= 0 AND score <= 5),
	category TEXT NOT NULL,
	feedback TEXT,
	evidence BLOB,
	signature BLOB,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (transaction_id) REFERENCES resource_transactions(transaction_id)
);

CREATE INDEX IF NOT EXISTS idx_lbtas_ratings_ratee ON lbtas_ratings(ratee_did, created_at);
CREATE INDEX IF NOT EXISTS idx_lbtas_ratings_transaction ON lbtas_ratings(transaction_id);

CREATE TABLE IF NOT EXISTS pending_payments (
	id TEXT PRIMARY KEY,
	charge_id TEXT NOT NULL,
	user_did TEXT NOT NULL,
	provider_did TEXT NOT NULL,
	amount INTEGER NOT NULL,
	currency TEXT NOT NULL DEFAULT 'USD',
	resource_type TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending',
	attempts INTEGER DEFAULT 0,
	next_retry DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pending_payments_status ON pending_payments(status);
CREATE INDEX IF NOT EXISTS idx_pending_payments_retry ON pending_payments(next_retry);

-- Tenants (thin clients registered with central SOHO)
CREATE TABLE IF NOT EXISTS tenants (
	tenant_id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	center_did TEXT,
	status TEXT NOT NULL DEFAULT 'active',
	cpu_cores INTEGER DEFAULT 0,
	storage_gb INTEGER DEFAULT 0,
	memory_gb INTEGER DEFAULT 0,
	gpu_model TEXT DEFAULT '',
	last_active DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_center ON tenants(center_did);

-- Central revenue tracking (1% fee model)
CREATE TABLE IF NOT EXISTS central_revenue (
	revenue_id TEXT PRIMARY KEY,
	transaction_id TEXT NOT NULL,
	tenant_id TEXT NOT NULL,
	total_amount INTEGER NOT NULL,
	central_fee INTEGER NOT NULL,
	producer_payout INTEGER NOT NULL,
	processor_fee INTEGER NOT NULL,
	currency TEXT DEFAULT 'USD',
	settled_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_central_revenue_tenant ON central_revenue(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_central_revenue_settled ON central_revenue(settled_at);

-- Auto-accept rules for rental management
CREATE TABLE IF NOT EXISTS auto_accept_rules (
	rule_id TEXT PRIMARY KEY,
	rule_name TEXT NOT NULL,
	enabled BOOLEAN DEFAULT 1,
	priority INTEGER DEFAULT 10,
	min_user_score INTEGER DEFAULT 0,
	max_amount INTEGER DEFAULT 0,
	resource_type TEXT DEFAULT '',
	allowed_hours TEXT DEFAULT '[]',
	allowed_days TEXT DEFAULT '[]',
	require_prepay BOOLEAN DEFAULT 0,
	action TEXT NOT NULL DEFAULT 'pending',
	notify_operator BOOLEAN DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_auto_accept_priority ON auto_accept_rules(priority, enabled);

-- P2P blockchain blocks (offline operation cache)
CREATE TABLE IF NOT EXISTS p2p_blocks (
	height INTEGER PRIMARY KEY,
	data BLOB NOT NULL,
	hash BLOB NOT NULL,
	prev_hash BLOB NOT NULL,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	synced_to_central BOOLEAN DEFAULT 0
);

-- P2P peers (thin client mesh)
CREATE TABLE IF NOT EXISTS p2p_peers (
	peer_did TEXT PRIMARY KEY,
	address TEXT NOT NULL,
	public_key BLOB NOT NULL,
	last_seen DATETIME,
	reputation_score INTEGER DEFAULT 50,
	cpu_cores INTEGER DEFAULT 0,
	storage_gb INTEGER DEFAULT 0,
	gpu_model TEXT DEFAULT ''
);

-- P2P pending sync queue
CREATE TABLE IF NOT EXISTS p2p_pending_sync (
	sync_id TEXT PRIMARY KEY,
	data_type TEXT NOT NULL,
	data BLOB NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	retry_count INTEGER DEFAULT 0
);

-- Rating alerts (catastrophic rating monitoring)
CREATE TABLE IF NOT EXISTS rating_alerts (
	alert_id TEXT PRIMARY KEY,
	transaction_id TEXT NOT NULL,
	user_did TEXT NOT NULL,
	provider_did TEXT NOT NULL,
	center_did TEXT NOT NULL,
	alert_type TEXT NOT NULL,
	severity TEXT NOT NULL,
	evidence BLOB,
	notes TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	resolution TEXT DEFAULT '',
	investigated_by TEXT DEFAULT '',
	investigated_at DATETIME,
	resolved_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_rating_alerts_status ON rating_alerts(status);
CREATE INDEX IF NOT EXISTS idx_rating_alerts_severity ON rating_alerts(severity, status);

-- Disputes
CREATE TABLE IF NOT EXISTS disputes (
	dispute_id TEXT PRIMARY KEY,
	transaction_id TEXT NOT NULL,
	filer_did TEXT NOT NULL,
	reason TEXT NOT NULL,
	priority TEXT NOT NULL DEFAULT 'normal',
	status TEXT NOT NULL DEFAULT 'open',
	resolution TEXT DEFAULT '',
	resolved_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (transaction_id) REFERENCES resource_transactions(transaction_id)
);

CREATE INDEX IF NOT EXISTS idx_disputes_status ON disputes(status);
CREATE INDEX IF NOT EXISTS idx_disputes_transaction ON disputes(transaction_id);

-- Investigations
CREATE TABLE IF NOT EXISTS investigations (
	investigation_id TEXT PRIMARY KEY,
	dispute_id TEXT NOT NULL,
	investigator_did TEXT DEFAULT '',
	status TEXT NOT NULL DEFAULT 'investigating',
	findings TEXT DEFAULT '',
	recommendation TEXT DEFAULT '',
	deadline DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	resolved_at DATETIME,
	FOREIGN KEY (dispute_id) REFERENCES disputes(dispute_id)
);

-- Center ratings (both parties rate the center after dispute resolution)
CREATE TABLE IF NOT EXISTS center_ratings (
	rating_id TEXT PRIMARY KEY,
	dispute_id TEXT NOT NULL,
	center_did TEXT NOT NULL,
	rater_did TEXT NOT NULL,
	rater_role TEXT NOT NULL,
	score INTEGER NOT NULL CHECK(score >= 0 AND score <= 5),
	feedback TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	signature BLOB,
	FOREIGN KEY (dispute_id) REFERENCES disputes(dispute_id)
);

CREATE INDEX IF NOT EXISTS idx_center_ratings_center ON center_ratings(center_did, created_at);

-- Center scores (aggregate reputation of central SOHO nodes)
CREATE TABLE IF NOT EXISTS center_scores (
	center_did TEXT PRIMARY KEY,
	overall_score INTEGER NOT NULL DEFAULT 50,
	investigation_quality REAL DEFAULT 0,
	fairness REAL DEFAULT 0,
	timeliness REAL DEFAULT 0,
	communication REAL DEFAULT 0,
	total_disputes INTEGER DEFAULT 0,
	total_ratings INTEGER DEFAULT 0,
	average_score REAL DEFAULT 0,
	score_history TEXT DEFAULT '[]',
	active BOOLEAN DEFAULT 1,
	suspended_at DATETIME,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- ── Enterprise Architecture Tables (v0.3) ──────────────────────────────────

-- Federation nodes (elastic orchestration node registry)
CREATE TABLE IF NOT EXISTS federation_nodes (
	node_did TEXT PRIMARY KEY,
	address TEXT NOT NULL,
	region TEXT NOT NULL DEFAULT '',
	total_cpu REAL DEFAULT 0,
	available_cpu REAL DEFAULT 0,
	total_memory_mb INTEGER DEFAULT 0,
	available_memory_mb INTEGER DEFAULT 0,
	total_disk_gb INTEGER DEFAULT 0,
	available_disk_gb INTEGER DEFAULT 0,
	gpu_model TEXT DEFAULT '',
	price_per_cpu_hour INTEGER DEFAULT 0,
	reputation_score INTEGER DEFAULT 50,
	uptime_percent REAL DEFAULT 0,
	failure_rate REAL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'offline',
	last_heartbeat DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_federation_nodes_status ON federation_nodes(status);
CREATE INDEX IF NOT EXISTS idx_federation_nodes_region ON federation_nodes(region, status);

-- Workloads (elastic orchestration workload definitions)
CREATE TABLE IF NOT EXISTS workloads (
	workload_id TEXT PRIMARY KEY,
	owner_did TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	workload_type TEXT NOT NULL DEFAULT 'container',
	replicas INTEGER DEFAULT 1,
	status TEXT NOT NULL DEFAULT 'pending',
	image TEXT DEFAULT '',
	cpu_cores REAL DEFAULT 0,
	memory_mb INTEGER DEFAULT 0,
	disk_gb INTEGER DEFAULT 0,
	gpu_required BOOLEAN DEFAULT 0,
	gpu_model TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_workloads_owner ON workloads(owner_did);
CREATE INDEX IF NOT EXISTS idx_workloads_status ON workloads(status);

-- Placements (workload replica → node assignments)
CREATE TABLE IF NOT EXISTS placements (
	placement_id TEXT PRIMARY KEY,
	workload_id TEXT NOT NULL,
	replica_num INTEGER NOT NULL DEFAULT 0,
	node_did TEXT NOT NULL,
	node_address TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (workload_id) REFERENCES workloads(workload_id)
);

CREATE INDEX IF NOT EXISTS idx_placements_workload ON placements(workload_id);
CREATE INDEX IF NOT EXISTS idx_placements_node ON placements(node_did);

-- Managed service instances (PostgreSQL, S3, SQS)
CREATE TABLE IF NOT EXISTS service_instances (
	instance_id TEXT PRIMARY KEY,
	owner_did TEXT NOT NULL,
	service_type TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	plan TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'provisioning',
	node_did TEXT DEFAULT '',
	endpoint TEXT DEFAULT '',
	port INTEGER DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_service_instances_owner ON service_instances(owner_did);
CREATE INDEX IF NOT EXISTS idx_service_instances_type ON service_instances(service_type, status);

-- SLA contracts
CREATE TABLE IF NOT EXISTS sla_contracts (
	contract_id TEXT PRIMARY KEY,
	owner_did TEXT NOT NULL,
	tier TEXT NOT NULL DEFAULT 'basic',
	status TEXT NOT NULL DEFAULT 'active',
	uptime_target REAL DEFAULT 99.0,
	latency_target_ms INTEGER DEFAULT 200,
	start_date DATETIME NOT NULL,
	end_date DATETIME NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sla_contracts_owner ON sla_contracts(owner_did);
CREATE INDEX IF NOT EXISTS idx_sla_contracts_status ON sla_contracts(status);

-- SLA violations
CREATE TABLE IF NOT EXISTS sla_violations (
	violation_id TEXT PRIMARY KEY,
	contract_id TEXT NOT NULL,
	violation_type TEXT NOT NULL,
	severity TEXT NOT NULL DEFAULT 'minor',
	measured_value REAL DEFAULT 0,
	target_value REAL DEFAULT 0,
	credit_amount INTEGER DEFAULT 0,
	detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	resolved_at DATETIME,
	FOREIGN KEY (contract_id) REFERENCES sla_contracts(contract_id)
);

CREATE INDEX IF NOT EXISTS idx_sla_violations_contract ON sla_violations(contract_id, detected_at);
CREATE INDEX IF NOT EXISTS idx_sla_violations_severity ON sla_violations(severity);

-- Blockchain batch anchoring (local chain)
CREATE TABLE IF NOT EXISTS blockchain_batches (
	height INTEGER PRIMARY KEY,
	merkle_root BLOB NOT NULL,
	prev_hash BLOB NOT NULL,
	hash BLOB NOT NULL UNIQUE,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	node_did TEXT NOT NULL DEFAULT '',
	signature BLOB,
	source_file TEXT DEFAULT '',
	leaf_count INTEGER DEFAULT 0,
	tree_height INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_blockchain_batches_hash ON blockchain_batches(hash);

-- Device tokens: Flutter / mobile sessions authenticated with owner Ed25519 key.
CREATE TABLE IF NOT EXISTS device_tokens (
	token_hash  TEXT     PRIMARY KEY,      -- SHA-256(raw_token) as hex
	device_name TEXT     NOT NULL DEFAULT 'unknown',
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_seen   DATETIME DEFAULT CURRENT_TIMESTAMP,
	revoked     INTEGER  NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_device_tokens_revoked ON device_tokens(revoked);

-- Schema versioning: track which migrations have been applied.
CREATE TABLE IF NOT EXISTS schema_version (
	version    INTEGER NOT NULL,
	applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// NewStore opens or creates a SQLite database at the given path and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// SQLite: single-writer required — cap the connection pool to 1.
	// This prevents "database is locked" SQLITE_BUSY errors under concurrent
	// goroutine access.  Reads through the same connection are still fast
	// because WAL mode allows concurrent readers to proceed without blocking.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // keep the connection open indefinitely

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Set a busy timeout so that writers retry for up to 5 s before returning
	// SQLITE_BUSY.  This provides a last-resort safety net if SetMaxOpenConns(1)
	// is ever bypassed (e.g. via the raw DB() accessor).
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Apply base schema (all CREATE TABLE IF NOT EXISTS — idempotent).
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run schema migration: %w", err)
	}

	s := &Store{db: db}

	// Apply versioned migrations on top of the base schema.
	if err := s.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

// NewMemoryStore creates an in-memory SQLite store for testing.
func NewMemoryStore() (*Store, error) {
	return NewStore(":memory:")
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping verifies the database connection is alive.
// Used by health checks and readiness probes.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// DB returns the underlying *sql.DB for advanced operations.
func (s *Store) DB() *sql.DB {
	return s.db
}

// SetNodeInfo stores a key-value pair in the node_info table.
func (s *Store) SetNodeInfo(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO node_info (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// GetNodeInfo retrieves a value from the node_info table.
func (s *Store) GetNodeInfo(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM node_info WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

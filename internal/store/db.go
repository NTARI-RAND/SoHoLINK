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

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Run schema migrations
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run schema migration: %w", err)
	}

	return &Store{db: db}, nil
}

// NewMemoryStore creates an in-memory SQLite store for testing.
func NewMemoryStore() (*Store, error) {
	return NewStore(":memory:")
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
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

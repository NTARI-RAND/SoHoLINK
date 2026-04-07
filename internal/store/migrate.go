package store

import (
	"fmt"
	"log"
)

// migrations is an ordered list of SQL statements, one per schema version.
// Version numbers are 1-based: migrations[0] takes the DB from v0 → v1.
//
// Rules:
//   - Only append; never edit or remove existing entries.
//   - Each entry must be idempotent (use CREATE TABLE IF NOT EXISTS, etc.).
//   - Keep each migration as a single string; multiple statements are fine.
var migrations = []string{
	// v1: payouts table — records every payout request and its lifecycle.
	`CREATE TABLE IF NOT EXISTS payouts (
		payout_id    TEXT    PRIMARY KEY,
		provider_did TEXT    NOT NULL,
		amount_sats  INTEGER NOT NULL,
		processor    TEXT    NOT NULL DEFAULT '',
		status       TEXT    NOT NULL DEFAULT 'pending',
		external_id  TEXT             DEFAULT '',
		error_msg    TEXT             DEFAULT '',
		requested_at DATETIME         DEFAULT CURRENT_TIMESTAMP,
		settled_at   DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_payouts_provider ON payouts(provider_did, status);`,

	// v2: add public_key column to federation_nodes for Ed25519 verification.
	// SQLite supports ADD COLUMN with a DEFAULT value even for NOT NULL columns.
	`ALTER TABLE federation_nodes ADD COLUMN public_key TEXT NOT NULL DEFAULT '';`,

	// v3: buyer-side marketplace tables.
	`CREATE TABLE IF NOT EXISTS wallet_balances (
		did          TEXT    PRIMARY KEY,
		balance_sats INTEGER NOT NULL DEFAULT 0,
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS wallet_topups (
		topup_id     TEXT    PRIMARY KEY,
		did          TEXT    NOT NULL,
		amount_sats  INTEGER NOT NULL,
		processor    TEXT    NOT NULL DEFAULT '',
		invoice      TEXT    NOT NULL DEFAULT '',
		status       TEXT    NOT NULL DEFAULT 'awaiting_payment',
		created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
		confirmed_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_wallet_topups_did ON wallet_topups(did, status);

	CREATE TABLE IF NOT EXISTS orders (
		order_id        TEXT    PRIMARY KEY,
		requester_did   TEXT    NOT NULL,
		order_type      TEXT    NOT NULL DEFAULT 'workload',
		resource_ref_id TEXT    NOT NULL DEFAULT '',
		description     TEXT    NOT NULL DEFAULT '',
		cpu_cores       REAL    NOT NULL DEFAULT 0,
		memory_mb       INTEGER NOT NULL DEFAULT 0,
		disk_gb         INTEGER NOT NULL DEFAULT 0,
		duration_hours  INTEGER NOT NULL DEFAULT 0,
		estimated_sats  INTEGER NOT NULL DEFAULT 0,
		charged_sats    INTEGER NOT NULL DEFAULT 0,
		status          TEXT    NOT NULL DEFAULT 'pending',
		created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_orders_requester ON orders(requester_did, status);`,

	// v4: content safety & legal compliance — CSAM hash blocklist, DID blocklist,
	// and workload manifest storage for audit purposes.
	`ALTER TABLE orders ADD COLUMN manifest_json TEXT NOT NULL DEFAULT '';

	CREATE TABLE IF NOT EXISTS content_hash_blocklist (
		hash_sha256   TEXT PRIMARY KEY,
		hash_type     TEXT NOT NULL DEFAULT 'sha256',
		reason        TEXT NOT NULL,
		source        TEXT NOT NULL,
		added_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		added_by_did  TEXT NOT NULL DEFAULT 'platform'
	);

	CREATE TABLE IF NOT EXISTS blocked_dids (
		did           TEXT PRIMARY KEY,
		reason        TEXT NOT NULL,
		blocked_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		blocked_by    TEXT NOT NULL DEFAULT 'platform',
		expires_at    DATETIME,
		propagated    INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_blocked_dids_expires ON blocked_dids(expires_at);`,

	// v5: payment idempotency & rental audit log.
	//
	// wallet_topups: add idempotency_key so duplicate topup requests (network
	// retries, webhook re-delivery) are silently deduplicated.
	//
	// rental_audit: append-only log of every auto-accept/auto-reject decision
	// made by the rental engine, for compliance review.
	`ALTER TABLE wallet_topups ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT '';
	CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_topups_idempotency
		ON wallet_topups(idempotency_key) WHERE idempotency_key != '';

	CREATE TABLE IF NOT EXISTS rental_audit (
		id          INTEGER  PRIMARY KEY AUTOINCREMENT,
		request_id  TEXT     NOT NULL,
		user_did    TEXT     NOT NULL,
		rule_id     TEXT     NOT NULL DEFAULT '',
		action      TEXT     NOT NULL,   -- 'auto_accept' | 'auto_reject' | 'pending'
		reason      TEXT     NOT NULL DEFAULT '',
		decided_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_rental_audit_request ON rental_audit(request_id);
	CREATE INDEX IF NOT EXISTS idx_rental_audit_user ON rental_audit(user_did, decided_at);`,

	// v6: compliance framework — adds compliance level, group, SLA tier, and
	// attestation fields to federation_nodes; adds compliance_audit log table.
	`ALTER TABLE federation_nodes ADD COLUMN compliance_level TEXT NOT NULL DEFAULT 'baseline';
	ALTER TABLE federation_nodes ADD COLUMN compliance_group TEXT;
	ALTER TABLE federation_nodes ADD COLUMN sla_tier TEXT NOT NULL DEFAULT 'best-effort';
	ALTER TABLE federation_nodes ADD COLUMN attestation_data TEXT;
	ALTER TABLE federation_nodes ADD COLUMN last_compliance_check INTEGER;

	CREATE TABLE IF NOT EXISTS compliance_audit (
		audit_id    TEXT    PRIMARY KEY,
		node_did    TEXT    NOT NULL,
		check_time  INTEGER NOT NULL,
		level       TEXT    NOT NULL DEFAULT 'baseline',
		passed      INTEGER NOT NULL DEFAULT 0,
		details     TEXT    NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_compliance_audit_node ON compliance_audit(node_did, check_time);`,

	// v7: FCM device token registry — stores Android FCM tokens so the server
	// can send background wakeup pushes when a mobile node's WebSocket is
	// disconnected and a task needs to be dispatched.
	`CREATE TABLE IF NOT EXISTS device_fcm_tokens (
		node_did   TEXT     PRIMARY KEY,
		fcm_token  TEXT     NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`,

	// v8: TPM 2.0 attestation — adds hardware-bound attestation columns to
	// federation_nodes and an append-only TPM attestation log table.
	//
	// tpm_ek_cert:     DER-encoded Endorsement Key certificate (base64).
	// tpm_pcr_quote:   Last TPMS_ATTEST quote blob (base64).
	// tpm_quote_sig:   TPM signature over the quote (base64).
	// tpm_quote_nonce: Anti-replay nonce used for the last quote (base64).
	// tpm_quote_ts:    Unix timestamp of the last TPM quote.
	`ALTER TABLE federation_nodes ADD COLUMN tpm_ek_cert TEXT NOT NULL DEFAULT '';
	ALTER TABLE federation_nodes ADD COLUMN tpm_pcr_quote TEXT NOT NULL DEFAULT '';
	ALTER TABLE federation_nodes ADD COLUMN tpm_quote_sig TEXT NOT NULL DEFAULT '';
	ALTER TABLE federation_nodes ADD COLUMN tpm_quote_nonce TEXT NOT NULL DEFAULT '';
	ALTER TABLE federation_nodes ADD COLUMN tpm_quote_ts INTEGER NOT NULL DEFAULT 0;

	CREATE TABLE IF NOT EXISTS tpm_attestations (
		attestation_id  TEXT    PRIMARY KEY,
		node_did        TEXT    NOT NULL,
		level           TEXT    NOT NULL DEFAULT 'baseline',
		tpm_ek_cert     TEXT    NOT NULL DEFAULT '',
		tpm_pcr_quote   TEXT    NOT NULL DEFAULT '',
		tpm_quote_sig   TEXT    NOT NULL DEFAULT '',
		nonce           TEXT    NOT NULL DEFAULT '',
		software_sig    TEXT    NOT NULL DEFAULT '',
		attested_at     INTEGER NOT NULL DEFAULT 0,
		verified        INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_tpm_attestations_node ON tpm_attestations(node_did, attested_at);`,
}

// runMigrations applies any unapplied migrations to the database.
// It is idempotent: running it on an already-up-to-date database is a no-op.
func (s *Store) runMigrations() error {
	// Seed the version table with v0 if it has never been written.
	_, _ = s.db.Exec(`
		INSERT OR IGNORE INTO schema_version (version)
		SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM schema_version)`)

	var current int
	_ = s.db.QueryRow(
		"SELECT version FROM schema_version ORDER BY version DESC LIMIT 1",
	).Scan(&current)

	for i := current; i < len(migrations); i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration v%d: %w", i+1, err)
		}
		if _, err := s.db.Exec(
			"INSERT INTO schema_version (version) VALUES (?)", i+1,
		); err != nil {
			return fmt.Errorf("recording migration v%d: %w", i+1, err)
		}
		log.Printf("[store] applied schema migration v%d", i+1)
	}
	return nil
}

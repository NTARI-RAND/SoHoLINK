-- 021_operators.up.sql
-- Operator registry for the frontend-as-operator identity layer (Layer C):
-- OPEN, permissionless signup. Anyone applies; the mechanical gates are
-- email-2FA + the automated conformance test. Passing BOTH auto-activates the
-- operator (no human in the entry path); the :8090 admin can DISCONNECT/REVOKE
-- afterward. A pending/unverified operator's keys authenticate NOTHING — the
-- GetActiveKeyMap chokepoint (later step) returns nil unless status='active'
-- AND onboarding_state='active'.
--
-- Canon-native: raw 32-byte Ed25519 public keys (not SPKI PEM). New ENUMs use
-- CREATE TYPE (not ALTER TYPE ADD VALUE), so the same-transaction enum rule in
-- CLAUDE.md is not tripped. Migrations 021-023 extend 001-020 with no
-- renumbering and no destructive change. Fresh registry; no backfill.

CREATE TYPE operator_status AS ENUM ('active', 'revoked');
CREATE TYPE operator_onboarding_state AS ENUM ('pending_verification', 'verified', 'active');
CREATE TYPE operator_key_state AS ENUM ('active', 'expired', 'retired', 'revoked');

CREATE TABLE operators (
    id                      TEXT PRIMARY KEY,                       -- stable slug, e.g. 'cloudy'
    name                    TEXT NOT NULL,
    email                   TEXT NOT NULL,                          -- normalized lowercase/trim; see CHECK
    phone                   TEXT,                                   -- nullable: phone-2FA deferred
    status                  operator_status           NOT NULL DEFAULT 'active',
    onboarding_state        operator_onboarding_state NOT NULL DEFAULT 'pending_verification',
    email_verified          BOOLEAN NOT NULL DEFAULT FALSE,
    conformance_passed_at   TIMESTAMPTZ,                            -- set by the harness on full pass
    conformance_keyset_hash BYTEA,                                  -- H(sorted 7 pubkeys) that passed
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Email is stored normalized (lowercase, trimmed) by the application; this
    -- CHECK makes the normalization a hard DB-boundary invariant so a UNIQUE
    -- email cannot be bypassed by case/whitespace variation.
    CONSTRAINT operators_email_normalized_chk CHECK (email = lower(btrim(email))),
    CONSTRAINT operators_email_key UNIQUE (email)
);

CREATE TABLE operator_keys (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id          TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    key_index            INT  NOT NULL CHECK (key_index BETWEEN 0 AND 6),
    public_key           BYTEA NOT NULL,                            -- raw 32-byte ed25519 for v0
    algo                 TEXT NOT NULL DEFAULT 'ed25519',
    state                operator_key_state NOT NULL DEFAULT 'active',
    usage_count          INT  NOT NULL DEFAULT 0 CHECK (usage_count >= 0),
    expiration_threshold INT  NOT NULL CHECK (expiration_threshold > 0), -- drawn from EXPIRATIONS at insert
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- At most one ACTIVE key per (operator, index): the 2-of-7 hot path resolves an
-- index to a single active public key. Expired/retired/revoked rows may coexist
-- as history at the same index.
CREATE UNIQUE INDEX uq_opkeys_active ON operator_keys(operator_id, key_index) WHERE state = 'active';
CREATE INDEX idx_opkeys_operator ON operator_keys(operator_id, state);

CREATE TABLE operator_verifications (                              -- session-bound 2FA codes
    operator_id  TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    channel      TEXT NOT NULL CHECK (channel IN ('email')),       -- 'phone' deferred (phone-2FA)
    code         TEXT NOT NULL,
    attempts     INT  NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    session_id   TEXT NOT NULL,                                    -- bound to the applicant session
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (operator_id, channel)
);

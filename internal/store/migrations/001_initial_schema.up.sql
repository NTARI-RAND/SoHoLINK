-- Enums
CREATE TYPE node_class AS ENUM ('A', 'B', 'C', 'D');

CREATE TYPE node_status AS ENUM ('online', 'offline', 'draining');

CREATE TYPE workload_type AS ENUM (
    'app_hosting',
    'object_storage',
    'cdn_edge',
    'batch_compute',
    'ai_inference'
);

CREATE TYPE job_status AS ENUM (
    'pending',
    'scheduled',
    'running',
    'completed',
    'failed',
    'disputed'
);

-- Providers: hardware owners with Stripe Connect accounts
CREATE TABLE providers (
    id                         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email                      TEXT        NOT NULL UNIQUE,
    display_name               TEXT        NOT NULL,
    spiffe_id                  TEXT        UNIQUE,           -- spiffe://soholink.org/provider/<id>
    stripe_account_id          TEXT        UNIQUE,           -- Stripe Connect acct_...
    stripe_onboarding_complete BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Consumers: workload buyers
CREATE TABLE consumers (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email              TEXT        NOT NULL UNIQUE,
    display_name       TEXT        NOT NULL,
    stripe_customer_id TEXT        UNIQUE,                   -- Stripe cus_...
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Nodes: individual hardware units registered by providers
CREATE TABLE nodes (
    id                UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id       UUID             NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    spiffe_id         TEXT             UNIQUE,               -- spiffe://soholink.org/node/<id>
    node_class        node_class       NOT NULL,
    hostname          TEXT             NOT NULL,
    country_code      CHAR(2)          NOT NULL,             -- ISO 3166-1 alpha-2
    region            TEXT,
    latitude          DOUBLE PRECISION,
    longitude         DOUBLE PRECISION,
    status            node_status      NOT NULL DEFAULT 'offline',
    last_heartbeat_at TIMESTAMPTZ,
    hardware_profile  JSONB            NOT NULL DEFAULT '{}', -- agent-reported: cpu_cores, ram_mb, gpu, storage_gb, bandwidth_mbps
    created_at        TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_nodes_provider_id  ON nodes(provider_id);
CREATE INDEX idx_nodes_status       ON nodes(status);
CREATE INDEX idx_nodes_country_code ON nodes(country_code);
CREATE INDEX idx_nodes_node_class   ON nodes(node_class);

-- Resource profiles: per-node capacity caps and on/off toggles
-- Every node has exactly one default profile; scheduled overrides are additional rows.
CREATE TABLE resource_profiles (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id             UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,
    is_default          BOOLEAN     NOT NULL DEFAULT FALSE,
    cpu_enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    gpu_pct             SMALLINT    NOT NULL DEFAULT 100 CHECK (gpu_pct        BETWEEN 0 AND 100),
    ram_pct             SMALLINT    NOT NULL DEFAULT 100 CHECK (ram_pct        BETWEEN 0 AND 100),
    storage_gb          INTEGER     NOT NULL DEFAULT 0   CHECK (storage_gb     >= 0),
    bandwidth_mbps      INTEGER     NOT NULL DEFAULT 0   CHECK (bandwidth_mbps >= 0),
    -- Recurring weekly schedule window (NULL on the default profile)
    schedule_start      TIME,       -- e.g. '22:00' — start of availability window
    schedule_end        TIME,       -- e.g. '06:00' — end (may wrap midnight)
    schedule_days       TEXT[],     -- e.g. ARRAY['mon','tue','wed','thu','fri']
    -- Absolute date range for this override (NULL = no date bounds)
    override_start_date DATE,
    override_end_date   DATE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one default profile per node
CREATE UNIQUE INDEX uq_resource_profiles_node_default
    ON resource_profiles(node_id)
    WHERE is_default = TRUE;

CREATE INDEX idx_resource_profiles_node_id ON resource_profiles(node_id);

-- Jobs: workload execution records
CREATE TABLE jobs (
    id                    UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_id           UUID          NOT NULL REFERENCES consumers(id),
    node_id               UUID          REFERENCES nodes(id) ON DELETE SET NULL,
    workload_type         workload_type NOT NULL,
    status                job_status    NOT NULL DEFAULT 'pending',
    -- Geo constraints — scheduler refuses to violate a non-NULL country_constraint
    country_constraint    CHAR(2),                              -- ISO 3166-1 alpha-2; NULL = any
    region_constraint     TEXT,
    -- Resource requirements (NULL = no constraint)
    cpu_cores             INTEGER,
    ram_mb                INTEGER,
    storage_gb            INTEGER,
    bandwidth_mbps        INTEGER,
    gpu_required          BOOLEAN       NOT NULL DEFAULT FALSE,
    -- Orchestrator-issued token — set after node match, presented by agent at job start
    job_token             TEXT          UNIQUE,
    -- Payment — all values in cents (BIGINT per coding conventions)
    amount_cents          BIGINT        NOT NULL DEFAULT 0,
    platform_fee_cents    BIGINT        NOT NULL DEFAULT 0,
    provider_payout_cents BIGINT        NOT NULL DEFAULT 0,
    stripe_charge_id      TEXT,
    stripe_transfer_id    TEXT,
    -- Lifecycle
    started_at            TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_jobs_consumer_id ON jobs(consumer_id);
CREATE INDEX idx_jobs_node_id     ON jobs(node_id);
CREATE INDEX idx_jobs_status      ON jobs(status);
CREATE INDEX idx_jobs_created_at  ON jobs(created_at DESC);

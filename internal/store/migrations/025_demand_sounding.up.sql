-- 025_demand_sounding.up.sql
-- Demand-sounding telemetry: the :8090-only "rungs" dashboard reads these.
-- "Rungs" are compute-capacity TIERS (cloud stages: cumulus -> congestus ->
-- cumulonimbus -> storm). A demand sounding shows a tier forming before it
-- exists: jobs towering against the top rung's ceiling = congestus building =
-- ship the storm tier. We track BOTH sides — demand (job shapes, rejections)
-- and contributor capacity (snapshots). Because every operator's nodes/jobs
-- pool in one coordinator, EVERY event row is tagged operator_id so the
-- dashboard slices PER-OPERATOR and AGGREGATES across all platforms.
--
-- This is the FIRST hypertable migration in the tree, so it enables the
-- TimescaleDB extension (idempotent; the timescale/timescaledb image ships the
-- library in shared_preload_libraries but a freshly-created DB may not yet have
-- the extension registered). The three EVENT tables become hypertables on their
-- `time` column; rung_tiers is plain config the dashboard reads.
--
-- Design notes honoring CLAUDE.md migration rules + hot-path/telemetry safety:
--   * No ALTER TYPE ADD VALUE — set-membership fields (reason, state) use TEXT +
--     CHECK, which also lets us seed rung_tiers with state='coming_soon' in THIS
--     same transaction (an enum's new value could not be USED in the tx that
--     creates it — the migration-015 lesson).
--   * The event tables are OBSERVABILITY sinks written fire-and-forget from an
--     async drain goroutine that fails-open. They deliberately carry NO foreign
--     keys (to operators, jobs, or rung_tiers) and no NOT-VALID-able constraints
--     that could make a telemetry INSERT fail on a benign race (e.g. an operator
--     row pruned, a workload string the enum hasn't learned yet). Referential
--     columns (operator_id, workload_type, node_class, rung) are stored as TEXT
--     for the same decoupling reason. Correctness of placement always wins;
--     telemetry never blocks or errors the hot path.
--   * `time` is a non-reserved keyword in Postgres and is used unquoted as the
--     partitioning column, matching common TimescaleDB convention.
--   * The spec's `order` column is materialized as `tier_order` — `order` is a
--     reserved word that would force quoting in every downstream query; the
--     rename removes that footgun. Steps 2-3 read `tier_order`.
--
-- Extends 001-024 with no renumbering and no destructive change.

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ---------------------------------------------------------------------------
-- DEMAND side 1: one row per submitted job's SHAPE (placed or not).
-- Feeds the intensity x duration scatter with the rung-ceiling threshold line,
-- the job-shape-vs-rung-boundary distribution, and the "jobs against the top
-- rung's ceiling" headline stat.
-- ---------------------------------------------------------------------------
CREATE TABLE operator_job_shapes (
    time          TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    operator_id   TEXT             NOT NULL,              -- authenticated operator context (frontend-as-operator)
    job_id        UUID             NOT NULL,              -- jobs.id (no FK: decoupled telemetry)
    workload_type TEXT             NOT NULL,              -- marketplace workload string (no enum coupling)
    intensity     DOUBLE PRECISION NOT NULL DEFAULT 0,    -- normalized compute intensity (scatter Y-candidate)
    duration_est  INTEGER          NOT NULL DEFAULT 0,    -- estimated duration, SECONDS (scatter X-candidate)
    cpu           DOUBLE PRECISION NOT NULL DEFAULT 0,    -- requested vCPU (fractional allowed)
    mem_mb        BIGINT           NOT NULL DEFAULT 0,    -- requested memory, MB
    disk_mb       BIGINT           NOT NULL DEFAULT 0,    -- requested disk, MB
    footprint     DOUBLE PRECISION NOT NULL DEFAULT 0,    -- composite size measure used for tier-fit / too_big
    placed        BOOLEAN          NOT NULL,              -- did the placement decision find a home?
    rung          TEXT                                    -- which rung it fit (NULL when not placed)
);
SELECT create_hypertable('operator_job_shapes', 'time', if_not_exists => TRUE);
-- Per-operator time slices AND (with all-operators aggregation) the dashboard's
-- primary access pattern. TimescaleDB already maintains a time-DESC index.
CREATE INDEX idx_job_shapes_operator_time ON operator_job_shapes (operator_id, time DESC);
-- The "jobs piling against the top rung" query scans by rung over time.
CREATE INDEX idx_job_shapes_rung_time     ON operator_job_shapes (rung, time DESC);

-- ---------------------------------------------------------------------------
-- DEMAND side 2: the purest UNMET-demand signal — placement rejections.
-- Feeds the rejection-by-reason-over-time stacked area / small multiples.
-- ---------------------------------------------------------------------------
CREATE TABLE operator_placement_rejections (
    time          TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    operator_id   TEXT             NOT NULL,
    job_id        UUID             NOT NULL,
    workload_type TEXT             NOT NULL,
    reason        TEXT             NOT NULL
        CHECK (reason IN ('too_big', 'no_matching_tier', 'had_to_split', 'opted_out', 'no_capacity')),
    footprint     DOUBLE PRECISION NOT NULL DEFAULT 0,    -- size of the rejected job (how far past the ceiling)
    wanted_rung   TEXT                                    -- the tier it wanted / would have needed (NULL if unknown)
);
SELECT create_hypertable('operator_placement_rejections', 'time', if_not_exists => TRUE);
CREATE INDEX idx_rejections_operator_time ON operator_placement_rejections (operator_id, time DESC);
-- The by-reason categorical chart scans reason over time.
CREATE INDEX idx_rejections_reason_time   ON operator_placement_rejections (reason, time DESC);

-- ---------------------------------------------------------------------------
-- SUPPLY side: contributor capacity sampled from heartbeats / registry.
-- Feeds the capacity-vs-demand two-line (single-axis) view.
-- ---------------------------------------------------------------------------
CREATE TABLE operator_capacity_snapshots (
    time            TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    operator_id     TEXT             NOT NULL,
    node_class      TEXT             NOT NULL,            -- 'A'..'D' (no enum coupling)
    workload_type   TEXT             NOT NULL,
    nodes_available INTEGER          NOT NULL DEFAULT 0,
    vcpus           INTEGER          NOT NULL DEFAULT 0,  -- total available vCPU across the class
    mem_mb          BIGINT           NOT NULL DEFAULT 0,  -- total available memory, MB
    disk_mb         BIGINT           NOT NULL DEFAULT 0,  -- total available disk, MB
    print_qps       DOUBLE PRECISION NOT NULL DEFAULT 0   -- print throughput capacity (fractional)
);
SELECT create_hypertable('operator_capacity_snapshots', 'time', if_not_exists => TRUE);
CREATE INDEX idx_capacity_operator_time ON operator_capacity_snapshots (operator_id, time DESC);

-- ---------------------------------------------------------------------------
-- CONFIG: the rung (capacity-tier) ladder the dashboard reads. Weather-stage
-- names, monotonically increasing ceilings, top tier 'coming_soon' = the
-- fake-door "storm" tier. Ceilings share units with operator_job_shapes:
-- cpu_ceiling in vCPU, mem_ceiling in MB, disk_ceiling in MB.
-- ---------------------------------------------------------------------------
CREATE TABLE rung_tiers (
    name         TEXT             PRIMARY KEY,            -- weather cloud stage, e.g. 'cumulus'
    tier_order   INTEGER          NOT NULL UNIQUE,        -- spec's `order`; renamed off the reserved word
    cpu_ceiling  DOUBLE PRECISION NOT NULL,              -- vCPU ceiling for a job to fit this tier
    mem_ceiling  BIGINT           NOT NULL,              -- memory ceiling, MB
    disk_ceiling BIGINT           NOT NULL,              -- disk ceiling, MB
    state        TEXT             NOT NULL
        CHECK (state IN ('available', 'coming_soon'))
);

-- Seed the ladder: three available stages + a top 'coming_soon' storm tier.
INSERT INTO rung_tiers (name, tier_order, cpu_ceiling, mem_ceiling, disk_ceiling, state) VALUES
    ('cumulus',      1,   2,   4096,   20480, 'available'),
    ('congestus',    2,   8,  16384,  102400, 'available'),
    ('cumulonimbus', 3,  32,  65536,  512000, 'available'),
    ('storm',        4, 128, 262144, 2097152, 'coming_soon');

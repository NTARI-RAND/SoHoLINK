-- 018_job_lifecycle_statuses.up.sql
-- Extends job_status with the dispatched/awaiting_pickup/picked_up/delivered
-- statuses needed by the long-running job lifecycle in sub-phase B5. Adds
-- four nullable columns on `jobs` to track pickup/delivery timestamps,
-- container exit codes, and failure causes.
--
-- The B5 design pairs handleGetJobs's 'scheduled → dispatched' atomic claim
-- with a POST /jobs/{id}/started confirmation that transitions to 'running'
-- (B5 commit 2). Print workloads then flow through 'awaiting_pickup →
-- picked_up → delivered' (B5 commits 5+). The existing `started_at` column
-- from migration 001 is populated by /started in commit 2 — not added here.
--
-- Schema-only; no existing rows reference these values. New columns are
-- nullable so existing rows remain valid without backfill.

ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'dispatched';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'awaiting_pickup';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'picked_up';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'delivered';

ALTER TABLE jobs
    ADD COLUMN picked_up_at  TIMESTAMPTZ,
    ADD COLUMN delivered_at  TIMESTAMPTZ,
    ADD COLUMN exit_code     INTEGER,
    ADD COLUMN failure_cause TEXT;

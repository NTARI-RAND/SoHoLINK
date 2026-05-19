-- 018_job_lifecycle_statuses.down.sql
-- Reverses 018_job_lifecycle_statuses.up.sql.
--
-- NOTE: PostgreSQL does not support removing values from an existing ENUM
-- type. The 'dispatched', 'awaiting_pickup', 'picked_up', and 'delivered'
-- values added by 018.up remain after this rollback. A complete rollback
-- requires recreating job_status from scratch (cascading through the jobs
-- table) and is out of scope for an incremental migration. Restore from a
-- pre-018 snapshot if full enum removal is required.

ALTER TABLE jobs
    DROP COLUMN IF EXISTS picked_up_at,
    DROP COLUMN IF EXISTS delivered_at,
    DROP COLUMN IF EXISTS exit_code,
    DROP COLUMN IF EXISTS failure_cause;

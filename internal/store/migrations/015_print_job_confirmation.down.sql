-- 015_print_job_confirmation.down.sql
-- Reverses 015_print_job_confirmation.up.sql.
--
-- NOTE: PostgreSQL does not support removing values from an existing
-- ENUM type. The 'awaiting_confirmation' and 'declined' values added
-- by 015.up remain after this rollback. This is safe — existing code
-- that doesn't reference them simply ignores them. A complete rollback
-- requires recreating job_status from scratch (which would cascade
-- through the jobs table) and is out of scope for an incremental
-- migration. Restore from a pre-015 snapshot if full enum removal is
-- required.

DROP INDEX IF EXISTS idx_jobs_confirmation_deadline;

ALTER TABLE jobs
  DROP COLUMN IF EXISTS confirmation_deadline,
  DROP COLUMN IF EXISTS declined_at,
  DROP COLUMN IF EXISTS confirmed_at,
  DROP COLUMN IF EXISTS spec_hash,
  DROP COLUMN IF EXISTS printer_id;

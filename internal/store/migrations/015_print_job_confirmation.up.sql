-- 015_print_job_confirmation.up.sql
-- Adds the lifecycle states and columns needed for B4 (print job
-- confirmation flow). The contributor must explicitly acknowledge a
-- print spec before the agent starts the container; on decline or
-- timeout, the orchestrator routes the job to another node.
--
-- This migration is schema-only. The dispatcher logic that writes the
-- new state, the agent code that fills ConnectionPath, the portal
-- confirmation page, and the auto-decline sweeper all land in
-- follow-up B4 commits. With only this migration applied, behavior
-- is unchanged: new columns default to NULL, new enum values are
-- unreferenced.
--
-- ALTER TYPE ... ADD VALUE is permitted inside a transaction on
-- PostgreSQL 12+; the runtime restriction is only on USING the new
-- value in the same transaction, which this migration does not do.

ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'awaiting_confirmation';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'declined';

-- Lifecycle columns. NULL until the corresponding state is reached.
--   printer_id            — the specific printer the orchestrator
--                            assigned at match time.
--   spec_hash             — SHA-256 of the canonicalized job spec
--                            presented to the contributor at
--                            confirmation; detects drift between
--                            acknowledged and dispatched spec.
--   confirmed_at          — timestamp of contributor acknowledgment.
--   declined_at           — timestamp of decline (manual or sweeper).
--   confirmation_deadline — set by the dispatcher (typically
--                            NOW() + 4 hours); past-deadline rows in
--                            'awaiting_confirmation' are auto-declined
--                            by the sweeper.
ALTER TABLE jobs
  ADD COLUMN printer_id            TEXT,
  ADD COLUMN spec_hash             BYTEA,
  ADD COLUMN confirmed_at          TIMESTAMPTZ,
  ADD COLUMN declined_at           TIMESTAMPTZ,
  ADD COLUMN confirmation_deadline TIMESTAMPTZ;

-- Invariant: when jobs.printer_id is non-NULL, the row
-- (jobs.node_id, jobs.printer_id) MUST exist in node_printers. This
-- pairing is enforced by the orchestrator at dispatch and by the
-- agent at job-poll. We deliberately do NOT add a composite FK
-- constraint: ON DELETE SET NULL on the composite would nullify
-- jobs.node_id on routine printer cleanup (defeating the single-
-- column node_id FK from migration 001, which nullifies only on
-- actual node deletion), and ON DELETE RESTRICT would block node
-- deletion via cascade through node_printers.

-- Partial index supports the auto-decline sweeper's query:
--   SELECT id FROM jobs
--   WHERE status = 'awaiting_confirmation'
--     AND confirmation_deadline < NOW()
CREATE INDEX idx_jobs_confirmation_deadline
  ON jobs (confirmation_deadline)
  WHERE status = 'awaiting_confirmation';

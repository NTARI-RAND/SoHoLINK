-- 019_awaiting_pickup_anchor.up.sql
-- C5: anchor column for the print lifecycle no-show window.
--
-- The no-show window (7 days, pilot setting) needs a stable timestamp to
-- measure against. updated_at is touched by many operations and unsuitable.
-- This column is set when a job transitions to awaiting_pickup
-- (in handleCompleteJob).
--
-- Existing jobs (none in awaiting_pickup state at C5 deploy time per pilot
-- traffic profile) get NULL; they would effectively be ineligible for
-- no-show flagging until re-created. Acceptable for the migration.

ALTER TABLE jobs ADD COLUMN awaiting_pickup_at timestamptz;

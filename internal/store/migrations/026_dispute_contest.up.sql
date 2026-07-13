-- 026_dispute_contest.up.sql
-- C7 (GOAL 1): consumer contest of a no-show → opens a dispute.
--
-- Two changes, no enum changes (the contest reuses the existing 'failed'
-- job_status and 'open' dispute_status, so the ALTER TYPE … ADD VALUE
-- same-transaction hazard is not triggered):
--
-- 1. jobs.payment_intent_id (nullable) — the anchor the contest INSERT copies
--    into disputes.payment_intent_id, and the future escrow-refund target
--    (GOAL 2, deferred pending the escrow-in design decision). Nullable:
--    no charge is created at submit today, so existing and new rows carry NULL
--    until escrow-in is wired; the contest INSERT tolerates NULL via COALESCE.
--
-- 2. idx_disputes_one_active_per_job — one active (open/under_review) dispute
--    per job. Cheap duplicate-contest guard; the contest handler translates a
--    unique violation (23505) into 409 already_contested. Resolved/escalated
--    disputes are excluded from the predicate, so a job may be contested again
--    only after its prior dispute leaves the active set.

ALTER TABLE jobs ADD COLUMN payment_intent_id TEXT;

CREATE UNIQUE INDEX idx_disputes_one_active_per_job
    ON disputes (job_id)
    WHERE status IN ('open', 'under_review');

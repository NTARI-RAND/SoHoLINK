-- 026_dispute_contest.down.sql

DROP INDEX IF EXISTS idx_disputes_one_active_per_job;

ALTER TABLE jobs DROP COLUMN payment_intent_id;

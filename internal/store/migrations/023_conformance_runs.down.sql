-- 023_conformance_runs.down.sql
-- Reverses 023_conformance_runs.up.sql. conformance_challenges has an ON DELETE
-- CASCADE FK to conformance_runs; drop the child first regardless. No enum types.

DROP TABLE IF EXISTS conformance_challenges;
DROP TABLE IF EXISTS conformance_runs;

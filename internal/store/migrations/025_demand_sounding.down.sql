-- 025_demand_sounding.down.sql
-- Reverses 025_demand_sounding.up.sql. DROP TABLE drops the hypertable and its
-- chunks/indexes. rung_tiers seed rows go with the table.
--
-- The timescaledb EXTENSION is intentionally NOT dropped: it is a shared,
-- cluster-scoped resource that a future hypertable migration (or another DB in
-- the cluster) may depend on, and CREATE EXTENSION IF NOT EXISTS in the up is
-- already idempotent. Dropping it here would be a destructive, non-local action.

DROP TABLE IF EXISTS operator_job_shapes;
DROP TABLE IF EXISTS operator_placement_rejections;
DROP TABLE IF EXISTS operator_capacity_snapshots;
DROP TABLE IF EXISTS rung_tiers;

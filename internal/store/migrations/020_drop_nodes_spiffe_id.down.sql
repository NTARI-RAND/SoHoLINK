-- 020 (down): restore nodes.spiffe_id column structure
--
-- Recreates the column with its original definition from
-- 001_initial_schema.up.sql. The column was always NULL in practice,
-- so no data restoration is possible or needed.

ALTER TABLE nodes ADD COLUMN spiffe_id TEXT UNIQUE;

-- 014_opt_out_and_printers.down.sql
-- Reverses 014_opt_out_and_printers.up.sql.

DROP INDEX IF EXISTS idx_node_printers_enabled;
DROP TABLE IF EXISTS node_printers;

ALTER TABLE nodes
  DROP COLUMN IF EXISTS opt_out_updated_at,
  DROP COLUMN IF EXISTS opt_out_version,
  DROP COLUMN IF EXISTS opt_out_printing,
  DROP COLUMN IF EXISTS opt_out_storage,
  DROP COLUMN IF EXISTS opt_out_compute;

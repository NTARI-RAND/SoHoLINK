-- 014_opt_out_and_printers.up.sql
-- Adds per-node opt-out preferences and per-printer toggles to support
-- B6 (Portal UI for opt-out management) and TODO 10 (orchestrator-side
-- workload filtering in FindMatch).
--
-- Fail-closed defaults: every category and every printer starts
-- disabled. Contributors must explicitly opt in via the portal before
-- their nodes accept work.
--
-- opt_out_version is monotonically incremented on every portal update.
-- The agent reports its currently-applied version on each heartbeat;
-- the orchestrator returns the new opt-out payload in the heartbeat
-- response only when the agent's version is stale.

ALTER TABLE nodes
  ADD COLUMN opt_out_compute      BOOLEAN     NOT NULL DEFAULT FALSE,
  ADD COLUMN opt_out_storage      BOOLEAN     NOT NULL DEFAULT FALSE,
  ADD COLUMN opt_out_printing     BOOLEAN     NOT NULL DEFAULT FALSE,
  ADD COLUMN opt_out_version      INTEGER     NOT NULL DEFAULT 0,
  ADD COLUMN opt_out_updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TABLE node_printers (
  node_id      UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  printer_id   TEXT        NOT NULL,
  printer_name TEXT        NOT NULL,
  enabled      BOOLEAN     NOT NULL DEFAULT FALSE,
  detected_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (node_id, printer_id)
);

-- Speeds up the FindMatch EXISTS check for printing jobs:
-- "does this node have at least one enabled printer?"
CREATE INDEX idx_node_printers_enabled
  ON node_printers (node_id)
  WHERE enabled = TRUE;

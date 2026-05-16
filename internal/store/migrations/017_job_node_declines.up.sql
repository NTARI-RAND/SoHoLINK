-- 017_job_node_declines.up.sql
-- Tracks which nodes have declined a given print job. The reroute worker
-- reads this table to exclude prior decliners when calling FindMatch, preventing
-- the same node from being re-assigned after a decline.
--
-- node_id carries ON DELETE CASCADE (matching node_printers): a deleted node's
-- decline history is useless for re-dispatch (it's not in the registry) and
-- should be cleaned up automatically.
--
-- The composite primary key (job_id, node_id) is also the primary access pattern
-- for the reroute worker: SELECT node_id FROM job_node_declines WHERE job_id = $1.
-- No additional index is needed since job_id is the leading PK column.

CREATE TABLE job_node_declines (
    job_id      UUID        NOT NULL REFERENCES jobs(id)  ON DELETE CASCADE,
    node_id     UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    declined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (job_id, node_id)
);

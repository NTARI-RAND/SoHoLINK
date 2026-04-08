-- Heartbeat event log — append-only, used to compute rolling uptime
CREATE TABLE node_heartbeat_events (
    id         BIGSERIAL PRIMARY KEY,
    node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_heartbeat_node_time
    ON node_heartbeat_events(node_id, recorded_at DESC);

-- Partition hint: rows older than 30 days can be pruned.
-- Uptime computation uses only the past 7 days.

-- Rolling uptime percentage on nodes — updated by background scorer
ALTER TABLE nodes
    ADD COLUMN uptime_pct NUMERIC(5,2) NOT NULL DEFAULT 100.00
        CHECK (uptime_pct BETWEEN 0 AND 100);

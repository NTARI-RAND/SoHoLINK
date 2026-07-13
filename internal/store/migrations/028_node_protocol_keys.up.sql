-- 028_node_protocol_keys.up.sql
-- Ed25519 verification keys for sohocloud-protocol node messages (A1).
-- SPEC §2: the protocol does not distribute public keys — a coordinator
-- resolves a node's verification key out of band. This table is that
-- out-of-band registry: keys are enrolled once via the SPIFFE-bound
-- POST /nodes/pubkey endpoint (first-write-wins; rotation is an operator
-- action, not a self-service overwrite).
--
-- last_listing_seq / last_heartbeat_seq persist the SPEC §5.5 strict
-- per-node Seq monotonicity across coordinator restarts: the adapter accepts
-- a listing/heartbeat only via a guarded UPDATE ... WHERE last_*_seq < $new.
CREATE TABLE node_protocol_keys (
    node_id            UUID        PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    public_key         BYTEA       NOT NULL,
    algo               TEXT        NOT NULL DEFAULT 'ed25519',
    last_listing_seq   BIGINT      NOT NULL DEFAULT 0,
    last_heartbeat_seq BIGINT      NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

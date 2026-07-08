-- 022_operator_replay.up.sql
-- Durable, fail-closed anti-replay state for operator transmissions. The
-- coordinator's OperatorAuth verify path (later step) performs a compare-and-set
-- (SeqCheckAndAdvance + NonceInsert) inside ONE pgx.Tx: if the transaction
-- cannot commit, the transmission is rejected (fail-closed), never accepted.
-- Per SPEC §11.0 these are a coordinator-side obligation layered on top of the
-- protocol's pure Verify.
--
-- Anti-replay is scoped per (operator, coordinator). A per-pair sliding-window
-- Seq bitmap tolerates out-of-order/retried transmissions within the window;
-- the nonce set catches replays inside the timestamp window regardless of Seq.

CREATE TABLE operator_replay (
    operator_id    TEXT   NOT NULL,
    coordinator_id TEXT   NOT NULL,
    seq_high       BIGINT NOT NULL DEFAULT 0,                        -- highest Seq accepted so far
    seq_window     BYTEA  NOT NULL DEFAULT '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea,
                                                                    -- 256-bit (32-byte) sliding-window bitmap
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (operator_id, coordinator_id)
);

CREATE TABLE operator_nonces (                                     -- durable nonce dedupe (single-use)
    nonce       BYTEA PRIMARY KEY,                                  -- >= 16 bytes CSPRNG; single-use
    operator_id TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'production'
        CHECK (scope IN ('production', 'conformance')),             -- domain-separates conformance from live
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Sweep support: expired nonces can be pruned once the timestamp window has
-- passed (they can no longer be replayed within the 5-minute freshness window).
CREATE INDEX idx_operator_nonces_expiry ON operator_nonces(expires_at);
-- Look up an operator's nonces by scope (e.g. conformance-scoped cleanup).
CREATE INDEX idx_operator_nonces_operator_scope ON operator_nonces(operator_id, scope);

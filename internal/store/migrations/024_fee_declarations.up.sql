-- 024_fee_declarations.up.sql
-- Coordinator-authored fee declarations. The :8090 governance admin signs a
-- sohocloud-protocol fees.FeeDeclaration with the coordinator's Ed25519 key
-- (loaded from env/secret, NEVER hardcoded, NEVER on a public handler) and
-- publishes it here. The public GET /fees read serves the CURRENT (latest)
-- signed declaration from this table.
--
-- SPEC §5.3: fees are legible and non-retroactive. A change is a NEW signed
-- declaration with a strictly greater Seq AND a strictly later EffectiveAt than
-- the current one; terms MUST NOT change retroactively for already-offered work.
-- Both invariants are enforced in the publish path (see PublishFeeDeclaration)
-- and by the constraints below: Seq is UNIQUE per coordinator (monotonicity is
-- enforced application-side under a row lock), and the signature bytes are stored
-- so the exact signed artifact can be re-served and independently verified.
--
-- Monetary/share values are basis points (int) per the protocol Terms shape,
-- summing to 10000. No renumbering of 001-023; extends cleanly.

CREATE TABLE fee_declarations (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    coordinator_id         TEXT   NOT NULL,                    -- this coordinator's id (e.g. 'soholink')
    contributor_share_bps  INT    NOT NULL CHECK (contributor_share_bps >= 0),
    platform_fee_bps       INT    NOT NULL CHECK (platform_fee_bps >= 0),
    effective_at           TIMESTAMPTZ NOT NULL,               -- non-retroactive; strictly increases per coordinator
    seq                    BIGINT NOT NULL CHECK (seq >= 0),   -- strictly monotonic per coordinator
    signature              BYTEA  NOT NULL,                    -- ed25519 over the canonical bytes (SPEC §4.6)
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_fee_declarations_coord_seq UNIQUE (coordinator_id, seq)
);

-- The public /fees read wants the latest declaration per coordinator quickly.
CREATE INDEX idx_fee_declarations_current
    ON fee_declarations (coordinator_id, seq DESC);

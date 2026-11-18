-- 030_operator_nodes.up.sql
-- A node may be owned by an OPERATOR (a frontend such as Cloudy) instead of a
-- PARTICIPANT. Under decisions 0001 and 0002 (Cloudy docs/decisions), a
-- prosumer's computer reaches the coordinator through its frontend, and the
-- coordinator holds no person for it: no participant, hostname, country,
-- region, coordinates, or payout account.
--
-- Exactly one owner, enforced here rather than in application code:
--   participant-owned: participant_id, hostname and country_code required (as
--                      before this migration), operator_id NULL;
--   operator-owned:    operator_id required, and every person-derived column
--                      (participant_id, hostname, country_code, region,
--                      latitude, longitude) NULL.
-- Existing rows are all participant-owned and satisfy the constraint as-is.
--
-- ON DELETE RESTRICT: an operator that still owns nodes cannot be deleted out
-- from under them; removing an operator's nodes is a deliberate act.
ALTER TABLE nodes ADD COLUMN operator_id TEXT REFERENCES operators(id) ON DELETE RESTRICT;

ALTER TABLE nodes ALTER COLUMN participant_id DROP NOT NULL;
ALTER TABLE nodes ALTER COLUMN hostname       DROP NOT NULL;
ALTER TABLE nodes ALTER COLUMN country_code   DROP NOT NULL;

ALTER TABLE nodes ADD CONSTRAINT nodes_exactly_one_owner CHECK (
    (participant_id IS NOT NULL AND operator_id IS NULL
        AND hostname IS NOT NULL AND country_code IS NOT NULL)
    OR
    (operator_id IS NOT NULL AND participant_id IS NULL
        AND hostname IS NULL AND country_code IS NULL AND region IS NULL
        AND latitude IS NULL AND longitude IS NULL)
);

CREATE INDEX idx_nodes_operator_id ON nodes(operator_id) WHERE operator_id IS NOT NULL;

-- 020: drop nodes.spiffe_id
--
-- The column was introduced in 001_initial_schema.up.sql with the intent
-- of storing each node's SPIFFE ID. It was never populated by any code
-- path. Pre-TODO-34, three job-route handlers gated SPIFFE ownership
-- checks on a COALESCE(spiffe_id, '') != '' condition that was therefore
-- always false, making the checks entirely inert. TODO 34 commit 2
-- replaced that pattern with deterministic SPIFFE ID construction from
-- jobs.node_id; the column has no remaining consumer.
--
-- See Dev XXVIII Finding 2 in CLAUDE.md.

ALTER TABLE nodes DROP COLUMN spiffe_id;

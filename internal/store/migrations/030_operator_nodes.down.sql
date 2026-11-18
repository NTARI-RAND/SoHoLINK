-- 030_operator_nodes.down.sql
-- Refuses to run while operator-owned nodes exist: restoring NOT NULL on the
-- person columns would fail on them anyway, and silently deleting a frontend's
-- nodes is never an acceptable side effect of a rollback. Remove them
-- deliberately first.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM nodes WHERE operator_id IS NOT NULL) THEN
        RAISE EXCEPTION '030 down: operator-owned nodes exist; remove them deliberately before rolling back';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_nodes_operator_id;
ALTER TABLE nodes DROP CONSTRAINT nodes_exactly_one_owner;
ALTER TABLE nodes DROP COLUMN operator_id;
ALTER TABLE nodes ALTER COLUMN participant_id SET NOT NULL;
ALTER TABLE nodes ALTER COLUMN hostname       SET NOT NULL;
ALTER TABLE nodes ALTER COLUMN country_code   SET NOT NULL;

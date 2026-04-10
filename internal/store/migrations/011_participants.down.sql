-- This migration is destructive and cannot be fully reversed.
-- Restore from backup if rollback is needed.
SELECT 1/0; -- intentional error to prevent accidental rollback

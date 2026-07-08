-- 021_operators.down.sql
-- Reverses 021_operators.up.sql. Drop tables first (they depend on the enum
-- types), then the enum types. Fresh registry — no data to preserve.

DROP TABLE IF EXISTS operator_verifications;
DROP TABLE IF EXISTS operator_keys;
DROP TABLE IF EXISTS operators;

DROP TYPE IF EXISTS operator_key_state;
DROP TYPE IF EXISTS operator_onboarding_state;
DROP TYPE IF EXISTS operator_status;

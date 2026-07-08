-- 024_fee_declarations.down.sql
-- Reverses 024_fee_declarations.up.sql. No enum types; the index is dropped with
-- the table.

DROP TABLE IF EXISTS fee_declarations;

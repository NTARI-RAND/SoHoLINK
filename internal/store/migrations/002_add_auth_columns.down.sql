ALTER TABLE providers
    DROP COLUMN password_hash,
    DROP COLUMN is_staff;

ALTER TABLE consumers
    DROP COLUMN password_hash;

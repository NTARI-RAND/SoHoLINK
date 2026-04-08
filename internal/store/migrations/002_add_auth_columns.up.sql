ALTER TABLE providers
    ADD COLUMN password_hash TEXT,
    ADD COLUMN is_staff      BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE consumers
    ADD COLUMN password_hash TEXT;

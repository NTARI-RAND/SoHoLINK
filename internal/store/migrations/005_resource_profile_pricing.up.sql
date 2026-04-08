ALTER TABLE resource_profiles
    ADD COLUMN price_multiplier NUMERIC(4,3) NOT NULL DEFAULT 1.000
        CHECK (price_multiplier BETWEEN 0.500 AND 2.000);

ALTER TABLE providers
    ADD COLUMN onboarding_complete    BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN isp_tier               TEXT        CHECK (isp_tier IN ('business', 'residential', 'cellular')),
    ADD COLUMN disclosure_accepted_at TIMESTAMPTZ;

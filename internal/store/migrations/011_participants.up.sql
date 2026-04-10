-- 1. Create participants table
CREATE TABLE participants (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                       TEXT NOT NULL,
    password_hash               TEXT,
    display_name                TEXT NOT NULL,
    soho_name                   TEXT,
    is_staff                    BOOLEAN NOT NULL DEFAULT FALSE,
    spiffe_id                   TEXT,
    stripe_account_id           TEXT,
    stripe_customer_id          TEXT,
    stripe_onboarding_complete  BOOLEAN NOT NULL DEFAULT FALSE,
    onboarding_complete         BOOLEAN NOT NULL DEFAULT FALSE,
    isp_tier                    TEXT CHECK (isp_tier IN ('business','residential','cellular')),
    disclosure_accepted_at      TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT participants_email_key UNIQUE (email),
    CONSTRAINT participants_spiffe_id_key UNIQUE (spiffe_id),
    CONSTRAINT participants_stripe_account_id_key UNIQUE (stripe_account_id),
    CONSTRAINT participants_stripe_customer_id_key UNIQUE (stripe_customer_id)
);

-- 2. Migrate providers -> participants
INSERT INTO participants (id, email, password_hash, display_name, is_staff,
    spiffe_id, stripe_account_id, stripe_onboarding_complete,
    onboarding_complete, isp_tier, disclosure_accepted_at,
    created_at, updated_at)
SELECT id, email, password_hash, display_name, is_staff,
    spiffe_id, stripe_account_id, stripe_onboarding_complete,
    onboarding_complete, isp_tier, disclosure_accepted_at,
    created_at, updated_at
FROM providers;

-- 3. Migrate consumers -> participants
INSERT INTO participants (id, email, password_hash, display_name,
    stripe_customer_id, created_at, updated_at)
SELECT id, email, password_hash, display_name,
    stripe_customer_id, created_at, updated_at
FROM consumers
ON CONFLICT (email) DO UPDATE SET
    stripe_customer_id = EXCLUDED.stripe_customer_id;

-- 4. Add participant_id to nodes
ALTER TABLE nodes ADD COLUMN participant_id UUID REFERENCES participants(id) ON DELETE CASCADE;
UPDATE nodes SET participant_id = provider_id;
ALTER TABLE nodes ALTER COLUMN participant_id SET NOT NULL;
DROP INDEX uq_nodes_provider_hostname;
CREATE UNIQUE INDEX uq_nodes_participant_hostname ON nodes(participant_id, hostname);

-- 5. Add participant_id to jobs
ALTER TABLE jobs ADD COLUMN participant_id UUID REFERENCES participants(id);
UPDATE jobs SET participant_id = consumer_id;

-- 6. Update disputes
ALTER TABLE disputes ADD COLUMN participant_id UUID REFERENCES participants(id);
UPDATE disputes SET participant_id = consumer_id;
ALTER TABLE disputes ADD COLUMN arbiter_participant_id UUID REFERENCES participants(id);
UPDATE disputes SET arbiter_participant_id = arbiter_id;

-- 7. Drop old FK columns
ALTER TABLE nodes DROP COLUMN provider_id;
ALTER TABLE jobs DROP COLUMN consumer_id;
ALTER TABLE disputes DROP COLUMN consumer_id;
ALTER TABLE disputes DROP COLUMN arbiter_id;

-- 8. Drop old tables
DROP TABLE providers;
DROP TABLE consumers;

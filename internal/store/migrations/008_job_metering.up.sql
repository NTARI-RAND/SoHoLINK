-- Add duration tracking to jobs
ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS started_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS completed_at  TIMESTAMPTZ;

-- Metering record computed at job completion
CREATE TABLE job_metering (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id                   UUID NOT NULL UNIQUE REFERENCES jobs(id),
    cpu_core_hours           NUMERIC(12,4) NOT NULL DEFAULT 0,
    ram_gb_hours             NUMERIC(12,4) NOT NULL DEFAULT 0,
    storage_gb_months        NUMERIC(12,6) NOT NULL DEFAULT 0,
    consumer_paid_cents      BIGINT NOT NULL DEFAULT 0,
    contributor_earned_cents BIGINT NOT NULL DEFAULT 0,
    platform_fee_cents       BIGINT NOT NULL DEFAULT 0,
    computed_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_job_metering_job ON job_metering(job_id);

DROP TABLE IF EXISTS job_metering;
ALTER TABLE jobs
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS completed_at;

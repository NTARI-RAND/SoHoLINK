-- 027_participant_geo.down.sql
ALTER TABLE participants
    DROP COLUMN IF EXISTS country_code,
    DROP COLUMN IF EXISTS region;

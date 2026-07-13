-- 027_participant_geo.up.sql
-- Requester-side geo for locality-first SOFT scoring (B3). Both columns are
-- nullable and unpopulated until the portal register/profile form collects
-- them (named follow-up); NULL/empty means the scheduler's locality term
-- contributes 0 for that requester. These are NEVER copied into the hard
-- CountryConstraint residency filter — that stays consumer-stated.
ALTER TABLE participants
    ADD COLUMN country_code CHAR(2),
    ADD COLUMN region TEXT;

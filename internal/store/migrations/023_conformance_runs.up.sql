-- 023_conformance_runs.up.sql
-- Conformance harness state. SoHoLINK grades operators against FRESH
-- per-onboarding inputs it generates and computes its own oracle for -- NOT
-- testdata/vectors.json verbatim (those are public: seeds+bytes+sigs, so they'd
-- be replayable). SoHoLINK is always the initiator/grader and NEVER dials
-- operator infrastructure.
--
-- Three suites (see docs/operator-onboarding-design.md §9):
--   A canonical-signing  -- fresh fields, byte-equality vs SoHoLINK's own canon + verify sig
--   B transmission       -- real 2-of-7 over a fresh SoHoLINK nonce/seq via the real verify path
--   C rotation/expiry    -- the 5-mock-key §4.2.2.1 exercise; assert swap-required fires
-- All three green -> conformance_passed_at + conformance_keyset_hash set on the
-- run and (later step) mirrored onto the operator, which auto-activates it.

CREATE TABLE conformance_runs (
    run_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operator_id  TEXT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'passed', 'failed')),
    keyset_hash  BYTEA,                                             -- key-set this run graded against
    results      JSONB NOT NULL DEFAULT '{}'::jsonb,                -- per-suite / per-check verdicts
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at  TIMESTAMPTZ
);
CREATE INDEX idx_conformance_runs_operator ON conformance_runs(operator_id, started_at DESC);

CREATE TABLE conformance_challenges (                              -- one row per issued challenge
    challenge_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id       UUID NOT NULL REFERENCES conformance_runs(run_id) ON DELETE CASCADE,
    suite        TEXT NOT NULL CHECK (suite IN ('A', 'B', 'C')),   -- which conformance suite
    idx          INT  NOT NULL CHECK (idx >= 0),                   -- challenge index within (run, suite)
    nonce        BYTEA NOT NULL,                                   -- fresh CSPRNG >= 16B, single-use
    inputs       JSONB NOT NULL DEFAULT '{}'::jsonb,               -- fresh SoHoLINK-generated fields sent to operator
    expected     JSONB NOT NULL,                                   -- SoHoLINK-computed oracle (never public vectors)
    result       JSONB,                                            -- graded verdict on submit (PASS/FAIL + detail)
    consumed_at  TIMESTAMPTZ,                                      -- set on first response; single-use
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, suite, idx)
);
CREATE INDEX idx_conformance_challenges_run ON conformance_challenges(run_id);

CREATE TYPE dispute_status AS ENUM (
    'open',
    'under_review',
    'resolved',
    'escalated'
);

CREATE TABLE disputes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id              UUID NOT NULL REFERENCES jobs(id),
    consumer_id         UUID NOT NULL REFERENCES consumers(id),
    node_id             UUID NOT NULL REFERENCES nodes(id),
    payment_intent_id   TEXT NOT NULL,
    status              dispute_status NOT NULL DEFAULT 'open',
    reason              TEXT NOT NULL,
    evidence_log        JSONB NOT NULL DEFAULT '[]',
    consumer_refund_pct INTEGER NOT NULL DEFAULT 50
        CHECK (consumer_refund_pct BETWEEN 0 AND 100),
    arbiter_id          UUID REFERENCES providers(id),
    arbiter_notes       TEXT,
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_disputes_status   ON disputes(status);
CREATE INDEX idx_disputes_consumer ON disputes(consumer_id);
CREATE INDEX idx_disputes_job      ON disputes(job_id);

CREATE TABLE node_registration_tokens (
    token                    TEXT        PRIMARY KEY,
    participant_id           UUID        NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at               TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days',
    used_at                  TIMESTAMPTZ,
    node_id                  UUID        REFERENCES nodes(id) ON DELETE SET NULL,
    spire_join_token         TEXT,
    spire_join_token_expires TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_nodes_provider_hostname
    ON nodes(provider_id, hostname);

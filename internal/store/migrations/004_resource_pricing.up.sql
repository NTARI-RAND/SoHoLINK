CREATE TYPE resource_type AS ENUM (
    'cpu_core_hr',
    'ram_gb_hr',
    'gpu_vram_gb_hr',
    'storage_gb_mo',
    'egress_gb'
);

CREATE TABLE resource_pricing (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type       resource_type NOT NULL,
    base_rate           NUMERIC(10,6) NOT NULL CHECK (base_rate >= 0),
    contributor_share   NUMERIC(4,3)  NOT NULL DEFAULT 0.650 CHECK (contributor_share BETWEEN 0 AND 1),
    reliability_floor   NUMERIC(4,3)  NOT NULL DEFAULT 0.900 CHECK (reliability_floor BETWEEN 0 AND 1),
    effective_from      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    effective_until     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_resource_pricing_type_effective
    ON resource_pricing(resource_type, effective_from DESC);

INSERT INTO resource_pricing (resource_type, base_rate, contributor_share) VALUES
    ('cpu_core_hr',    0.025, 0.650),
    ('ram_gb_hr',      0.003, 0.650),
    ('gpu_vram_gb_hr', 0.400, 0.650),
    ('storage_gb_mo',  0.010, 0.650),
    ('egress_gb',      0.040, 0.650);

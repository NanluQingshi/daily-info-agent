-- Runtime-managed RSS sources (#80).
-- Rows here take precedence over the static RSS_FEEDS env list once the
-- table has been seeded; enabled=false keeps the source visible in the
-- management UI and its health history without fetching it.
CREATE TABLE sources (
    id         BIGSERIAL PRIMARY KEY,
    url        TEXT        NOT NULL UNIQUE,
    enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sources_enabled ON sources (enabled) WHERE enabled;

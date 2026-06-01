CREATE TABLE articles (
    id                BIGSERIAL PRIMARY KEY,
    run_id            TEXT             NOT NULL,
    source_url        TEXT             NOT NULL UNIQUE,
    title             TEXT             NOT NULL DEFAULT '',
    description       TEXT             NOT NULL DEFAULT '',
    content           TEXT             NOT NULL DEFAULT '',
    summary           TEXT             NOT NULL DEFAULT '',
    category          TEXT             NOT NULL DEFAULT '',
    source_domain     TEXT             NOT NULL DEFAULT '',
    source_type       TEXT             NOT NULL DEFAULT '',
    credibility_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    tags              TEXT[]           NOT NULL DEFAULT '{}',
    language          TEXT             NOT NULL DEFAULT '',
    detected_language TEXT             NOT NULL DEFAULT '',
    agent_version     TEXT             NOT NULL DEFAULT '',
    verification_pass BOOLEAN          NOT NULL DEFAULT FALSE,
    skip_reason       TEXT             NOT NULL DEFAULT '',
    domain_hit        BOOLEAN          NOT NULL DEFAULT FALSE,
    status            TEXT             NOT NULL DEFAULT 'pending',
    external_id       BIGINT,
    published_at      TIMESTAMPTZ,
    fetched_at        TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    created_at        TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_articles_run_id        ON articles (run_id);
CREATE INDEX idx_articles_category      ON articles (category);
CREATE INDEX idx_articles_status        ON articles (status);
CREATE INDEX idx_articles_created_at    ON articles (created_at DESC);
CREATE INDEX idx_articles_source_domain ON articles (source_domain);

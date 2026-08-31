-- User feedback on AI-generated summaries and categories (#61).
-- One row per (article, kind); upsert semantics make repeat clicks
-- idempotent and rating changes overwrite the previous value.
CREATE TABLE article_feedback (
    id         BIGSERIAL PRIMARY KEY,
    article_id BIGINT      NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind IN ('summary', 'category')),
    rating     SMALLINT    NOT NULL CHECK (rating IN (1, -1)), -- 1 = up, -1 = down
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (article_id, kind)
);

CREATE INDEX idx_article_feedback_kind_rating ON article_feedback (kind, rating);

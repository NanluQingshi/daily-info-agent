-- Revert Chinese-aware FTS back to the stock 'simple' config (#55).
-- The zhparser extension and the `zh` configuration are dropped too:
-- leaving them behind would fail on databases where the extension was
-- never installed.

ALTER TABLE articles DROP COLUMN search_tsv;

ALTER TABLE articles
    ADD COLUMN search_tsv tsvector
        GENERATED ALWAYS AS (
            to_tsvector('simple',
                coalesce(title, '') || ' ' ||
                coalesce(summary, '') || ' ' ||
                left(coalesce(content_text, ''), 100000))
        ) STORED;

CREATE INDEX idx_articles_search_tsv ON articles USING GIN (search_tsv);

DROP TEXT SEARCH CONFIGURATION IF EXISTS zh;

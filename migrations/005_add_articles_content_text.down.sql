-- Reverse #47: drop the generated tsvector first (it references content_text),
-- then the extracted-text column itself, and restore the original FTS
-- expression over title + summary.

ALTER TABLE articles DROP COLUMN search_tsv;

ALTER TABLE articles DROP COLUMN content_text;

ALTER TABLE articles
    ADD COLUMN search_tsv tsvector
        GENERATED ALWAYS AS (
            to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(summary, ''))
        ) STORED;

CREATE INDEX idx_articles_search_tsv ON articles USING GIN (search_tsv);

-- Full-text article extraction (#47): store the extracted original page text
-- alongside the feed summary, and extend the FTS index to cover it.

ALTER TABLE articles
    ADD COLUMN content_text TEXT NOT NULL DEFAULT '';

-- Rebuild the generated tsvector to include the extracted text. PostgreSQL
-- cannot alter a GENERATED column's expression in place, so drop and re-add.
-- Dropping the column automatically drops idx_articles_search_tsv.
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

-- Add a generated tsvector column over title + summary for full-text search,
-- replacing the previous ILIKE '%query%' scan. Backfill existing rows so the
-- column is populated immediately; the GENERATED ALWAYS clause keeps it in
-- sync on future inserts/updates automatically.
ALTER TABLE articles
    ADD COLUMN search_tsv tsvector
        GENERATED ALWAYS AS (
            to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(summary, ''))
        ) STORED;

CREATE INDEX idx_articles_search_tsv ON articles USING GIN (search_tsv);

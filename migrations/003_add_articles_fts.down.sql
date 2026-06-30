DROP INDEX IF EXISTS idx_articles_search_tsv;
ALTER TABLE articles DROP COLUMN IF EXISTS search_tsv;

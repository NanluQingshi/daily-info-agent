DROP INDEX IF EXISTS idx_articles_bookmarked;
ALTER TABLE articles
    DROP COLUMN IF EXISTS bookmarked,
    DROP COLUMN IF EXISTS read_at;

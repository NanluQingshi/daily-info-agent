-- Bookmark & read tracking for articles (#59).
ALTER TABLE articles
    ADD COLUMN bookmarked BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN read_at    TIMESTAMPTZ; -- NULL = unread

-- Partial index: the "bookmarked only" filter scans just starred rows.
CREATE INDEX idx_articles_bookmarked ON articles (created_at DESC) WHERE bookmarked;

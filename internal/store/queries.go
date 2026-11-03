package store

const sqlInsertArticle = `
INSERT INTO articles (
    run_id, source_url, title, description, content, content_text, summary,
    category, source_domain, source_type, credibility_score,
    tags, language, detected_language, agent_version,
    verification_pass, skip_reason, domain_hit, status,
    published_at, fetched_at
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21
) ON CONFLICT (source_url) DO NOTHING`

const sqlGetArticle = `
SELECT id, run_id, source_url, title, description, content, content_text, summary,
       category, source_domain, source_type, credibility_score,
       tags, language, detected_language, agent_version,
       verification_pass, skip_reason, domain_hit, status,
       external_id, published_at, fetched_at, created_at, updated_at,
       bookmarked, read_at
FROM articles
WHERE id = $1`

const sqlDeleteArticle = `DELETE FROM articles WHERE id = $1`

const sqlPruneRunLogs = `DELETE FROM run_logs WHERE started_at < $1`

const sqlPruneArticles = `DELETE FROM articles WHERE created_at < $1`

const sqlMarkPublished = `
UPDATE articles SET status = 'published', external_id = $2, updated_at = NOW()
WHERE id = $1`

const sqlMarkFailed = `
UPDATE articles SET status = 'failed', updated_at = NOW()
WHERE id = $1`

const sqlMarkPending = `
UPDATE articles SET status = 'pending', external_id = NULL, updated_at = NOW()
WHERE id = $1`

// sqlUpdateArticlesTags overwrites the tags of all articles in the given id set.
// Uses a temporary text[] join since pgx supports array parameters directly.
const sqlUpdateArticlesTags = `
UPDATE articles
SET tags = $2::text[], updated_at = NOW()
WHERE id = ANY($1::bigint[])`

// sqlListArticles uses nullable parameters so filters are optional.
// When $5 (the keyword) is non-null, results are ranked by ts_rank against the
// search_tsv tsvector (title + summary + content_text, segmented by the
// zhparser-based `zh` config — see migration 006) using a plainto_tsquery — replacing the
// old ILIKE '%query%' scan. When $5 is null, ordering falls back to created_at.
const sqlListArticles = `
SELECT id, run_id, source_url, title, description, content, content_text, summary,
       category, source_domain, source_type, credibility_score,
       tags, language, detected_language, agent_version,
       verification_pass, skip_reason, domain_hit, status,
       external_id, published_at, fetched_at, created_at, updated_at,
       bookmarked, read_at
FROM articles
WHERE ($1::text        IS NULL OR category   = $1)
  AND ($2::text        IS NULL OR status     = $2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
  AND ($4::timestamptz IS NULL OR created_at <= $4)
  AND ($5::text        IS NULL OR search_tsv @@ plainto_tsquery('zh', $5))
  AND ($8::boolean     IS NULL OR bookmarked = $8)
  AND ($9::boolean     IS NULL OR ($9 AND read_at IS NULL) OR (NOT $9 AND read_at IS NOT NULL))
ORDER BY
  CASE WHEN $5::text IS NULL THEN created_at END DESC,
  CASE WHEN $5::text IS NOT NULL THEN ts_rank(search_tsv, plainto_tsquery('zh', $5)) END DESC,
  created_at DESC
LIMIT $6 OFFSET $7`

const sqlCountArticles = `
SELECT COUNT(*) FROM articles
WHERE ($1::text        IS NULL OR category   = $1)
  AND ($2::text        IS NULL OR status     = $2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
  AND ($4::timestamptz IS NULL OR created_at <= $4)
  AND ($5::text        IS NULL OR search_tsv @@ plainto_tsquery('zh', $5))
  AND ($6::boolean     IS NULL OR bookmarked = $6)
  AND ($7::boolean     IS NULL OR ($7 AND read_at IS NULL) OR (NOT $7 AND read_at IS NOT NULL))`

const sqlSetArticleFlags = `
UPDATE articles SET
    bookmarked = COALESCE($2, bookmarked),
    read_at    = CASE
                   WHEN $3::boolean IS NULL THEN read_at
                   WHEN $3 THEN NOW()
                   ELSE NULL
                 END,
    updated_at = NOW()
WHERE id = $1
RETURNING id, run_id, source_url, title, description, content, content_text, summary,
       category, source_domain, source_type, credibility_score,
       tags, language, detected_language, agent_version,
       verification_pass, skip_reason, domain_hit, status,
       external_id, published_at, fetched_at, created_at, updated_at,
       bookmarked, read_at`

const sqlUpsertArticleFeedback = `
INSERT INTO article_feedback (article_id, kind, rating)
VALUES ($1, $2, $3)
ON CONFLICT (article_id, kind) DO UPDATE SET rating = EXCLUDED.rating
RETURNING id, article_id, kind, rating, created_at`

const sqlGetArticleFeedback = `
SELECT id, article_id, kind, rating, created_at
FROM article_feedback
WHERE article_id = $1
ORDER BY kind`

const sqlFeedbackStats = `
SELECT kind,
       COUNT(*) FILTER (WHERE rating = 1)  AS up,
       COUNT(*) FILTER (WHERE rating = -1) AS down
FROM article_feedback
GROUP BY kind
ORDER BY kind`

const sqlArticlesMissingContent = `
SELECT id, source_url
FROM articles
WHERE (content_text IS NULL OR content_text = '')
  AND source_url <> ''
ORDER BY id
LIMIT $1`

const sqlCountArticlesMissingContent = `
SELECT COUNT(*) FROM articles
WHERE (content_text IS NULL OR content_text = '') AND source_url <> ''`

const sqlUpdateArticleContentText = `
UPDATE articles SET content_text = $2, updated_at = NOW()
WHERE id = $1`

const sqlInsertRunLog = `
INSERT INTO run_logs (
    run_id, total_fetched, total_extracted, total_processed, total_saved,
    total_published, total_skipped, total_failed,
    duration_ms, fatal_error, started_at, finished_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (run_id) DO NOTHING`

const sqlStatsByDay = `
SELECT TO_CHAR(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS date, COUNT(*) AS count
FROM articles
WHERE created_at >= $1
GROUP BY date
ORDER BY date DESC`

const sqlStatsByCategory = `
SELECT category, COUNT(*) AS count
FROM articles
GROUP BY category
ORDER BY count DESC`

const sqlSourceActivity = `
SELECT source_domain, COUNT(*) AS articles, MAX(fetched_at) AS last_fetched_at
FROM articles
WHERE fetched_at >= $1 AND source_domain <> ''
GROUP BY source_domain
ORDER BY articles DESC
`

const sqlRecentRuns = `
SELECT run_id, total_fetched, total_extracted, total_processed, total_saved,
       total_published, total_skipped, total_failed,
       duration_ms, fatal_error, started_at, finished_at
FROM run_logs
ORDER BY started_at DESC
LIMIT 10`

const sqlListRunLogs = `
SELECT run_id, total_fetched, total_extracted, total_processed, total_saved,
       total_published, total_skipped, total_failed,
       duration_ms, fatal_error, started_at, finished_at
FROM run_logs
ORDER BY started_at DESC
LIMIT $1`

const sqlGetRunLog = `
SELECT run_id, total_fetched, total_extracted, total_processed, total_saved,
       total_published, total_skipped, total_failed,
       duration_ms, fatal_error, started_at, finished_at
FROM run_logs
WHERE run_id = $1`

// ── Session persistence ──────────────────────────────────────────────────

const sqlGetSession = `SELECT messages FROM sessions WHERE session_id = $1`

const sqlUpsertSession = `
INSERT INTO sessions (session_id, messages, updated_at)
VALUES ($1, $2::jsonb, NOW())
ON CONFLICT (session_id)
DO UPDATE SET messages = EXCLUDED.messages, updated_at = NOW()`

const sqlDeleteSession = `DELETE FROM sessions WHERE session_id = $1`

const sqlListSources = `
SELECT id, url, enabled, created_at
FROM sources
ORDER BY created_at, id`

const sqlAddSource = `
INSERT INTO sources (url)
VALUES ($1)
ON CONFLICT (url) DO NOTHING
RETURNING id, url, enabled, created_at`

const sqlSetSourceEnabled = `
UPDATE sources SET enabled = $2
WHERE id = $1
RETURNING id, url, enabled, created_at`

const sqlRemoveSource = `DELETE FROM sources WHERE id = $1`

package store

const sqlInsertArticle = `
INSERT INTO articles (
    run_id, source_url, title, description, content, summary,
    category, source_domain, source_type, credibility_score,
    tags, language, detected_language, agent_version,
    verification_pass, skip_reason, domain_hit, status,
    published_at, fetched_at
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
) ON CONFLICT (source_url) DO NOTHING`

const sqlGetArticle = `
SELECT id, run_id, source_url, title, description, content, summary,
       category, source_domain, source_type, credibility_score,
       tags, language, detected_language, agent_version,
       verification_pass, skip_reason, domain_hit, status,
       external_id, published_at, fetched_at, created_at, updated_at
FROM articles
WHERE id = $1`

const sqlDeleteArticle = `DELETE FROM articles WHERE id = $1`

const sqlMarkPublished = `
UPDATE articles SET status = 'published', external_id = $2, updated_at = NOW()
WHERE id = $1`

const sqlMarkFailed = `
UPDATE articles SET status = 'failed', updated_at = NOW()
WHERE id = $1`

const sqlMarkPending = `
UPDATE articles SET status = 'pending', external_id = NULL, updated_at = NOW()
WHERE id = $1`

// sqlListArticles uses nullable parameters so filters are optional.
// $5 is an optional keyword matched case-insensitively against title and summary.
const sqlListArticles = `
SELECT id, run_id, source_url, title, description, content, summary,
       category, source_domain, source_type, credibility_score,
       tags, language, detected_language, agent_version,
       verification_pass, skip_reason, domain_hit, status,
       external_id, published_at, fetched_at, created_at, updated_at
FROM articles
WHERE ($1::text        IS NULL OR category   = $1)
  AND ($2::text        IS NULL OR status     = $2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
  AND ($4::timestamptz IS NULL OR created_at <= $4)
  AND ($5::text        IS NULL OR title   ILIKE '%' || $5 || '%'
                                OR summary ILIKE '%' || $5 || '%')
ORDER BY created_at DESC
LIMIT $6 OFFSET $7`

const sqlCountArticles = `
SELECT COUNT(*) FROM articles
WHERE ($1::text        IS NULL OR category   = $1)
  AND ($2::text        IS NULL OR status     = $2)
  AND ($3::timestamptz IS NULL OR created_at >= $3)
  AND ($4::timestamptz IS NULL OR created_at <= $4)
  AND ($5::text        IS NULL OR title   ILIKE '%' || $5 || '%'
                                OR summary ILIKE '%' || $5 || '%')`

const sqlInsertRunLog = `
INSERT INTO run_logs (
    run_id, total_fetched, total_processed, total_saved,
    total_published, total_skipped, total_failed,
    duration_ms, fatal_error, started_at, finished_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
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

const sqlRecentRuns = `
SELECT run_id, total_fetched, total_processed, total_saved,
       total_published, total_skipped, total_failed,
       duration_ms, fatal_error, started_at, finished_at
FROM run_logs
ORDER BY started_at DESC
LIMIT 10`

const sqlGetRunLog = `
SELECT run_id, total_fetched, total_processed, total_saved,
       total_published, total_skipped, total_failed,
       duration_ms, fatal_error, started_at, finished_at
FROM run_logs
WHERE run_id = $1`

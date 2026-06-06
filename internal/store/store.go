// Package store provides PostgreSQL persistence for articles and run logs.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/daily-info-agent/pkg/models"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("store: record not found")

// ArticleStore is the persistence interface used by the scheduler and API handlers.
type ArticleStore interface {
	SaveArticles(ctx context.Context, articles []models.ProcessedArticle, runID string) (int, error)
	SaveRunLog(ctx context.Context, log models.RunLogRow) error
	ListArticles(ctx context.Context, f models.ArticleFilter) ([]models.ArticleRow, int, error)
	GetArticle(ctx context.Context, id int64) (models.ArticleRow, error)
	DeleteArticle(ctx context.Context, id int64) error
	MarkPublished(ctx context.Context, id int64, externalID int64) error
	MarkFailed(ctx context.Context, id int64) error
	GetStats(ctx context.Context, since time.Time) (models.StatsResult, error)
	Ping(ctx context.Context) error
}

// PostgresStore implements ArticleStore using pgx/v5.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a connection pool and returns a PostgresStore.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{pool: pool}, nil
}

// Close releases the connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// Ping verifies the database connection is alive.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// SaveArticles inserts all articles from a pipeline run using a batch.
// Articles whose source_url already exists are silently ignored.
func (s *PostgresStore) SaveArticles(ctx context.Context, articles []models.ProcessedArticle, runID string) (int, error) {
	if len(articles) == 0 {
		return 0, nil
	}

	batch := &pgx.Batch{}
	for _, a := range articles {
		status := articleStatus(a)
		var pubAt *time.Time
		if !a.Raw.PublishedAt.IsZero() {
			t := a.Raw.PublishedAt
			pubAt = &t
		}
		batch.Queue(sqlInsertArticle,
			runID,
			a.Raw.URL,
			a.Raw.Title,
			a.Raw.Description,
			a.Raw.Content,
			a.Summary,
			string(a.Category),
			a.Raw.SourceDomain,
			string(a.Raw.SourceType),
			a.CredibilityScore,
			a.Tags,
			a.Raw.Language,
			a.DetectedLanguage,
			a.AgentVersion,
			a.Verification.Pass,
			string(a.Verification.SkipReason),
			a.Verification.DomainHit,
			status,
			pubAt,
			a.Raw.FetchedAt,
		)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	var inserted int
	for range articles {
		ct, err := results.Exec()
		if err != nil {
			return inserted, err
		}
		inserted += int(ct.RowsAffected())
	}
	return inserted, nil
}

// articleStatus maps the verification result to a DB status string.
func articleStatus(a models.ProcessedArticle) string {
	if !a.Verification.Pass {
		return "skipped"
	}
	return "pending"
}

// SaveRunLog inserts a run log record.
func (s *PostgresStore) SaveRunLog(ctx context.Context, log models.RunLogRow) error {
	_, err := s.pool.Exec(ctx, sqlInsertRunLog,
		log.RunID,
		log.TotalFetched,
		log.TotalProcessed,
		log.TotalSaved,
		log.TotalPublished,
		log.TotalSkipped,
		log.TotalFailed,
		log.DurationMs,
		log.FatalError,
		log.StartedAt,
		log.FinishedAt,
	)
	return err
}

// ListArticles returns a paginated, filtered list of articles and total count.
func (s *PostgresStore) ListArticles(ctx context.Context, f models.ArticleFilter) ([]models.ArticleRow, int, error) {
	page, pageSize := normalizePagination(f.Page, f.PageSize)
	offset := (page - 1) * pageSize

	var catParam *string
	if f.Category != nil {
		v := string(*f.Category)
		catParam = &v
	}
	var queryParam *string
	if f.Query != "" {
		queryParam = &f.Query
	}

	rows, err := s.pool.Query(ctx, sqlListArticles,
		catParam,
		f.Status,
		f.DateFrom,
		f.DateTo,
		queryParam,
		pageSize,
		offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var articles []models.ArticleRow
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, 0, err
		}
		articles = append(articles, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	err = s.pool.QueryRow(ctx, sqlCountArticles, catParam, f.Status, f.DateFrom, f.DateTo, queryParam).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	return articles, total, nil
}

// GetArticle returns a single article by primary key.
func (s *PostgresStore) GetArticle(ctx context.Context, id int64) (models.ArticleRow, error) {
	rows, err := s.pool.Query(ctx, sqlGetArticle, id)
	if err != nil {
		return models.ArticleRow{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return models.ArticleRow{}, err
		}
		return models.ArticleRow{}, ErrNotFound
	}
	return scanArticle(rows)
}

// DeleteArticle hard-deletes an article by id.
func (s *PostgresStore) DeleteArticle(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, sqlDeleteArticle, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkPublished updates status to 'published' and records the external id.
func (s *PostgresStore) MarkPublished(ctx context.Context, id int64, externalID int64) error {
	_, err := s.pool.Exec(ctx, sqlMarkPublished, id, externalID)
	return err
}

// MarkFailed updates status to 'failed'.
func (s *PostgresStore) MarkFailed(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, sqlMarkFailed, id)
	return err
}

// GetStats returns aggregate stats since the given time.
func (s *PostgresStore) GetStats(ctx context.Context, since time.Time) (models.StatsResult, error) {
	var result models.StatsResult

	// By day
	rows, err := s.pool.Query(ctx, sqlStatsByDay, since)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var d models.DayStat
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return result, err
		}
		result.ByDay = append(result.ByDay, d)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	// By category
	catRows, err := s.pool.Query(ctx, sqlStatsByCategory)
	if err != nil {
		return result, err
	}
	defer catRows.Close()
	for catRows.Next() {
		var c models.CategoryStat
		var cat string
		if err := catRows.Scan(&cat, &c.Count); err != nil {
			return result, err
		}
		c.Category = models.Category(cat)
		result.ByCategory = append(result.ByCategory, c)
	}
	if err := catRows.Err(); err != nil {
		return result, err
	}

	// Recent runs
	runRows, err := s.pool.Query(ctx, sqlRecentRuns)
	if err != nil {
		return result, err
	}
	defer runRows.Close()
	for runRows.Next() {
		var r models.RunLogRow
		if err := runRows.Scan(
			&r.RunID, &r.TotalFetched, &r.TotalProcessed, &r.TotalSaved,
			&r.TotalPublished, &r.TotalSkipped, &r.TotalFailed,
			&r.DurationMs, &r.FatalError, &r.StartedAt, &r.FinishedAt,
		); err != nil {
			return result, err
		}
		result.RecentRuns = append(result.RecentRuns, r)
	}
	if err := runRows.Err(); err != nil {
		return result, err
	}

	return result, nil
}

// scanArticle scans a row from the articles table into an ArticleRow.
func scanArticle(rows pgx.Rows) (models.ArticleRow, error) {
	var a models.ArticleRow
	var skipReason string
	var cat string
	err := rows.Scan(
		&a.ID, &a.RunID, &a.SourceURL, &a.Title, &a.Description, &a.Content,
		&a.Summary, &cat, &a.SourceDomain, &a.SourceType,
		&a.CredibilityScore, &a.Tags, &a.Language, &a.DetectedLanguage,
		&a.AgentVersion, &a.VerificationPass, &skipReason, &a.DomainHit,
		&a.Status, &a.ExternalID, &a.PublishedAt, &a.FetchedAt,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return models.ArticleRow{}, err
	}
	a.Category = models.Category(cat)
	a.SkipReason = models.SkipReason(skipReason)
	return a, nil
}

// normalizePagination returns valid page and pageSize values.
func normalizePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

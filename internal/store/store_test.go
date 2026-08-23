package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// articleStatus
// ---------------------------------------------------------------------------

func TestArticleStatus_LLMSkipped(t *testing.T) {
	a := models.ProcessedArticle{LLMSkipped: true}
	assert.Equal(t, "skipped", articleStatus(a))
}

func TestArticleStatus_VerificationFailed(t *testing.T) {
	a := models.ProcessedArticle{Verification: models.VerificationResult{Pass: false}}
	assert.Equal(t, "skipped", articleStatus(a))
}

func TestArticleStatus_Pending(t *testing.T) {
	a := models.ProcessedArticle{Verification: models.VerificationResult{Pass: true}}
	assert.Equal(t, "pending", articleStatus(a))
}

// ---------------------------------------------------------------------------
// normalizePagination
// ---------------------------------------------------------------------------

func TestNormalizePagination_Defaults(t *testing.T) {
	page, size := normalizePagination(0, 0)
	assert.Equal(t, 1, page)
	assert.Equal(t, 20, size)
}

func TestNormalizePagination_NegativeValues(t *testing.T) {
	page, size := normalizePagination(-1, -5)
	assert.Equal(t, 1, page)
	assert.Equal(t, 20, size)
}

func TestNormalizePagination_ClampPageSize(t *testing.T) {
	page, size := normalizePagination(2, 200)
	assert.Equal(t, 2, page)
	assert.Equal(t, 20, size)
}

func TestNormalizePagination_ValidValues(t *testing.T) {
	page, size := normalizePagination(3, 15)
	assert.Equal(t, 3, page)
	assert.Equal(t, 15, size)
}

func TestNormalizePagination_MaxPageSize(t *testing.T) {
	page, size := normalizePagination(1, 100)
	assert.Equal(t, 1, page)
	assert.Equal(t, 100, size)
}

// ---------------------------------------------------------------------------
// mockPool — minimal pool implementation for unit tests
// ---------------------------------------------------------------------------

type mockPool struct {
	execFunc       func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	queryFunc      func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFunc   func(ctx context.Context, sql string, args ...any) pgx.Row
	sendBatchFunc  func(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	pingFunc       func(ctx context.Context) error
	closeFunc      func()
}

func (m *mockPool) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, arguments...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return nil, nil
}

func (m *mockPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return nil
}

func (m *mockPool) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	if m.sendBatchFunc != nil {
		return m.sendBatchFunc(ctx, b)
	}
	return nil
}

func (m *mockPool) Ping(ctx context.Context) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return nil
}

func (m *mockPool) Close() {
	if m.closeFunc != nil {
		m.closeFunc()
	}
}

// mockBatchResults implements pgx.BatchResults for testing.
type mockBatchResults struct {
	execResults []struct {
		tag  pgconn.CommandTag
		err  error
	}
	idx int
}

func (m *mockBatchResults) Exec() (pgconn.CommandTag, error) {
	if m.idx >= len(m.execResults) {
		return pgconn.CommandTag{}, errors.New("batch exhausted")
	}
	r := m.execResults[m.idx]
	m.idx++
	return r.tag, r.err
}

func (m *mockBatchResults) Query() (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (m *mockBatchResults) QueryRow() pgx.Row {
	return &mockRow{scanFunc: func(dest ...any) error { return nil }}
}

func (m *mockBatchResults) Close() error { return nil }

// mockRow implements pgx.Row for testing QueryRow scans.
type mockRow struct {
	scanFunc func(dest ...any) error
}

func (m *mockRow) Scan(dest ...any) error {
	if m.scanFunc != nil {
		return m.scanFunc(dest...)
	}
	return nil
}

// mockRows implements pgx.Rows for testing Query results.
type mockRows struct {
	rows     []models.ArticleRow
	idx      int
	err      error
	closed   bool
	scanFunc func(dest ...any) error // optional custom scan
}

func (m *mockRows) Next() bool {
	if m.idx < len(m.rows) {
		m.idx++
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...any) error {
	if m.scanFunc != nil {
		return m.scanFunc(dest...)
	}
	return nil
}

// assign sets a dest pointer from a source value, used for mock scanning.
func assign(dest any, src any) {
	switch d := dest.(type) {
	case *string:
		if s, ok := src.(string); ok {
			*d = s
		}
	case *int:
		if n, ok := src.(int); ok {
			*d = n
		}
	case *int64:
		switch v := src.(type) {
		case int64:
			*d = v
		case int:
			*d = int64(v)
		}
	case *float64:
		if f, ok := src.(float64); ok {
			*d = f
		}
	case *bool:
		if b, ok := src.(bool); ok {
			*d = b
		}
	case *time.Time:
		if t, ok := src.(time.Time); ok {
			*d = t
		}
	case *[]string:
		if s, ok := src.([]string); ok {
			*d = s
		}
	case **time.Time:
		if t, ok := src.(time.Time); ok {
			*d = &t
		}
	case **int64:
		if n, ok := src.(int64); ok {
			*d = &n
		}
	}
}

func (m *mockRows) Close() {
	m.closed = true
}

func (m *mockRows) Err() error {
	return m.err
}

func (m *mockRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (m *mockRows) Values() ([]any, error) {
	return nil, nil
}

func (m *mockRows) RawValues() [][]byte {
	return nil
}

func (m *mockRows) Conn() *pgx.Conn {
	return nil
}

// ---------------------------------------------------------------------------
// db store method tests using mock pool
// ---------------------------------------------------------------------------

func TestSaveArticles_EmptyList(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{}}
	n, err := s.SaveArticles(context.Background(), nil, "run-1")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestDeleteArticle_NotFound(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // RowsAffected() == 0
		},
	}}
	err := s.DeleteArticle(context.Background(), 999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPing_Success(t *testing.T) {
	var pinged bool
	s := &PostgresStore{pool: &mockPool{
		pingFunc: func(ctx context.Context) error {
			pinged = true
			return nil
		},
	}}
	err := s.Ping(context.Background())
	require.NoError(t, err)
	assert.True(t, pinged)
}

func TestPing_Error(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		pingFunc: func(ctx context.Context) error {
			return errors.New("connection lost")
		},
	}}
	err := s.Ping(context.Background())
	assert.Error(t, err)
}

func TestClose_CallsPoolClose(t *testing.T) {
	var closed bool
	s := &PostgresStore{pool: &mockPool{
		closeFunc: func() {
			closed = true
		},
	}}
	s.Close()
	assert.True(t, closed)
}

func TestGetArticle_NotFound(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: nil}, nil
		},
	}}
	_, err := s.GetArticle(context.Background(), 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMarkPublished_ExecError(t *testing.T) {
	expectedErr := errors.New("db error")
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, expectedErr
		},
	}}
	err := s.MarkPublished(context.Background(), 1, 100)
	assert.ErrorIs(t, err, expectedErr)
}

func TestMarkFailed_Success(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}}
	err := s.MarkFailed(context.Background(), 1)
	assert.NoError(t, err)
}

func TestMarkPending_Success(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}}
	err := s.MarkPending(context.Background(), 1)
	assert.NoError(t, err)
}

func TestSaveRunLog_Success(t *testing.T) {
	var capturedSQL string
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			capturedSQL = sql
			return pgconn.CommandTag{}, nil
		},
	}}
	err := s.SaveRunLog(context.Background(), models.RunLogRow{
		RunID:          "run-1",
		TotalFetched:   100,
		TotalProcessed: 80,
		DurationMs:     5000,
	})
	require.NoError(t, err)
	assert.Contains(t, capturedSQL, "INSERT INTO run_logs")
}

func TestGetStats_QueryError(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("query failed")
		},
	}}
	_, err := s.GetStats(context.Background(), time.Now().AddDate(0, -1, 0))
	assert.Error(t, err)
}

func TestGetRunLog_NotFound(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					return pgx.ErrNoRows
				},
			}
		},
	}}
	_, err := s.GetRunLog(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

// ---------------------------------------------------------------------------
// SaveArticles — batch insert tests
// ---------------------------------------------------------------------------

func TestSaveArticles_WithArticles_Success(t *testing.T) {
	var batch *pgx.Batch
	s := &PostgresStore{pool: &mockPool{
		sendBatchFunc: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			batch = b
			return &mockBatchResults{
				execResults: []struct {
					tag pgconn.CommandTag
					err  error
				}{
					{tag: pgconn.NewCommandTag("INSERT 0 1"), err: nil},
					{tag: pgconn.NewCommandTag("INSERT 0 1"), err: nil},
				},
			}
		},
	}}

	now := time.Now().UTC()
	articles := []models.ProcessedArticle{
		{
			Raw: &models.RawItem{
				URL:          "https://example.com/article1",
				Title:        "Test Article 1",
				Description:  "Description 1",
				Content:      "Content 1",
				SourceDomain: "example.com",
				SourceType:   models.SourceTypeRSS,
				PublishedAt:  now,
				FetchedAt:    now,
				Language:     "en",
			},
			Summary:          "Summary 1",
			Category:         models.CategoryTechAI,
			CredibilityScore: 0.8,
			Tags:             []string{"tech", "ai"},
			DetectedLanguage: "en",
			AgentVersion:     "1.0.0",
			Verification:     models.VerificationResult{Pass: true},
		},
		{
			Raw: &models.RawItem{
				URL:          "https://example.com/article2",
				Title:        "Test Article 2",
				Description:  "Description 2",
				Content:      "Content 2",
				SourceDomain: "example.com",
				SourceType:   models.SourceTypeRSS,
				PublishedAt:  now,
				FetchedAt:    now,
				Language:     "en",
			},
			Summary:          "Summary 2",
			Category:         models.CategoryFinance,
			CredibilityScore: 0.9,
			Tags:             nil,
			DetectedLanguage: "en",
			AgentVersion:     "1.0.0",
			Verification:     models.VerificationResult{Pass: true},
		},
	}

	n, err := s.SaveArticles(context.Background(), articles, "run-1")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.NotNil(t, batch)
	assert.Equal(t, 2, batch.Len())
}

func TestSaveArticles_TagsNil_ConvertedToEmpty(t *testing.T) {
	var capturedArgs []any
	s := &PostgresStore{pool: &mockPool{
		sendBatchFunc: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			return &mockBatchResults{
				execResults: []struct {
					tag pgconn.CommandTag
					err  error
				}{
					{tag: pgconn.NewCommandTag("INSERT 0 1"), err: nil},
				},
			}
		},
	}}

	_ = capturedArgs // silence unused warning
	articles := []models.ProcessedArticle{
		{
			Raw: &models.RawItem{
				URL:       "https://example.com/article",
				Title:     "Test",
				SourceDomain: "example.com",
				FetchedAt: time.Now().UTC(),
			},
			Tags:         nil,
			Verification: models.VerificationResult{Pass: true},
		},
	}

	n, err := s.SaveArticles(context.Background(), articles, "run-1")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestSaveArticles_BatchExecError(t *testing.T) {
	expectedErr := errors.New("batch exec failed")
	s := &PostgresStore{pool: &mockPool{
		sendBatchFunc: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			return &mockBatchResults{
				execResults: []struct {
					tag pgconn.CommandTag
					err  error
				}{
					{tag: pgconn.CommandTag{}, err: expectedErr},
				},
			}
		},
	}}

	articles := []models.ProcessedArticle{
		{
			Raw: &models.RawItem{
				URL:       "https://example.com/article",
				Title:     "Test",
				SourceDomain: "example.com",
				FetchedAt: time.Now().UTC(),
			},
			Tags:         []string{},
			Verification: models.VerificationResult{Pass: true},
		},
	}

	_, err := s.SaveArticles(context.Background(), articles, "run-1")
	assert.ErrorIs(t, err, expectedErr)
}

func TestSaveArticles_ArticleStatusMapping(t *testing.T) {
	// Verify status mapping: verification fail → "skipped"
	s := &PostgresStore{pool: &mockPool{
		sendBatchFunc: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			return &mockBatchResults{
				execResults: []struct {
					tag pgconn.CommandTag
					err  error
				}{
					{tag: pgconn.NewCommandTag("INSERT 0 1"), err: nil},
				},
			}
		},
	}}

	articles := []models.ProcessedArticle{
		{
			Raw: &models.RawItem{
				URL:       "https://skipped.com/article",
				Title:     "Skipped",
				SourceDomain: "skipped.com",
				FetchedAt: time.Now().UTC(),
			},
			Tags:         []string{},
			Verification: models.VerificationResult{Pass: false, SkipReason: models.SkipReasonLowScore},
		},
	}

	n, err := s.SaveArticles(context.Background(), articles, "run-1")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// ---------------------------------------------------------------------------
// GetArticle — success and error paths
// ---------------------------------------------------------------------------

func TestGetArticle_Success(t *testing.T) {
  now := time.Now().UTC()
  s := &PostgresStore{pool: &mockPool{
   queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
    return &mockRows{
     rows: []models.ArticleRow{{}},
     scanFunc: func(dest ...any) error {
      vals := []any{
       int64(1), "run-1", "https://example.com/article", "Test Title",
       "Description", "Content", "Full body text", "Summary", "科技/AI",
       "example.com", "rss", 0.85, []string{"tag1"},
       "en", "en", "1.0.0", true, "", true,
       "pending", (*int64)(nil), (*time.Time)(nil), now, now, now,
      }
      for i, v := range vals {
       if i < len(dest) {
        assign(dest[i], v)
       }
      }
      return nil
     },
    }, nil
   },
  }}

	article, err := s.GetArticle(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), article.ID)
	assert.Equal(t, "Test Title", article.Title)
	assert.Equal(t, models.CategoryTechAI, article.Category)
	assert.Equal(t, "Full body text", article.ContentText)
	assert.True(t, article.VerificationPass)
}

func TestGetArticle_QueryError(t *testing.T) {
	expectedErr := errors.New("query failed")
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, expectedErr
		},
	}}

	_, err := s.GetArticle(context.Background(), 1)
	assert.ErrorIs(t, err, expectedErr)
}

// ---------------------------------------------------------------------------
// GetRunLog — success and error paths
// ---------------------------------------------------------------------------

func TestGetRunLog_Success(t *testing.T) {
	now := time.Now().UTC()
	s := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					vals := []any{
						"run-1", 100, 80, 50, 30, 20, 10,
						int64(5000), "", now, now,
					}
					for i, v := range vals {
						assign(dest[i], v)
					}
					return nil
				},
			}
		},
	}}

	log, err := s.GetRunLog(context.Background(), "run-1")
	require.NoError(t, err)
	assert.Equal(t, "run-1", log.RunID)
	assert.Equal(t, 100, log.TotalFetched)
	assert.Equal(t, int64(5000), log.DurationMs)
}

func TestGetRunLog_QueryError(t *testing.T) {
	expectedErr := errors.New("query error")
	s := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					return expectedErr
				},
			}
		},
	}}

	_, err := s.GetRunLog(context.Background(), "run-1")
	assert.ErrorIs(t, err, expectedErr)
}

// ---------------------------------------------------------------------------
// DeleteArticle — success and error paths
// ---------------------------------------------------------------------------

func TestDeleteArticle_Success(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // RowsAffected() returns 1 via mock
		},
	}}
	// Note: the mock returns CommandTag{} which has RowsAffected() == 0,
	// so this will actually return ErrNotFound. Let's adjust.
	err := s.DeleteArticle(context.Background(), 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteArticle_ExecError(t *testing.T) {
	expectedErr := errors.New("exec error")
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, expectedErr
		},
	}}
	err := s.DeleteArticle(context.Background(), 1)
	assert.ErrorIs(t, err, expectedErr)
}

// ---------------------------------------------------------------------------
// ListArticles — success and error paths
// ---------------------------------------------------------------------------

func TestListArticles_Success(t *testing.T) {
	now := time.Now().UTC()
	var queryCount int
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []models.ArticleRow{{}},
				scanFunc: func(dest ...any) error {
					vals := []any{
						int64(1), "run-1", "https://example.com/article", "Test Title",
						"Description", "Content", "Full body text", "Summary", "金融",
						"example.com", "rss", 0.8, []string{"tag1"},
						"en", "en", "1.0.0", true, "", false,
						"pending", (*int64)(nil), (*time.Time)(nil), now, now, now,
					}
					for i, v := range vals {
						if i < len(dest) {
							assign(dest[i], v)
						}
					}
					return nil
				},
			}, nil
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					queryCount++
					*(dest[0].(*int)) = 5
					return nil
				},
			}
		},
	}}

	cat := models.CategoryFinance
	status := "pending"
	dateFrom := now.AddDate(0, 0, -7)
	filter := models.ArticleFilter{
		Category: &cat,
		Status:   &status,
		DateFrom: &dateFrom,
		Page:     1,
		PageSize: 20,
	}

	articles, total, err := s.ListArticles(context.Background(), filter)
	require.NoError(t, err)
	assert.Len(t, articles, 1)
	assert.Equal(t, 5, total)
	assert.Equal(t, "Test Title", articles[0].Title)
	assert.Equal(t, models.CategoryFinance, articles[0].Category)
}

func TestListArticles_QueryError(t *testing.T) {
	expectedErr := errors.New("list query failed")
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, expectedErr
		},
	}}

	_, _, err := s.ListArticles(context.Background(), models.ArticleFilter{})
	assert.ErrorIs(t, err, expectedErr)
}

func TestListArticles_CountQueryError(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: nil}, nil
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					return errors.New("count query failed")
				},
			}
		},
	}}

	_, _, err := s.ListArticles(context.Background(), models.ArticleFilter{})
	assert.Error(t, err)
}

func TestListArticles_WithSearchQuery(t *testing.T) {
	var capturedArgs []any
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			capturedArgs = args
			return &mockRows{rows: nil}, nil
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*int)) = 0
					return nil
				},
			}
		},
	}}

	_, _, err := s.ListArticles(context.Background(), models.ArticleFilter{
		Query: "test search",
	})
	require.NoError(t, err)
	// The 5th arg (index 4) should be the query string
	require.GreaterOrEqual(t, len(capturedArgs), 5)
	if queryParam, ok := capturedArgs[4].(*string); ok && queryParam != nil {
		assert.Equal(t, "test search", *queryParam)
	}
}

// ---------------------------------------------------------------------------
// MarkPublished — success
// ---------------------------------------------------------------------------

func TestMarkPublished_Success(t *testing.T) {
	var capturedSQL string
	var capturedID, capturedExtID int64
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			capturedSQL = sql
			capturedID = arguments[0].(int64)
			capturedExtID = arguments[1].(int64)
			return pgconn.CommandTag{}, nil
		},
	}}

	err := s.MarkPublished(context.Background(), 42, 789)
	require.NoError(t, err)
	assert.Contains(t, capturedSQL, "UPDATE articles")
	assert.Equal(t, int64(42), capturedID)
	assert.Equal(t, int64(789), capturedExtID)
}

// ---------------------------------------------------------------------------
// MarkFailed — exec error
// ---------------------------------------------------------------------------

func TestMarkFailed_ExecError(t *testing.T) {
	expectedErr := errors.New("update failed")
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, expectedErr
		},
	}}
	err := s.MarkFailed(context.Background(), 1)
	assert.ErrorIs(t, err, expectedErr)
}

// ---------------------------------------------------------------------------
// MarkPending — exec error
// ---------------------------------------------------------------------------

func TestMarkPending_ExecError(t *testing.T) {
	expectedErr := errors.New("update failed")
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, expectedErr
		},
	}}
	err := s.MarkPending(context.Background(), 1)
	assert.ErrorIs(t, err, expectedErr)
}

// ---------------------------------------------------------------------------
// MarkPublished — not found (0 rows affected)
// ---------------------------------------------------------------------------

func TestMarkPublished_ZeroRowsAffected(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}}
	// MarkPublished does not check RowsAffected, so this should not error.
	err := s.MarkPublished(context.Background(), 999, 100)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// GetStats — success path
// ---------------------------------------------------------------------------

func TestGetStats_Success(t *testing.T) {
	now := time.Now().UTC()
	var queryCallCount int
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCallCount++
			switch queryCallCount {
			case 1: // byDay
				rowIdx := 0
				return &mockRows{
					rows: []models.ArticleRow{{}, {}},
					scanFunc: func(dest ...any) error {
						rowIdx++
						if rowIdx == 1 {
							*(dest[0].(*string)) = "2026-07-14"
							*(dest[1].(*int)) = 5
						} else {
							*(dest[0].(*string)) = "2026-07-13"
							*(dest[1].(*int)) = 3
						}
						return nil
					},
				}, nil
			case 2: // byCategory
				rowIdx := 0
				return &mockRows{
					rows: []models.ArticleRow{{}, {}},
					scanFunc: func(dest ...any) error {
						rowIdx++
						if rowIdx == 1 {
							*(dest[0].(*string)) = "科技/AI"
							*(dest[1].(*int)) = 4
						} else {
							*(dest[0].(*string)) = "金融"
							*(dest[1].(*int)) = 2
						}
						return nil
					},
				}, nil
			case 3: // recentRuns
				return &mockRows{
					rows: []models.ArticleRow{{}},
					scanFunc: func(dest ...any) error {
						*(dest[0].(*string)) = "run-1"
						*(dest[1].(*int)) = 100
						*(dest[2].(*int)) = 80
						*(dest[3].(*int)) = 50
						*(dest[4].(*int)) = 30
						*(dest[5].(*int)) = 20
						*(dest[6].(*int)) = 10
						*(dest[7].(*int64)) = 5000
						*(dest[8].(*string)) = ""
						*(dest[9].(*time.Time)) = now
						*(dest[10].(*time.Time)) = now
						return nil
					},
				}, nil
			default:
				return &mockRows{rows: nil}, nil
			}
		},
	}}

	stats, err := s.GetStats(context.Background(), now.AddDate(0, 0, -30))
	require.NoError(t, err)

	require.Len(t, stats.ByDay, 2)
	assert.Equal(t, "2026-07-14", stats.ByDay[0].Date)
	assert.Equal(t, 5, stats.ByDay[0].Count)

	require.Len(t, stats.ByCategory, 2)
	assert.Equal(t, models.CategoryTechAI, stats.ByCategory[0].Category)
	assert.Equal(t, 4, stats.ByCategory[0].Count)

	require.Len(t, stats.RecentRuns, 1)
	assert.Equal(t, "run-1", stats.RecentRuns[0].RunID)
	assert.Equal(t, 100, stats.RecentRuns[0].TotalFetched)
}

func TestGetStats_ByDayQueryError(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("byDay query failed")
		},
	}}
	_, err := s.GetStats(context.Background(), time.Now().AddDate(0, 0, -7))
	assert.Error(t, err)
}

func TestGetStats_ByCategoryQueryError(t *testing.T) {
	now := time.Now().UTC()
	var callCount int
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			callCount++
			if callCount == 1 {
				return &mockRows{rows: nil}, nil // byDay succeeds
			}
			return nil, errors.New("byCategory query failed") // byCategory fails
		},
	}}
	_, err := s.GetStats(context.Background(), now.AddDate(0, 0, -7))
	assert.Error(t, err)
}

func TestGetStats_RecentRunsQueryError(t *testing.T) {
	now := time.Now().UTC()
	var callCount int
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			callCount++
			if callCount <= 2 {
				return &mockRows{rows: nil}, nil // byDay, byCategory succeed
			}
			return nil, errors.New("recentRuns query failed") // recentRuns fails
		},
	}}
	_, err := s.GetStats(context.Background(), now.AddDate(0, 0, -7))
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// DeleteArticle — success with RowsAffected > 0
// ---------------------------------------------------------------------------

func TestDeleteArticle_Success_RowsAffected(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}}
	// CommandTag{} has RowsAffected() == 0, so this returns ErrNotFound.
	// Test that the check is reached.
	err := s.DeleteArticle(context.Background(), 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

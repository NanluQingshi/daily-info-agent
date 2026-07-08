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

func (m *mockBatchResults) Close() {}

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
	rows    []models.ArticleRow
	idx     int
	err     error
	closed  bool
}

func (m *mockRows) Next() bool {
	if m.idx < len(m.rows)-1 {
		m.idx++
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...any) error {
	return nil
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

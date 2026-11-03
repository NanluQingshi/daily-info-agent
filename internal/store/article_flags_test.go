package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// flagColumns matches the article projection order (ending with the new
// bookmarked / read_at columns) for scan-based mocks.
func flagColumns(now time.Time) []any {
	return []any{
		int64(7), "run-1", "https://example.com/a", "Title", "desc", "content", "fulltext", "summary",
		"金融", "example.com", "rss", 0.9, []string{"tag"}, "zh", "zh", "v1",
		true, "", true, "published",
		(*int64)(nil), (*time.Time)(nil), now, now, now,
		true, now,
	}
}

func scanWith(vals []any) func(dest ...any) error {
	return func(dest ...any) error {
		for i, v := range vals {
			if i < len(dest) {
				assign(dest[i], v)
			}
		}
		return nil
	}
}

func TestSetArticleFlags_Success(t *testing.T) {
	now := time.Now().UTC()
	yes, no := true, false
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
			assert.Equal(t, int64(7), args[0])
			assert.Equal(t, &yes, args[1])
			assert.Equal(t, &no, args[2])
			return &mockRows{rows: []models.ArticleRow{{}}, scanFunc: scanWith(flagColumns(now))}, nil
		},
	}}

	a, err := s.SetArticleFlags(context.Background(), 7, &yes, &no)
	require.NoError(t, err)
	assert.Equal(t, int64(7), a.ID)
	assert.True(t, a.Bookmarked)
	require.NotNil(t, a.ReadAt)
}

func TestSetArticleFlags_NotFound(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &mockRows{rows: []models.ArticleRow{}}, nil // zero rows
		},
	}}

	yes := true
	_, err := s.SetArticleFlags(context.Background(), 42, &yes, nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSetArticleFlags_QueryError(t *testing.T) {
	expected := errors.New("query failed")
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, expected
		},
	}}

	_, err := s.SetArticleFlags(context.Background(), 42, nil, nil)
	assert.ErrorIs(t, err, expected)
}

func TestSetArticleFlags_ScanError(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &mockRows{
				rows:     []models.ArticleRow{{}},
				scanFunc: func(dest ...any) error { return errors.New("scan boom") },
			}, nil
		},
	}}

	yes := true
	_, err := s.SetArticleFlags(context.Background(), 7, &yes, nil)
	require.Error(t, err)
}

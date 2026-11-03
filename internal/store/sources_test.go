package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"github.com/user/daily-info-agent/pkg/models"
)

func TestListSources_MapsRowsAndEmptySlice(t *testing.T) {
	created := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	s := &PostgresStore{pool: &mockPool{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "FROM sources")
			return &mockRows{
				rows: make([]models.ArticleRow, 2), // two rows
				scanFunc: func(dest ...any) error {
					assign(dest[0], int64(1))
					assign(dest[1], "https://a.example/rss")
					assign(dest[2], true)
					assign(dest[3], created)
					return nil
				},
			}, nil
		},
	}}

	out, err := s.ListSources(context.Background())
	assert.NoError(t, err)
	assert.Len(t, out, 2)
	assert.Equal(t, "https://a.example/rss", out[0].URL)
	assert.True(t, out[0].Enabled)
	assert.Equal(t, created, out[0].CreatedAt)

	// empty table -> empty slice (callers treat nil as "use RSS_FEEDS")
	s2 := &PostgresStore{pool: &mockPool{
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}}
	out2, err2 := s2.ListSources(context.Background())
	assert.NoError(t, err2)
	assert.Empty(t, out2)
}

func TestAddSource_ConflictWhenNoRowReturned(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "ON CONFLICT (url) DO NOTHING")
			assert.Equal(t, "https://dup.example/rss", args[0])
			return &mockRow{scanFunc: func(...any) error { return pgx.ErrNoRows }}
		},
	}}

	_, err := s.AddSource(context.Background(), "https://dup.example/rss")
	assert.ErrorIs(t, err, ErrConflict)
}

func TestAddSource_ReturnsStoredRow(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(context.Context, string, ...any) pgx.Row {
			return &mockRow{scanFunc: func(dest ...any) error {
				assign(dest[0], int64(11))
				assign(dest[1], "https://ok.example/rss")
				assign(dest[2], true)
				assign(dest[3], time.Date(2025, 6, 2, 8, 30, 0, 0, time.UTC))
				return nil
			}}
		},
	}}

	row, err := s.AddSource(context.Background(), "https://ok.example/rss")
	assert.NoError(t, err)
	assert.Equal(t, int64(11), row.ID)
	assert.Equal(t, "https://ok.example/rss", row.URL)
	assert.True(t, row.Enabled)
}

func TestSetSourceEnabled_NotFoundAndSuccess(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(context.Context, string, ...any) pgx.Row {
			return &mockRow{scanFunc: func(...any) error { return pgx.ErrNoRows }}
		},
	}}
	_, err := s.SetSourceEnabled(context.Background(), 99, false)
	assert.ErrorIs(t, err, ErrNotFound)

	s2 := &PostgresStore{pool: &mockPool{
		queryRowFunc: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "UPDATE sources SET enabled = $2")
			assert.Equal(t, int64(5), args[0])
			assert.Equal(t, false, args[1])
			return &mockRow{scanFunc: func(dest ...any) error {
				assign(dest[0], int64(5))
				assign(dest[1], "https://x.example/rss")
				assign(dest[2], false)
				assign(dest[3], time.Now().UTC())
				return nil
			}}
		},
	}}
	row, err := s2.SetSourceEnabled(context.Background(), 5, false)
	assert.NoError(t, err)
	assert.False(t, row.Enabled)
}

func TestRemoveSource_NotFoundAndSuccess(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // RowsAffected() == 0
		},
	}}
	assert.ErrorIs(t, s.RemoveSource(context.Background(), 1), ErrNotFound)

	s2 := &PostgresStore{pool: &mockPool{
		execFunc: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "DELETE FROM sources")
			assert.Equal(t, int64(1), args[0])
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}}
	assert.NoError(t, s2.RemoveSource(context.Background(), 1))
}

func TestRemoveSource_DBError(t *testing.T) {
	s := &PostgresStore{pool: &mockPool{
		execFunc: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("connection refused")
		},
	}}
	assert.Error(t, s.RemoveSource(context.Background(), 1))
}

package retention

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/metrics"
)

type pruneCall struct {
	before time.Time
}

type fakeStore struct {
	store.ArticleStore // embedded nil: only the prune methods below are called

	logsCalls    []pruneCall
	articleCalls []pruneCall
	logsRemoved  int64
	artRemoved   int64
	logsErr      error
	artErr       error
}

func (f *fakeStore) PruneRunLogs(_ context.Context, before time.Time) (int64, error) {
	f.logsCalls = append(f.logsCalls, pruneCall{before})
	if f.logsErr != nil {
		return 0, f.logsErr
	}
	return f.logsRemoved, nil
}

func (f *fakeStore) PruneArticles(_ context.Context, before time.Time) (int64, error) {
	f.articleCalls = append(f.articleCalls, pruneCall{before})
	if f.artErr != nil {
		return 0, f.artErr
	}
	return f.artRemoved, nil
}

func newRunner(days int, st *fakeStore) *Runner {
	r := New(days, st, slog.Default())
	r.now = func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) }
	return r
}

func TestRunner_DisabledNeverPrunes(t *testing.T) {
	st := &fakeStore{}
	r := newRunner(0, st) // RETENTION_DAYS=0 → disabled
	assert.False(t, r.Enabled())
	r.Run(context.Background())
	assert.Empty(t, st.logsCalls)
	assert.Empty(t, st.articleCalls)
}

func TestRunner_NilStoreIsNoop(t *testing.T) {
	r := New(30, nil, slog.Default())
	assert.True(t, r.Enabled())
	r.Run(context.Background()) // must not panic
}

func TestRunner_PrunesBothTablesWithCutoff(t *testing.T) {
	metrics.App.RunLogsPruned.Store(0)
	metrics.App.ArticlesPruned.Store(0)

	st := &fakeStore{logsRemoved: 7, artRemoved: 3}
	r := newRunner(30, st)
	r.Run(context.Background())

	want := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) // now − 30d
	require.Len(t, st.logsCalls, 1)
	require.Len(t, st.articleCalls, 1)
	assert.Equal(t, want, st.logsCalls[0].before)
	assert.Equal(t, want, st.articleCalls[0].before)
	assert.Equal(t, int64(7), metrics.App.RunLogsPruned.Load())
	assert.Equal(t, int64(3), metrics.App.ArticlesPruned.Load())
}

func TestRunner_ErrorsDoNotStopOtherTable(t *testing.T) {
	metrics.App.ArticlesPruned.Store(0) // counters are global — isolate
	st := &fakeStore{logsErr: errors.New("boom"), artRemoved: 2}
	r := newRunner(14, st)
	r.Run(context.Background()) // logs fail, articles still pruned

	require.Len(t, st.logsCalls, 1)
	require.Len(t, st.articleCalls, 1)
	assert.Equal(t, int64(2), metrics.App.ArticlesPruned.Load())
}

func TestRunner_ZeroRowsRemovedDoNotBumpMetrics(t *testing.T) {
	metrics.App.RunLogsPruned.Store(0)
	st := &fakeStore{} // removes nothing
	r := newRunner(7, st)
	r.Run(context.Background())
	assert.Equal(t, int64(0), metrics.App.RunLogsPruned.Load())
}

func TestRunner_RunForeverStopsOnCancel(t *testing.T) {
	st := &fakeStore{logsRemoved: 1}
	r := newRunner(7, st)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.RunForever(ctx); close(done) }()
	cancel()
	<-done                           // returns promptly instead of blocking on the 24h ticker
	assert.NotEmpty(t, st.logsCalls) // ran once immediately
}

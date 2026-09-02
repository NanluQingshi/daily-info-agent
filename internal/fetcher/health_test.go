package fetcher

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// failingFetcher always fails — used to trigger health tracking.
type failingFetcher struct{}

func (failingFetcher) Name() string { return "rss" }
func (failingFetcher) Fetch(_ context.Context, _ models.FetchConfig) ([]models.RawItem, error) {
	return nil, errors.New("boom")
}

// healthyFetcher always succeeds.
type healthyFetcher struct{ items []models.RawItem }

func (healthyFetcher) Name() string { return "rss" }
func (h healthyFetcher) Fetch(_ context.Context, _ models.FetchConfig) ([]models.RawItem, error) {
	return h.items, nil
}

func newHealthManager(t *testing.T, f Fetcher) *Manager {
	t.Helper()
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	return NewManager([]Fetcher{f}, nil, nil, cacheFile, slog.Default())
}

func TestManager_Health_SkippedAfterConsecutiveFailures(t *testing.T) {
	m := newHealthManager(t, failingFetcher{})
	cfg := []models.FetchConfig{{Type: models.SourceTypeRSS, URL: "http://bad.example.com/feed"}}

	// Fail enough times to cross the threshold.
	for i := 0; i < maxConsecutiveFailures; i++ {
		_, _ = m.FetchAll(context.Background(), cfg)
	}

	// Source must now be skipped.
	assert.True(t, m.isSkipped("http://bad.example.com/feed"))

	health := m.Health()
	require.Len(t, health, 1)
	assert.Equal(t, "http://bad.example.com/feed", health[0].Source)
	assert.True(t, health[0].Skipped)
	assert.Equal(t, maxConsecutiveFailures, health[0].ConsecutiveFailures)
}

func TestManager_Health_ResetOnSuccess(t *testing.T) {
	m := newHealthManager(t, failingFetcher{})
	cfg := []models.FetchConfig{{Type: models.SourceTypeRSS, URL: "http://flaky.example.com/feed"}}

	// One failure — recorded but not yet skipped.
	_, _ = m.FetchAll(context.Background(), cfg)
	assert.False(t, m.isSkipped("http://flaky.example.com/feed"))

	// Switch to a healthy fetcher (same manager, replace fetcher list).
	m.fetchers = []Fetcher{healthyFetcher{items: []models.RawItem{{URL: "http://x.example.com/1"}}}}
	_, _ = m.FetchAll(context.Background(), cfg)

	// Failure count reset, not skipped.
	assert.False(t, m.isSkipped("http://flaky.example.com/feed"))
	health := m.Health()
	require.Len(t, health, 1)
	assert.Equal(t, 0, health[0].ConsecutiveFailures)
	assert.False(t, health[0].Skipped)
}

// ---------------------------------------------------------------------------
// Extended snapshot fields (totals, timestamps, last error)
// ---------------------------------------------------------------------------

func TestHealthSnapshot_TracksTotalsAndErrors(t *testing.T) {
	m := NewManager(nil, nil, nil, "", slog.Default())

	boom := errors.New("boom: connection reset")
	m.recordFailure("https://a.example/rss", boom)
	m.recordFailure("https://a.example/rss", errors.New("second failure"))
	m.recordSuccess("https://b.example/rss")
	m.recordFailure("https://b.example/rss", boom)

	snaps := m.Health()
	require.Len(t, snaps, 2)

	var a, b HealthSnapshot
	for _, s := range snaps {
		switch s.Source {
		case "https://a.example/rss":
			a = s
		case "https://b.example/rss":
			b = s
		}
	}

	assert.Equal(t, 2, a.ConsecutiveFailures)
	assert.Equal(t, int64(2), a.TotalAttempts)
	assert.Equal(t, int64(2), a.TotalFailures)
	assert.Equal(t, "error", a.LastOutcome)
	assert.Equal(t, "second failure", a.LastError) // most recent error kept
	assert.False(t, a.LastAttemptAt.IsZero())
	assert.True(t, a.LastSuccessAt.IsZero(), "never succeeded")

	assert.Equal(t, 1, b.ConsecutiveFailures)
	assert.Equal(t, int64(2), b.TotalAttempts)
	assert.Equal(t, int64(1), b.TotalFailures)
	assert.False(t, b.LastSuccessAt.IsZero())
}

func TestHealthSnapshot_SuccessResetsButKeepsTotals(t *testing.T) {
	m := NewManager(nil, nil, nil, "", slog.Default())

	m.recordFailure("https://flaky.example/rss", errors.New("x"))
	m.recordFailure("https://flaky.example/rss", errors.New("y"))
	m.recordFailure("https://flaky.example/rss", errors.New("z")) // → skipped
	require.True(t, m.isSkipped("https://flaky.example/rss"))

	m.recordSuccess("https://flaky.example/rss") // recovery re-enables
	require.False(t, m.isSkipped("https://flaky.example/rss"))

	snaps := m.Health()
	require.Len(t, snaps, 1)
	s := snaps[0]
	assert.Equal(t, 0, s.ConsecutiveFailures)
	assert.False(t, s.Skipped)
	assert.Equal(t, int64(4), s.TotalAttempts)
	assert.Equal(t, int64(3), s.TotalFailures)
	assert.Equal(t, "ok", s.LastOutcome)
	assert.Empty(t, s.LastError)
}

func TestHealthSnapshot_SortedBySource(t *testing.T) {
	m := NewManager(nil, nil, nil, "", slog.Default())
	m.recordSuccess("https://z.example/rss")
	m.recordSuccess("https://a.example/rss")
	m.recordSuccess("https://m.example/rss")

	snaps := m.Health()
	require.Len(t, snaps, 3)
	assert.Equal(t, "https://a.example/rss", snaps[0].Source)
	assert.Equal(t, "https://m.example/rss", snaps[1].Source)
	assert.Equal(t, "https://z.example/rss", snaps[2].Source)
}

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

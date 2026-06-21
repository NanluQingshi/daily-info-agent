package fetcher_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Mock Fetcher implementation
// ---------------------------------------------------------------------------

// mockFetcher is a test-double that implements fetcher.Fetcher.
type mockFetcher struct {
	name  string // must match "rss", "newsapi", or "rsshub" for Manager dispatch
	items []models.RawItem
	err   error
	calls int
}

func (m *mockFetcher) Name() string { return m.name }
func (m *mockFetcher) Fetch(_ context.Context, _ models.FetchConfig) ([]models.RawItem, error) {
	m.calls++
	return m.items, m.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestManager(t *testing.T, fetchers []fetcher.Fetcher) *fetcher.Manager {
	t.Helper()
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	return fetcher.NewManager(fetchers, nil, cacheFile, slog.Default())
}

func makeItem(url, domain string, sourceType models.SourceType) models.RawItem {
	return models.RawItem{
		URL:          url,
		SourceDomain: domain,
		SourceType:   sourceType,
		Title:        "Title for " + url,
		Description:  "Description for " + url,
		PublishedAt:  time.Now().UTC(),
		FetchedAt:    time.Now().UTC(),
		Language:     "en",
	}
}

// ---------------------------------------------------------------------------
// Deduplication tests
// ---------------------------------------------------------------------------

func TestManager_FetchAll_SameURLFromTwoFetchers_AppearsOnce(t *testing.T) {
	sharedURL := "http://news.example.com/shared-article"
	itemA := makeItem("http://news.example.com/article-a", "news.example.com", models.SourceTypeRSS)
	itemShared := makeItem(sharedURL, "news.example.com", models.SourceTypeRSS)
	itemC := makeItem("http://news.example.com/article-c", "news.example.com", models.SourceTypeNewsAPI)
	itemSharedDup := makeItem(sharedURL, "news.example.com", models.SourceTypeNewsAPI)

	rssFetcher := &mockFetcher{name: "rss", items: []models.RawItem{itemA, itemShared}}
	newsFetcher := &mockFetcher{name: "newsapi", items: []models.RawItem{itemSharedDup, itemC}}

	mgr := newTestManager(t, []fetcher.Fetcher{rssFetcher, newsFetcher})

	cfgs := []models.FetchConfig{
		{Type: models.SourceTypeRSS},
		{Type: models.SourceTypeNewsAPI},
	}

	items, err := mgr.FetchAll(context.Background(), cfgs)
	require.NoError(t, err)

	// Should have 3 unique items: itemA, itemShared, itemC
	assert.Len(t, items, 3)

	urls := make(map[string]int)
	for _, item := range items {
		urls[item.URL]++
	}
	assert.Equal(t, 1, urls[sharedURL], "shared URL should appear exactly once")
}

func TestManager_FetchAll_SingleFetcher_AllItemsReturned(t *testing.T) {
	items := []models.RawItem{
		makeItem("http://example.com/1", "example.com", models.SourceTypeRSS),
		makeItem("http://example.com/2", "example.com", models.SourceTypeRSS),
		makeItem("http://example.com/3", "example.com", models.SourceTypeRSS),
	}
	rssFetcher := &mockFetcher{name: "rss", items: items}

	mgr := newTestManager(t, []fetcher.Fetcher{rssFetcher})
	cfgs := []models.FetchConfig{{Type: models.SourceTypeRSS}}

	result, err := mgr.FetchAll(context.Background(), cfgs)
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestManager_FetchAll_EmptyConfigs_ReturnsNil(t *testing.T) {
	mgr := newTestManager(t, []fetcher.Fetcher{})

	result, err := mgr.FetchAll(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestManager_FetchAll_OneFetcherErrors_OtherSucceeds(t *testing.T) {
	goodItem := makeItem("http://good.example.com/article", "good.example.com", models.SourceTypeNewsAPI)

	errorFetcher := &mockFetcher{
		name: "rss",
		err:  &fetcher.FetchError{Source: "rss", URL: "http://bad.example.com/feed", Wrapped: errors.New("timeout")},
	}
	successFetcher := &mockFetcher{
		name:  "newsapi",
		items: []models.RawItem{goodItem},
	}

	mgr := newTestManager(t, []fetcher.Fetcher{errorFetcher, successFetcher})

	cfgs := []models.FetchConfig{
		{Type: models.SourceTypeRSS},
		{Type: models.SourceTypeNewsAPI},
	}

	result, err := mgr.FetchAll(context.Background(), cfgs)
	require.NoError(t, err, "partial failure should not return an error")
	require.Len(t, result, 1)
	assert.Equal(t, goodItem.URL, result[0].URL)
}

func TestManager_FetchAll_AllFetchersError_ReturnsError(t *testing.T) {
	fetchErr := &fetcher.FetchError{Source: "rss", URL: "http://bad.example.com/feed", Wrapped: errors.New("network error")}

	rssFetcher := &mockFetcher{name: "rss", err: fetchErr}
	newsFetcher := &mockFetcher{
		name: "newsapi",
		err:  &fetcher.FetchError{Source: "newsapi", URL: "", Wrapped: errors.New("api error")},
	}

	mgr := newTestManager(t, []fetcher.Fetcher{rssFetcher, newsFetcher})

	cfgs := []models.FetchConfig{
		{Type: models.SourceTypeRSS},
		{Type: models.SourceTypeNewsAPI},
	}

	_, err := mgr.FetchAll(context.Background(), cfgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all")
}

func TestManager_FetchAll_NoFetcherForSourceType_ItemsSkipped(t *testing.T) {
	// Register only an RSS fetcher but request RSSHub source type.
	rssFetcher := &mockFetcher{
		name:  "rss",
		items: []models.RawItem{makeItem("http://rss.example.com/1", "rss.example.com", models.SourceTypeRSS)},
	}
	mgr := newTestManager(t, []fetcher.Fetcher{rssFetcher})

	// Config asks for rsshub which has no registered fetcher.
	cfgs := []models.FetchConfig{
		{Type: models.SourceTypeRSSHub, URL: "http://rsshub.example.com/wechat"},
	}

	result, err := mgr.FetchAll(context.Background(), cfgs)
	// No jobs dispatched → no results, but no error either (0 jobs, 0 errors).
	// The manager returns nil when there are no jobs at all.
	require.NoError(t, err)
	assert.Empty(t, result)
}

// ---------------------------------------------------------------------------
// FetchForTopic tests
// ---------------------------------------------------------------------------

func TestManager_FetchForTopic_KeywordsMatchedCaseInsensitive(t *testing.T) {
	items := []models.RawItem{
		makeItem("http://example.com/bitcoin-article", "example.com", models.SourceTypeNewsAPI),
		makeItem("http://example.com/unrelated", "example.com", models.SourceTypeNewsAPI),
	}
	items[0].Title = "Bitcoin reaches all-time high"
	items[1].Title = "Local sports team wins championship"

	newsFetcher := &mockFetcher{name: "newsapi", items: items}
	mgr := newTestManager(t, []fetcher.Fetcher{newsFetcher})

	result, err := mgr.FetchForTopic(context.Background(), []string{"bitcoin"}, 10)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "http://example.com/bitcoin-article", result[0].URL)
}

func TestManager_FetchForTopic_EmptyKeywords_ReturnsError(t *testing.T) {
	mgr := newTestManager(t, []fetcher.Fetcher{})

	_, err := mgr.FetchForTopic(context.Background(), nil, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no keywords")
}

func TestManager_FetchForTopic_MaxItemsRespected(t *testing.T) {
	keyword := "tech"
	items := make([]models.RawItem, 15)
	for i := range items {
		item := makeItem(
			fmt.Sprintf("http://tech.example.com/article-%d", i),
			"tech.example.com",
			models.SourceTypeNewsAPI,
		)
		item.Title = fmt.Sprintf("tech news article %d", i)
		items[i] = item
	}

	newsFetcher := &mockFetcher{name: "newsapi", items: items}
	mgr := newTestManager(t, []fetcher.Fetcher{newsFetcher})

	result, err := mgr.FetchForTopic(context.Background(), []string{keyword}, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), 5)
}

// ---------------------------------------------------------------------------
// SaveCache smoke test
// ---------------------------------------------------------------------------

func TestManager_SaveCache_NoError(t *testing.T) {
	mgr := newTestManager(t, []fetcher.Fetcher{})
	err := mgr.SaveCache()
	require.NoError(t, err)
}

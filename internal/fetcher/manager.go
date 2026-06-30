package fetcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
)

// dedupCache is the persistent URL fingerprint cache stored as JSON.
type dedupCache struct {
	mu      sync.Mutex
	Entries map[string]time.Time `json:"entries"` // url-hash -> first-seen time
	path    string
}

func loadDedupCache(path string) *dedupCache {
	c := &dedupCache{
		Entries: make(map[string]time.Time),
		path:    path,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c // not found or unreadable — start fresh
	}
	_ = json.Unmarshal(data, &c.Entries)
	return c
}

// has reports whether the URL has been seen within the last 7 days.
func (c *dedupCache) has(rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := urlHash(rawURL)
	seen, ok := c.Entries[key]
	if !ok {
		return false
	}
	return time.Since(seen) < 7*24*time.Hour
}

// add records the URL in the cache. Returns true when the URL was new
// (i.e. the cache actually changed and a save is warranted).
func (c *dedupCache) add(rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := urlHash(rawURL)
	if _, ok := c.Entries[key]; ok {
		return false
	}
	c.Entries[key] = time.Now().UTC()
	return true
}

// save persists the cache to disk, pruning entries older than 7 days.
func (c *dedupCache) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	pruned := make(map[string]time.Time, len(c.Entries))
	for k, t := range c.Entries {
		if t.After(cutoff) {
			pruned[k] = t
		}
	}
	c.Entries = pruned

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	data, err := json.MarshalIndent(c.Entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	return os.WriteFile(c.path, data, 0o644)
}

func urlHash(rawURL string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(rawURL))))
	return fmt.Sprintf("%x", h[:8]) // 64-bit prefix is sufficient for dedup
}

// fetchResult is an internal type used to collect goroutine results.
type fetchResult struct {
	items []models.RawItem
	err   error
}

// Manager orchestrates parallel fetching across all configured sources and
// applies URL-based deduplication using a local cache file.
type Manager struct {
	fetchers      []Fetcher
	rssFeeds      []string // RSS feed URLs used by FetchForTopic
	rssHubRoutes  []string // RSSHub route paths used by FetchForTopic
	cache         *dedupCache
	logger        *slog.Logger
}

// NewManager creates a Manager wired with the provided fetchers and cache path.
//   - rssFeeds: full RSS feed URLs fetched and keyword-filtered in topic mode.
//   - rssHubRoutes: RSSHub route paths (e.g. "/wallstreetcn/news/global")
//     appended to the RSSHub base URL by the rssHubFetcher.
//     Pass nil to disable RSSHub in FetchForTopic.
func NewManager(fetchers []Fetcher, rssFeeds []string, rssHubRoutes []string, cacheFile string, logger *slog.Logger) *Manager {
	return &Manager{
		fetchers:     fetchers,
		rssFeeds:     rssFeeds,
		rssHubRoutes: rssHubRoutes,
		cache:        loadDedupCache(cacheFile),
		logger:       logger,
	}
}

// FetchAll fetches from all sources in parallel for the given FetchConfigs.
// Results are deduplicated by URL against both the in-memory cache and the
// persistent cache file.
// All source errors are logged as warnings; the method returns an error only if
// ALL sources fail.
func (m *Manager) FetchAll(ctx context.Context, cfgs []models.FetchConfig) ([]models.RawItem, error) {
	items, err := m.fetchConcurrent(ctx, cfgs, true)
	if err != nil {
		return nil, err
	}
	// Mark fetched URLs so subsequent runs skip them. Skip the disk write
	// when nothing changed — a no-new-items run shouldn't touch the file.
	changed := false
	for _, item := range items {
		if m.cache.add(item.URL) {
			changed = true
		}
	}
	if changed {
		if saveErr := m.cache.save(); saveErr != nil {
			m.logger.Warn("failed to save dedup cache", slog.String("error", saveErr.Error()))
		}
	}
	return items, nil
}

// newsAPIEverythingURL is the NewsAPI endpoint for full keyword search.
// Unlike /top-headlines (which surfaces today's hot stories regardless of
// keywords), /everything searches the full article index by relevance.
const newsAPIEverythingURL = "https://newsapi.org/v2/everything"

// FetchForTopic fetches items relevant to the given keywords across all sources.
//
// - RSS: fetches every feed in m.rssFeeds in parallel, then post-filters by keyword.
// - NewsAPI: queries /v2/everything with the keywords joined by OR for best recall,
//   sorted by relevancy.
// - RSSHub: skipped (requires explicit route configuration).
//
// Unlike FetchAll (used by the scheduler), FetchForTopic does NOT apply the
// deduplication cache — chat-mode queries should always return fresh results
// regardless of what the scheduler has previously seen.
//
// Returns at most maxItems results after keyword filtering.
func (m *Manager) FetchForTopic(ctx context.Context, keywords []string, maxItems int) ([]models.RawItem, error) {
	if len(keywords) == 0 {
		return nil, fmt.Errorf("no keywords provided")
	}

	var cfgs []models.FetchConfig

	for _, f := range m.fetchers {
		switch f.Name() {

		case "rss":
			// A: include every configured RSS feed; keyword filtering happens
			// after fetch via filterByKeywords below.
			for _, feedURL := range m.rssFeeds {
				cfgs = append(cfgs, models.FetchConfig{
					Type: models.SourceTypeRSS,
					URL:  feedURL,
				})
			}

		case "newsapi":
			// B: use /everything for full-text keyword search.
			// Join keywords with OR so any match returns a result.
			cfgs = append(cfgs, models.FetchConfig{
				Type: models.SourceTypeNewsAPI,
				URL:  newsAPIEverythingURL,
				Params: map[string]string{
					"q":        strings.Join(keywords, " OR "),
					"language": "en",
					"pageSize": "20",
					"sortBy":   "relevancy",
				},
			})

		case "rsshub":
			// RSSHub routes are pre-configured; we fetch them all and let
			// filterByKeywords do the relevance filtering locally.
			for _, route := range m.rssHubRoutes {
				cfgs = append(cfgs, models.FetchConfig{
					Type: models.SourceTypeRSSHub,
					URL:  route,
				})
			}
		}
	}

	// fetchRaw runs the same parallel logic as FetchAll but skips dedup so
	// chat-mode queries always get fresh results.
	items, err := m.fetchRaw(ctx, cfgs)
	if err != nil {
		return nil, fmt.Errorf("fetch for topic: %w", err)
	}

	filtered := filterByKeywords(items, keywords)
	if len(filtered) > maxItems {
		filtered = filtered[:maxItems]
	}

	return filtered, nil
}

// fetchRaw is like FetchAll but never reads or writes the dedup cache.
// Used by FetchForTopic so chat queries always see fresh articles.
func (m *Manager) fetchRaw(ctx context.Context, cfgs []models.FetchConfig) ([]models.RawItem, error) {
	return m.fetchConcurrent(ctx, cfgs, false)
}

// fetchConcurrent is the shared implementation behind FetchAll and fetchRaw.
// When useCache is true, the persistent dedup cache is consulted (and items
// seen there are skipped); the caller is responsible for marking newly fetched
// URLs and persisting. When false, only in-run deduplication applies.
func (m *Manager) fetchConcurrent(ctx context.Context, cfgs []models.FetchConfig, useCache bool) ([]models.RawItem, error) {
	if len(cfgs) == 0 {
		return nil, nil
	}

	type job struct {
		fetcher Fetcher
		cfg     models.FetchConfig
	}

	fetcherMap := make(map[models.SourceType]Fetcher)
	for _, f := range m.fetchers {
		switch f.Name() {
		case "rss":
			fetcherMap[models.SourceTypeRSS] = f
		case "newsapi":
			fetcherMap[models.SourceTypeNewsAPI] = f
		case "rsshub":
			fetcherMap[models.SourceTypeRSSHub] = f
		}
	}

	var jobs []job
	for _, cfg := range cfgs {
		f, ok := fetcherMap[cfg.Type]
		if !ok {
			m.logger.Warn("no fetcher registered for source type",
				slog.String("source_type", string(cfg.Type)),
				slog.String("url", cfg.URL),
			)
			continue
		}
		jobs = append(jobs, job{fetcher: f, cfg: cfg})
	}

	results := make(chan fetchResult, len(jobs))
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(jj job) {
			defer wg.Done()
			items, err := jj.fetcher.Fetch(ctx, jj.cfg)
			results <- fetchResult{items: items, err: err}
		}(j)
	}
	wg.Wait()
	close(results)

	seen := make(map[string]struct{}) // in-run dedup
	var allItems []models.RawItem
	errCount := 0
	for r := range results {
		if r.err != nil {
			m.logger.Warn("fetcher error", slog.String("error", r.err.Error()))
			errCount++
			continue
		}
		for _, item := range r.items {
			h := urlHash(item.URL)
			if _, dup := seen[h]; dup {
				continue
			}
			if useCache && m.cache.has(item.URL) {
				m.logger.Debug("dedup skip", slog.String("url", item.URL))
				continue
			}
			seen[h] = struct{}{}
			allItems = append(allItems, item)
		}
	}

	if errCount > 0 && len(allItems) == 0 && errCount == len(jobs) {
		return nil, fmt.Errorf("all %d sources failed", errCount)
	}
	return allItems, nil
}

// filterByKeywords returns items whose title or description contains at least
// one of the given keywords (case-insensitive).
func filterByKeywords(items []models.RawItem, keywords []string) []models.RawItem {
	if len(keywords) == 0 {
		return items
	}

	lowerKW := make([]string, len(keywords))
	for i, kw := range keywords {
		lowerKW[i] = strings.ToLower(kw)
	}

	var matched []models.RawItem
	for _, item := range items {
		text := strings.ToLower(item.Title + " " + item.Description)
		for _, kw := range lowerKW {
			if strings.Contains(text, kw) {
				matched = append(matched, item)
				break
			}
		}
	}
	return matched
}

// SaveCache persists the deduplication cache to disk. Called by scheduler after a run.
func (m *Manager) SaveCache() error {
	return m.cache.save()
}

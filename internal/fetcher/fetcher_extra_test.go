package fetcher

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// FetchError
// ---------------------------------------------------------------------------

func TestFetchError_Error(t *testing.T) {
	err := &FetchError{Source: "rss", URL: "http://example.com/feed", Wrapped: errors.New("timeout")}
	msg := err.Error()
	assert.Contains(t, msg, "rss")
	assert.Contains(t, msg, "example.com")
	assert.Contains(t, msg, "timeout")
}

func TestFetchError_Unwrap(t *testing.T) {
	cause := errors.New("inner error")
	err := &FetchError{Wrapped: cause}
	assert.Equal(t, cause, err.Unwrap())
}

// ---------------------------------------------------------------------------
// newHTTPClient
// ---------------------------------------------------------------------------

func TestNewHTTPClient_DefaultTimeout(t *testing.T) {
	c := newHTTPClient(0)
	assert.NotNil(t, c)
	assert.Equal(t, defaultFetchTimeout, c.Timeout)
}

func TestNewHTTPClient_CustomTimeout(t *testing.T) {
	c := newHTTPClient(5 * time.Second)
	assert.Equal(t, 5*time.Second, c.Timeout)
}

// ---------------------------------------------------------------------------
// extractDomain
// ---------------------------------------------------------------------------

func TestExtractDomain_StandardURL(t *testing.T) {
	assert.Equal(t, "reuters.com", extractDomain("https://www.reuters.com/article/test"))
}

func TestExtractDomain_NoSubdomain(t *testing.T) {
	assert.Equal(t, "bbc.com", extractDomain("https://bbc.com/news"))
}

func TestExtractDomain_StripsWWW(t *testing.T) {
	assert.Equal(t, "example.com", extractDomain("https://www.example.com/path"))
}

func TestExtractDomain_EmptyURL_ReturnsRaw(t *testing.T) {
	assert.Equal(t, "", extractDomain(""))
}

func TestExtractDomain_InvalidURL_ReturnsRaw(t *testing.T) {
	assert.Equal(t, "://bad", extractDomain("://bad"))
}

func TestExtractDomain_MultiLevelSubdomain(t *testing.T) {
	assert.Equal(t, "sub.example.com", extractDomain("https://sub.example.com/page"))
}

// ---------------------------------------------------------------------------
// normalizeLang
// ---------------------------------------------------------------------------

func TestNormalizeLang_Empty(t *testing.T) {
	assert.Equal(t, "en", normalizeLang(""))
}

func TestNormalizeLang_Simple(t *testing.T) {
	assert.Equal(t, "en", normalizeLang("en"))
	assert.Equal(t, "zh", normalizeLang("zh"))
}

func TestNormalizeLang_WithRegion(t *testing.T) {
	assert.Equal(t, "en", normalizeLang("en-US"))
	assert.Equal(t, "zh", normalizeLang("zh-CN"))
}

func TestNormalizeLang_CaseInsensitive(t *testing.T) {
	assert.Equal(t, "en", normalizeLang("EN"))
	assert.Equal(t, "zh", normalizeLang("ZH-CN"))
}

// ---------------------------------------------------------------------------
// truncateAtRuneBoundary
// ---------------------------------------------------------------------------

func TestTruncateAtRuneBoundary_ShortEnough(t *testing.T) {
	s := "hello world"
	assert.Equal(t, s, truncateAtRuneBoundary(s, 100))
}

func TestTruncateAtRuneBoundary_ExactBoundary(t *testing.T) {
	s := "hello"
	assert.Equal(t, s, truncateAtRuneBoundary(s, 5))
}

func TestTruncateAtRuneBoundary_TruncateASCII(t *testing.T) {
	s := "hello world this is a test"
	assert.Equal(t, "hello", truncateAtRuneBoundary(s, 5))
}

func TestTruncateAtRuneBoundary_MultibyteSafe(t *testing.T) {
	// 3 Chinese characters = 9 bytes in UTF-8
	s := "你好世界"
	// Truncate at 6 bytes — should cut after 2 characters (6 bytes), not in the middle
	result := truncateAtRuneBoundary(s, 6)
	assert.Equal(t, "你好", result)
}

func TestTruncateAtRuneBoundary_Empty(t *testing.T) {
	assert.Equal(t, "", truncateAtRuneBoundary("", 10))
}

// ---------------------------------------------------------------------------
// urlHash
// ---------------------------------------------------------------------------

func TestURLHash_Deterministic(t *testing.T) {
	h1 := urlHash("https://example.com/article")
	h2 := urlHash("https://example.com/article")
	assert.Equal(t, h1, h2)
}

func TestURLHash_DifferentURLs_DifferentHashes(t *testing.T) {
	h1 := urlHash("https://example.com/article-a")
	h2 := urlHash("https://example.com/article-b")
	assert.NotEqual(t, h1, h2)
}

func TestURLHash_TrimsAndLowercases(t *testing.T) {
	h1 := urlHash("  HTTPS://EXAMPLE.COM/Article  ")
	h2 := urlHash("https://example.com/article")
	assert.Equal(t, h1, h2)
}

func TestURLHash_Format(t *testing.T) {
	h := urlHash("https://example.com/article")
	// 8 bytes hex = 16 characters
	assert.Len(t, h, 16)
}

// ---------------------------------------------------------------------------
// dedupCache — has / add / save / load
// ---------------------------------------------------------------------------

func TestDedupCache_Has_NewURL_ReturnsFalse(t *testing.T) {
	c := &dedupCache{Entries: make(map[string]time.Time)}
	assert.False(t, c.has("https://example.com/new"))
}

func TestDedupCache_Add_NewURL_ReturnsTrue(t *testing.T) {
	c := &dedupCache{Entries: make(map[string]time.Time)}
	assert.True(t, c.add("https://example.com/new"))
}

func TestDedupCache_Add_Duplicate_ReturnsFalse(t *testing.T) {
	c := &dedupCache{Entries: make(map[string]time.Time)}
	c.add("https://example.com/article")
	assert.False(t, c.add("https://example.com/article"))
}

func TestDedupCache_Has_ExistingURL_ReturnsTrue(t *testing.T) {
	c := &dedupCache{Entries: make(map[string]time.Time)}
	c.add("https://example.com/article")
	// Within 7 days — should return true
	assert.True(t, c.has("https://example.com/article"))
}

func TestDedupCache_Has_ExpiredEntry_ReturnsFalse(t *testing.T) {
	key := urlHash("https://example.com/old")
	c := &dedupCache{
		Entries: map[string]time.Time{
			key: time.Now().UTC().AddDate(0, 0, -8), // 8 days ago — expired
		},
	}
	assert.False(t, c.has("https://example.com/old"))
}

func TestDedupCache_SaveAndLoad_PersistsEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dedup.json")

	// Create cache, add an entry, save
	c := &dedupCache{
		Entries: make(map[string]time.Time),
		path:    path,
	}
	c.add("https://example.com/article")
	err := c.save()
	require.NoError(t, err)

	// File should exist
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load into new cache
	c2 := loadDedupCache(path)
	assert.True(t, c2.has("https://example.com/article"))
}

func TestDedupCache_Save_PrunesExpiredEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dedup.json")

	oldKey := urlHash("https://example.com/old")
	freshKey := urlHash("https://example.com/fresh")

	c := &dedupCache{
		Entries: map[string]time.Time{
			oldKey:   time.Now().UTC().AddDate(0, 0, -10), // expired
			freshKey: time.Now().UTC(),                    // fresh
		},
		path: path,
	}
	require.NoError(t, c.save())

	// Load and verify expired entry is gone
	c2 := loadDedupCache(path)
	assert.False(t, c2.has("https://example.com/old"), "expired entry should be pruned")
	assert.True(t, c2.has("https://example.com/fresh"), "fresh entry should survive")
}

func TestLoadDedupCache_NonexistentFile_ReturnsEmpty(t *testing.T) {
	c := loadDedupCache("/nonexistent/path/dedup.json")
	assert.NotNil(t, c)
	assert.Empty(t, c.Entries)
}

func TestLoadDedupCache_InvalidJSON_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dedup.json")
	os.WriteFile(path, []byte("{invalid json}"), 0o644)

	c := loadDedupCache(path)
	assert.NotNil(t, c)
	// Invalid JSON results in empty entries
	assert.Empty(t, c.Entries)
}

// ---------------------------------------------------------------------------
// filterByKeywords
// ---------------------------------------------------------------------------

func TestFilterByKeywords_MatchesTitle(t *testing.T) {
	items := []models.RawItem{
		{Title: "Bitcoin price reaches new high", URL: "http://example.com/1"},
		{Title: "Local sports news", URL: "http://example.com/2"},
	}
	result := filterByKeywords(items, []string{"bitcoin"})
	require.Len(t, result, 1)
	assert.Equal(t, "http://example.com/1", result[0].URL)
}

func TestFilterByKeywords_MatchesDescription(t *testing.T) {
	items := []models.RawItem{
		{Title: "News today", Description: "Discussion about AI chips", URL: "http://example.com/1"},
		{Title: "Weather report", URL: "http://example.com/2"},
	}
	result := filterByKeywords(items, []string{"ai"})
	require.Len(t, result, 1)
}

func TestFilterByKeywords_MultipleKeywords_RequiresAny(t *testing.T) {
	items := []models.RawItem{
		{Title: "Bitcoin rally continues", URL: "http://example.com/1"},
		{Title: "Ethereum drops sharply", URL: "http://example.com/2"},
	}
	result := filterByKeywords(items, []string{"bitcoin", "ethereum"})
	require.Len(t, result, 2)
}

func TestFilterByKeywords_NoMatch_ReturnsEmpty(t *testing.T) {
	items := []models.RawItem{
		{Title: "Sports update", URL: "http://example.com/1"},
	}
	result := filterByKeywords(items, []string{"cryptocurrency"})
	assert.Empty(t, result)
}

func TestFilterByKeywords_EmptyKeywords_ReturnsAll(t *testing.T) {
	items := []models.RawItem{
		{Title: "Article 1", URL: "http://example.com/1"},
		{Title: "Article 2", URL: "http://example.com/2"},
	}
	result := filterByKeywords(items, nil)
	assert.Len(t, result, 2)
}

func TestFilterByKeywords_CaseInsensitive(t *testing.T) {
	items := []models.RawItem{
		{Title: "Bitcoin Price Today", URL: "http://example.com/1"},
	}
	result := filterByKeywords(items, []string{"bitcoin"})
	assert.Len(t, result, 1)
	result = filterByKeywords(items, []string{"BITCOIN"})
	assert.Len(t, result, 1)
}

// ---------------------------------------------------------------------------
// NewRSSFetcher
// ---------------------------------------------------------------------------

func TestNewRSSFetcher_NilClient_CreatesDefault(t *testing.T) {
	f := NewRSSFetcher(nil)
	assert.NotNil(t, f)
	assert.Equal(t, "rss", f.Name())
}

func TestNewRSSFetcher_WithClient(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	f := NewRSSFetcher(client)
	assert.NotNil(t, f)
}

// ---------------------------------------------------------------------------
// NewNewsAPIFetcher
// ---------------------------------------------------------------------------

func TestNewNewsAPIFetcher_NilClient_CreatesDefault(t *testing.T) {
	f := NewNewsAPIFetcher("test-key", nil)
	assert.NotNil(t, f)
	assert.Equal(t, "newsapi", f.Name())
}

func TestNewNewsAPIFetcher_WithClient(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	f := NewNewsAPIFetcher("test-key", client)
	assert.NotNil(t, f)
}

// ---------------------------------------------------------------------------
// NewRSSHubFetcher
// ---------------------------------------------------------------------------

func TestNewRSSHubFetcher_DefaultBaseURL(t *testing.T) {
	f := NewRSSHubFetcher("https://rsshub.app", nil)
	assert.NotNil(t, f)
	assert.Equal(t, "rsshub", f.Name())
}

func TestNewRSSHubFetcher_CustomBaseURL(t *testing.T) {
	f := NewRSSHubFetcher("https://rsshub.example.com", nil)
	assert.NotNil(t, f)
}

func TestNewRSSHubFetcher_BaseURLTrailingSlashTrimmed(t *testing.T) {
	f := NewRSSHubFetcher("https://rsshub.app/", nil)
	assert.NotNil(t, f)
}

// ---------------------------------------------------------------------------
// SearchFetcher
// ---------------------------------------------------------------------------

func TestSearchFetcher_Name(t *testing.T) {
	f := NewSearchFetcher("https://example.com", nil, slog.Default())
	assert.Equal(t, "search", f.Name())
}

func TestNewSearchFetcher_NilClient_CreatesDefault(t *testing.T) {
	f := NewSearchFetcher("https://example.com", nil, slog.Default())
	assert.NotNil(t, f)
}

// ---------------------------------------------------------------------------
// decodeDuckDuckGoURL
// ---------------------------------------------------------------------------

func TestDecodeDuckDuckGoURL_WithUddgParam_Decodes(t *testing.T) {
	href := "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Farticle&rut=abc123"
	got := decodeDuckDuckGoURL(href)
	assert.Equal(t, "https://example.com/article", got)
}

func TestDecodeDuckDuckGoURL_NoUddgParam_ReturnsAsIs(t *testing.T) {
	// Protocol-relative URL without uddg param falls through to the // prefix handler.
	href := "//duckduckgo.com/l/?rut=abc123"
	got := decodeDuckDuckGoURL(href)
	assert.Equal(t, "https://duckduckgo.com/l/?rut=abc123", got)
}

func TestDecodeDuckDuckGoURL_DirectHttpURL_ReturnsAsIs(t *testing.T) {
	href := "https://example.com/article"
	got := decodeDuckDuckGoURL(href)
	assert.Equal(t, href, got)
}

func TestDecodeDuckDuckGoURL_ProtocolRelativeURL_PrefixesHTTPS(t *testing.T) {
	href := "//example.com/article"
	got := decodeDuckDuckGoURL(href)
	assert.Equal(t, "https://example.com/article", got)
}

func TestDecodeDuckDuckGoURL_RelativePath_ReturnsAsIs(t *testing.T) {
	href := "/relative/path"
	got := decodeDuckDuckGoURL(href)
	assert.Equal(t, href, got)
}

// ---------------------------------------------------------------------------
// detectLanguage
// ---------------------------------------------------------------------------

func TestDetectLanguage_ChineseText_ReturnsZh(t *testing.T) {
	assert.Equal(t, "zh", detectLanguage("这是一段中文文本"))
}

func TestDetectLanguage_EnglishText_ReturnsEn(t *testing.T) {
	assert.Equal(t, "en", detectLanguage("This is an English text"))
}

func TestDetectLanguage_MixedText_ChineseFirst_ReturnsZh(t *testing.T) {
	assert.Equal(t, "zh", detectLanguage("中文 English mixed"))
}

func TestDetectLanguage_EmptyText_ReturnsEn(t *testing.T) {
	assert.Equal(t, "en", detectLanguage(""))
}

// ---------------------------------------------------------------------------
// buildSearchQuery
// ---------------------------------------------------------------------------

func TestBuildSearchQuery_FinanceCategory(t *testing.T) {
	q := buildSearchQuery([]models.Category{models.CategoryFinance})
	assert.Contains(t, q, "finance")
	assert.Contains(t, q, "stock")
}

func TestBuildSearchQuery_TechAICategory(t *testing.T) {
	q := buildSearchQuery([]models.Category{models.CategoryTechAI})
	assert.Contains(t, q, "technology")
	assert.Contains(t, q, "AI")
}

func TestBuildSearchQuery_UnknownCategory(t *testing.T) {
	q := buildSearchQuery([]models.Category{"unknown"})
	assert.Contains(t, q, "latest")
	assert.Contains(t, q, "unknown")
}

func TestBuildSearchQuery_EmptyCategories_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", buildSearchQuery(nil))
}

// ---------------------------------------------------------------------------
// catTargetDomain
// ---------------------------------------------------------------------------

func TestCatTargetDomain_WithDomain_ReturnsDomain(t *testing.T) {
	assert.Equal(t, "example.com", catTargetDomain("科技/AI", "example.com"))
}

func TestCatTargetDomain_TechCategory_NoDomain_ReturnsTechSearch(t *testing.T) {
	assert.Equal(t, "tech-search", catTargetDomain("科技/AI", ""))
}

func TestCatTargetDomain_FinanceCategory_NoDomain_ReturnsFinanceSearch(t *testing.T) {
	assert.Equal(t, "finance-search", catTargetDomain("金融", ""))
}

func TestCatTargetDomain_UnknownCategory_NoDomain_ReturnsWebSearch(t *testing.T) {
	assert.Equal(t, "web-search", catTargetDomain("其他", ""))
}

// ---------------------------------------------------------------------------
// extractSearchDomain
// ---------------------------------------------------------------------------

func TestExtractSearchDomain_StandardURL(t *testing.T) {
	assert.Equal(t, "example.com", extractSearchDomain("https://www.example.com/article"))
}

func TestExtractSearchDomain_Subdomain(t *testing.T) {
	assert.Equal(t, "example.com", extractSearchDomain("https://blog.example.com/article"))
}

func TestExtractSearchDomain_InvalidURL_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", extractSearchDomain("://invalid"))
}

func TestExtractSearchDomain_SingleWord_ReturnsAsIs(t *testing.T) {
	assert.Equal(t, "localhost", extractSearchDomain("http://localhost"))
}

// ---------------------------------------------------------------------------
// truncateText
// ---------------------------------------------------------------------------

func TestTruncateText_ShortEnough_ReturnsFull(t *testing.T) {
	assert.Equal(t, "hello", truncateText("hello", 10))
}

func TestTruncateText_ExactLength_ReturnsFull(t *testing.T) {
	assert.Equal(t, "hello", truncateText("hello", 5))
}

func TestTruncateText_TruncatesWithEllipsis(t *testing.T) {
	assert.Equal(t, "hello...", truncateText("hello world", 5))
}

func TestTruncateText_MultibyteSafe(t *testing.T) {
	assert.Equal(t, "你好...", truncateText("你好世界", 2))
}

func TestTruncateText_Empty_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", truncateText("", 10))
}

// ---------------------------------------------------------------------------
// NewsAPI Fetcher — HTTP mock tests
// ---------------------------------------------------------------------------

func TestNewsAPIFetcher_Fetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.URL.Query().Get("apiKey"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"totalResults": 2,
			"articles": [
				{
					"source": {"id": "test", "name": "Test Source"},
					"title": "Article One",
					"description": "Description one",
					"url": "https://example.com/article1",
					"publishedAt": "2026-07-14T08:00:00Z",
					"content": "Content one"
				},
				{
					"source": {"id": "test", "name": "Test Source"},
					"title": "Article Two",
					"description": "Description two",
					"url": "https://example.com/article2",
					"publishedAt": "2026-07-14T09:00:00Z",
					"content": "Content two"
				}
			]
		}`))
	}))
	defer srv.Close()

	f := NewNewsAPIFetcher("test-key", srv.Client())
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		URL:     srv.URL,
		Params:  map[string]string{"language": "en"},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Article One", items[0].Title)
	assert.Equal(t, "Article Two", items[1].Title)
	assert.Equal(t, "example.com", items[0].SourceDomain)
}

func TestNewsAPIFetcher_Fetch_APIError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "error", "code": "apiKeyInvalid", "message": "Your API key is invalid"}`))
	}))
	defer srv.Close()

	f := NewNewsAPIFetcher("bad-key", srv.Client())
	_, err := f.Fetch(context.Background(), models.FetchConfig{
		URL:     srv.URL,
		Timeout: 5 * time.Second,
	})
	require.Error(t, err)
	var fetchErr *FetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Contains(t, err.Error(), "apiKeyInvalid")
}

func TestNewsAPIFetcher_Fetch_RemovedTitleSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"totalResults": 2,
			"articles": [
				{
					"source": {"id": "test", "name": "Test"},
					"title": "[Removed]",
					"description": "Should be skipped",
					"url": "https://example.com/removed",
					"publishedAt": "2026-07-14T08:00:00Z",
					"content": "removed"
				},
				{
					"source": {"id": "test", "name": "Test"},
					"title": "Valid Article",
					"description": "Valid",
					"url": "https://example.com/valid",
					"publishedAt": "2026-07-14T09:00:00Z",
					"content": "valid content"
				}
			]
		}`))
	}))
	defer srv.Close()

	f := NewNewsAPIFetcher("test-key", srv.Client())
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		URL:     srv.URL,
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Valid Article", items[0].Title)
}

func TestNewsAPIFetcher_Fetch_EmptyTitleOrURL_Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"totalResults": 3,
			"articles": [
				{"source": {"id": "test"}, "title": "", "url": "", "publishedAt": "2026-07-14T08:00:00Z"},
				{"source": {"id": "test"}, "title": "Valid", "description": "desc", "url": "https://example.com/valid", "publishedAt": "2026-07-14T09:00:00Z"},
				{"source": {"id": "test"}, "title": "No URL", "description": "desc", "url": "", "publishedAt": "2026-07-14T10:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()

	f := NewNewsAPIFetcher("test-key", srv.Client())
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		URL:     srv.URL,
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Valid", items[0].Title)
}

func TestNewsAPIFetcher_Fetch_HTTPError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewNewsAPIFetcher("test-key", srv.Client())
	_, err := f.Fetch(context.Background(), models.FetchConfig{
		URL:     srv.URL,
		Timeout: 5 * time.Second,
	})
	require.Error(t, err)
}

func TestNewsAPIFetcher_DefaultParams(t *testing.T) {
	// Verify that default language and pageSize are set when not provided.
	var capturedParams string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedParams = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok", "totalResults": 0, "articles": []}`))
	}))
	defer srv.Close()

	f := NewNewsAPIFetcher("test-key", srv.Client())
	_, err := f.Fetch(context.Background(), models.FetchConfig{
		URL:     srv.URL,
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Contains(t, capturedParams, "language=en")
	assert.Contains(t, capturedParams, "pageSize=20")
	assert.Contains(t, capturedParams, "apiKey=test-key")
}

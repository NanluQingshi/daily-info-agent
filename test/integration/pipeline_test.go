//go:build integration

// Package integration contains end-to-end tests for the daily-info-agent pipeline.
// These tests spin up real httptest servers for every external dependency and
// exercise the full flow from Scheduler.Run to the website API.
//
// Run with:
//
//	go test -tags integration ./test/integration/...
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/scheduler"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Package-level test infrastructure
// ---------------------------------------------------------------------------

var (
	// rssSrv serves a minimal RSS feed containing two articles.
	rssSrv *httptest.Server
	// deepSeekSrv serves an OpenAI-compatible chat completions endpoint.
	deepSeekSrv *httptest.Server
	// websiteSrv simulates the Java website API (POST /api/agent/articles).
	websiteSrv *httptest.Server

	// websitePublishCount tracks how many publish calls the website API received.
	websitePublishCount atomic.Int32
	// websiteCaptured holds all PublishRequest bodies received by the website API.
	websiteCaptured []models.PublishRequest

	// testCacheDir is a temp directory for the dedup cache.
	testCacheDir string
)

// ---------------------------------------------------------------------------
// TestMain: wire up all mock servers once for the whole package.
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	// --- RSS mock server ---
	rssSrv = httptest.NewServer(rssHandler())

	// --- DeepSeek (AI) mock server ---
	deepSeekSrv = httptest.NewServer(deepSeekHandler())

	// --- Website API mock server ---
	websiteSrv = httptest.NewServer(websiteAPIHandler())

	// --- Temp cache directory ---
	var err error
	testCacheDir, err = os.MkdirTemp("", "daily-info-agent-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// --- Teardown ---
	rssSrv.Close()
	deepSeekSrv.Close()
	websiteSrv.Close()
	_ = os.RemoveAll(testCacheDir)

	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Mock server handlers
// ---------------------------------------------------------------------------

const rssTestFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Integration Test News</title>
    <link>http://integration-test.example.com</link>
    <description>Integration test RSS feed</description>
    <language>en</language>
    <item>
      <title>Central Bank Raises Interest Rates</title>
      <link>http://reuters.com/finance/interest-rates-2024</link>
      <description>The central bank raised interest rates by 0.25 basis points.</description>
      <pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate>
    </item>
    <item>
      <title>AI Startup Raises $500M Series C</title>
      <link>http://theverge.com/ai/startup-series-c-2024</link>
      <description>An AI startup announced a $500 million Series C round.</description>
      <pubDate>Tue, 02 Jan 2024 11:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`

func rssHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rssTestFeed)
	})
}

func deepSeekHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// Return a realistic batch response matching the RSS feed items above.
		content := `[
			{
				"url": "http://reuters.com/finance/interest-rates-2024",
				"category": "金融",
				"summary": "中央银行将利率上调25个基点，以应对通货膨胀压力。",
				"credibility_score": 0.95,
				"tags": ["finance", "interest rates", "central bank"],
				"language": "en"
			},
			{
				"url": "http://theverge.com/ai/startup-series-c-2024",
				"category": "科技/AI",
				"summary": "一家人工智能初创公司宣布完成5亿美元C轮融资，估值创新高。",
				"credibility_score": 0.88,
				"tags": ["AI", "startup", "venture capital", "technology"],
				"language": "en"
			}
		]`

		resp := map[string]interface{}{
			"id":      "chatcmpl-integration-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "deepseek-chat",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens": 200, "completion_tokens": 120, "total_tokens": 320,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	return mux
}

func websiteAPIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/agent/articles" {
			http.NotFound(w, r)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req models.PublishRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		id := websitePublishCount.Add(1)
		websiteCaptured = append(websiteCaptured, req)

		resp := models.PublishResponse{
			ID:        int64(id),
			SourceURL: req.SourceURL,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Status:    "published",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildIntegrationScheduler creates a Scheduler wired to all mock servers.
// The trustedDomains list controls which articles pass verification.
func buildIntegrationScheduler(trustedDomains []string) *scheduler.Scheduler {
	aiClient := processor.NewDeepSeekClient("test-api-key", deepSeekSrv.URL)
	proc := processor.New(aiClient, "deepseek-chat", slog.Default())

	rssFetcher := fetcher.NewRSSFetcher(rssSrv.Client())
	cacheFile := filepath.Join(testCacheDir, fmt.Sprintf("dedup-%d.json", time.Now().UnixNano()))
	mgr := fetcher.NewManager(
		[]fetcher.Fetcher{rssFetcher},
		nil,
		nil,
		cacheFile,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	ver := verifier.New(trustedDomains, false, slog.Default())

	pub := publisher.New(websiteSrv.URL, "integration-test-token", websiteSrv.Client(), slog.Default())

	cfg := &config.Config{
		RSSFeeds:          []string{rssSrv.URL},
		DefaultCategories: models.AllCategories,
		SkipVerification:  false,
	}

	return scheduler.New(mgr, proc, ver, pub, cfg, slog.Default())
}

// resetWebsiteCounters resets the website publish tracking state.
func resetWebsiteCounters() {
	websitePublishCount.Store(0)
	websiteCaptured = nil
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestIntegration_FullPipeline_SchedulerPublishesExpectedArticles(t *testing.T) {
	resetWebsiteCounters()

	// Trust both domains so both articles pass verification.
	sched := buildIntegrationScheduler([]string{"reuters.com", "theverge.com"})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := sched.Run(ctx)

	// ---- Assertions on RunResult ----
	require.NoError(t, result.FatalError, "pipeline should complete without fatal error")
	assert.Equal(t, 2, result.TotalFetched, "should fetch both RSS items")
	assert.Equal(t, 2, result.TotalProcessed, "both items should be processed")
	assert.Equal(t, 2, result.TotalPublished, "both trusted-domain articles should be published")
	assert.Equal(t, 0, result.TotalSkipped, "no articles should be skipped")
	assert.Greater(t, result.DurationMs, int64(0))
	assert.NotEmpty(t, result.RunID)

	// ---- Assertions on website API calls ----
	assert.Equal(t, int32(2), websitePublishCount.Load(),
		"website API should receive exactly 2 POST requests")

	// Verify the published payloads.
	publishedURLs := make(map[string]models.PublishRequest)
	for _, req := range websiteCaptured {
		publishedURLs[req.SourceURL] = req
	}

	reutersReq, ok := publishedURLs["http://reuters.com/finance/interest-rates-2024"]
	require.True(t, ok, "reuters article should be published")
	assert.Equal(t, "金融", reutersReq.Category)
	assert.NotEmpty(t, reutersReq.Summary)
	assert.InDelta(t, 0.95, reutersReq.CredibilityScore, 0.001)
	// SourceDomain is extracted from the RSS feed URL (the httptest server address),
	// not from the article link — so we only check it is non-empty.
	assert.NotEmpty(t, reutersReq.SourceDomain)
	assert.NotEmpty(t, reutersReq.RunID)

	vergeReq, ok := publishedURLs["http://theverge.com/ai/startup-series-c-2024"]
	require.True(t, ok, "theverge article should be published")
	assert.Equal(t, "科技/AI", vergeReq.Category)
	assert.NotEmpty(t, vergeReq.Summary)
}

func TestIntegration_FullPipeline_UntrustedDomains_NotPublished(t *testing.T) {
	resetWebsiteCounters()

	// Empty trusted domains list — both articles will fail unless score >= 0.7.
	// Both articles have score >= 0.7 (0.95 and 0.88), so they still pass on score.
	sched := buildIntegrationScheduler([]string{})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := sched.Run(ctx)

	require.NoError(t, result.FatalError)
	// Both items have credibility_score >= 0.7 → they pass on AI score.
	assert.Equal(t, 2, result.TotalPublished,
		"items with score >= 0.7 should publish even without domain whitelist")
}

func TestIntegration_FullPipeline_BothDomainsUntrustedLowScore_NothingPublished(t *testing.T) {
	resetWebsiteCounters()

	// Override DeepSeek to return low scores.
	lowScoreDSSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		content := `[
			{"url":"http://reuters.com/finance/interest-rates-2024","category":"金融","summary":"低分摘要","credibility_score":0.2,"tags":[],"language":"en"},
			{"url":"http://theverge.com/ai/startup-series-c-2024","category":"科技/AI","summary":"低分摘要","credibility_score":0.1,"tags":[],"language":"en"}
		]`
		resp := map[string]interface{}{
			"id": "low-score", "object": "chat.completion",
			"created": time.Now().Unix(), "model": "deepseek-chat",
			"choices": []map[string]interface{}{
				{"index": 0, "message": map[string]interface{}{
					"role": "assistant", "content": content,
				}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer lowScoreDSSrv.Close()

	aiClient := processor.NewDeepSeekClient("test-key", lowScoreDSSrv.URL)
	proc := processor.New(aiClient, "deepseek-chat", slog.Default())

	rssFetcher := fetcher.NewRSSFetcher(rssSrv.Client())
	cacheFile := filepath.Join(testCacheDir, fmt.Sprintf("dedup-lowscore-%d.json", time.Now().UnixNano()))
	mgr := fetcher.NewManager(
		[]fetcher.Fetcher{rssFetcher},
		nil,
		nil,
		cacheFile,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	// Empty trusted domains + low scores → all articles skipped.
	ver := verifier.New([]string{}, false, slog.Default())
	pub := publisher.New(websiteSrv.URL, "test-token", websiteSrv.Client(), slog.Default())
	cfg := &config.Config{
		RSSFeeds:          []string{rssSrv.URL},
		DefaultCategories: []models.Category{models.CategoryFinance},
	}

	sched := scheduler.New(mgr, proc, ver, pub, cfg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := sched.Run(ctx)

	require.NoError(t, result.FatalError)
	assert.Equal(t, 0, result.TotalPublished, "low-score untrusted articles should not be published")
	assert.Greater(t, result.TotalSkipped, 0, "articles should be counted as skipped")
	assert.Equal(t, int32(0), websitePublishCount.Load(),
		"website API should receive no publish calls")
}

func TestIntegration_FullPipeline_PublishedArticle_HasCorrectJSONFields(t *testing.T) {
	resetWebsiteCounters()

	sched := buildIntegrationScheduler([]string{"reuters.com", "theverge.com"})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := sched.Run(ctx)
	require.NoError(t, result.FatalError)
	require.Equal(t, 2, result.TotalPublished)
	require.Len(t, websiteCaptured, 2)

	for _, req := range websiteCaptured {
		// All required JSON fields must be present and non-empty.
		assert.NotEmpty(t, req.SourceURL, "source_url must not be empty")
		assert.NotEmpty(t, req.Title, "title must not be empty")
		assert.NotEmpty(t, req.Summary, "summary must not be empty")
		assert.NotEmpty(t, req.Category, "category must not be empty")
		assert.NotEmpty(t, req.SourceDomain, "source_domain must not be empty")
		assert.Greater(t, req.CredibilityScore, 0.0, "credibility_score must be > 0")
		assert.NotEmpty(t, req.PublishedAt, "published_at must not be empty")
		assert.NotEmpty(t, req.FetchedAt, "fetched_at must not be empty")
		assert.NotEmpty(t, req.RunID, "run_id must not be empty")

		// Timestamps should be valid RFC3339.
		_, err := time.Parse(time.RFC3339, req.PublishedAt)
		assert.NoError(t, err, "published_at should be valid RFC3339")
		_, err = time.Parse(time.RFC3339, req.FetchedAt)
		assert.NoError(t, err, "fetched_at should be valid RFC3339")
	}
}

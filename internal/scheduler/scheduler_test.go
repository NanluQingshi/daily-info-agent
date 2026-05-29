package scheduler_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
// Mock fetcher
// ---------------------------------------------------------------------------

type mockFetcher struct {
	name  string
	items []models.RawItem
	err   error
}

func (m *mockFetcher) Name() string { return m.name }
func (m *mockFetcher) Fetch(_ context.Context, _ models.FetchConfig) ([]models.RawItem, error) {
	return m.items, m.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// chatCompletionEnvelope wraps AI content into an OpenAI-compatible response.
func chatCompletionEnvelope(content string) []byte {
	resp := map[string]interface{}{
		"id": "chatcmpl-sched-test", "object": "chat.completion",
		"created": time.Now().Unix(), "model": "deepseek-chat",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role": "assistant", "content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80,
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

// publishSuccessHandler records publish calls and always returns 201.
func publishSuccessHandler(callCount *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agent/articles" {
			callCount.Add(1)
			resp := models.PublishResponse{
				ID: callCount.Load(), SourceURL: "http://example.com",
				CreatedAt: time.Now().UTC().Format(time.RFC3339), Status: "published",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}
}

// buildTestScheduler wires up a full Scheduler with mock dependencies.
// deepSeekContent: the JSON string returned as the AI response message content.
// items: items returned by the mock RSS fetcher.
// trustedDomains: domains to trust in the verifier.
// publishSrv: the mock website API server.
func buildTestScheduler(
	t *testing.T,
	deepSeekContent string,
	items []models.RawItem,
	trustedDomains []string,
	publishSrv *httptest.Server,
) *scheduler.Scheduler {
	t.Helper()

	// Mock DeepSeek server.
	dsMux := http.NewServeMux()
	dsMux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionEnvelope(deepSeekContent))
	})
	dsSrv := httptest.NewServer(dsMux)
	t.Cleanup(dsSrv.Close)

	aiClient := processor.NewDeepSeekClient("test-key", dsSrv.URL)
	proc := processor.New(aiClient, "deepseek-chat", slog.Default())

	// Mock fetcher returning predefined items.
	rssMock := &mockFetcher{name: "rss", items: items}
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{rssMock}, cacheFile, slog.Default())

	// Verifier.
	ver := verifier.New(trustedDomains, false, slog.Default())

	// Publisher pointed at mock server.
	pub := publisher.New(publishSrv.URL, "test-token", publishSrv.Client(), slog.Default())

	// Config: only RSS feeds (not NewsAPI) so only the RSS fetcher is exercised.
	cfg := &config.Config{
		RSSFeeds: []string{"http://mock-feed.example.com/rss"}, // single placeholder
		DefaultCategories: models.AllCategories,
		SkipVerification: false,
	}

	return scheduler.New(mgr, proc, ver, pub, cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)), // discard logs
	)
}

func makeRawItem(url, domain string) models.RawItem {
	return models.RawItem{
		URL: url, SourceDomain: domain, SourceType: models.SourceTypeRSS,
		Title: "Test Article: " + url, Description: "Description",
		PublishedAt: time.Now().UTC(), FetchedAt: time.Now().UTC(), Language: "en",
	}
}

// ---------------------------------------------------------------------------
// RunForCategories (Run) tests
// ---------------------------------------------------------------------------

func TestScheduler_RunDefault_SuccessfulPipeline(t *testing.T) {
	item := makeRawItem("http://reuters.com/article/sched-1", "reuters.com")

	aiJSON := `[{"url":"http://reuters.com/article/sched-1","category":"金融","summary":"调度器测试摘要","credibility_score":0.95,"tags":["finance"],"language":"en"}]`

	var publishCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&publishCallCount))
	defer pubSrv.Close()

	sched := buildTestScheduler(t, aiJSON, []models.RawItem{item}, []string{"reuters.com"}, pubSrv)

	result := sched.Run(context.Background())

	assert.NoError(t, result.FatalError)
	assert.Equal(t, 1, result.TotalFetched)
	assert.Equal(t, 1, result.TotalProcessed)
	assert.Equal(t, 1, result.TotalPublished)
	assert.Equal(t, 0, result.TotalSkipped)
	assert.Equal(t, 0, result.TotalFailed)
	assert.Greater(t, result.DurationMs, int64(0))
	assert.NotEmpty(t, result.RunID)
}

func TestScheduler_RunDefault_NoFetchedItems_ReturnsZeroCounts(t *testing.T) {
	// Mock fetcher returns no items.
	var publishCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&publishCallCount))
	defer pubSrv.Close()

	sched := buildTestScheduler(t, `[]`, []models.RawItem{}, []string{}, pubSrv)

	result := sched.Run(context.Background())

	assert.NoError(t, result.FatalError)
	assert.Equal(t, 0, result.TotalFetched)
	assert.Equal(t, 0, result.TotalPublished)
	assert.Equal(t, int32(0), publishCallCount.Load(), "publisher should not be called when no items")
}

func TestScheduler_RunDefault_AllSourcesFail_FatalErrorSet(t *testing.T) {
	// Fetcher returns an error.
	var publishCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&publishCallCount))
	defer pubSrv.Close()

	// Use a DeepSeek server (never reached in this case).
	dsMux := http.NewServeMux()
	dsMux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(chatCompletionEnvelope("[]"))
	})
	dsSrv := httptest.NewServer(dsMux)
	defer dsSrv.Close()

	aiClient := processor.NewDeepSeekClient("test-key", dsSrv.URL)
	proc := processor.New(aiClient, "deepseek-chat", slog.Default())

	// Fetcher that always returns an error.
	errFetcher := &mockFetcher{
		name: "rss",
		err:  &fetcher.FetchError{Source: "rss", URL: "http://bad-feed", Wrapped: nil},
	}
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{errFetcher}, cacheFile, slog.Default())

	ver := verifier.New(nil, false, slog.Default())
	pub := publisher.New(pubSrv.URL, "token", pubSrv.Client(), slog.Default())

	cfg := &config.Config{
		RSSFeeds:          []string{"http://bad-feed.example.com/rss"},
		DefaultCategories: models.AllCategories,
	}

	sched := scheduler.New(mgr, proc, ver, pub, cfg, slog.Default())
	result := sched.Run(context.Background())

	assert.Error(t, result.FatalError, "all sources failing should set FatalError")
}

// ---------------------------------------------------------------------------
// RunForCategory tests
// ---------------------------------------------------------------------------

func TestScheduler_RunForCategory_SingleCategory_FlowsThroughPipeline(t *testing.T) {
	item := makeRawItem("http://theverge.com/tech/sched-cat-1", "theverge.com")

	aiJSON := `[{"url":"http://theverge.com/tech/sched-cat-1","category":"科技/AI","summary":"科技类别摘要","credibility_score":0.9,"tags":["tech","AI"],"language":"en"}]`

	var publishCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&publishCallCount))
	defer pubSrv.Close()

	sched := buildTestScheduler(t, aiJSON, []models.RawItem{item}, []string{"theverge.com"}, pubSrv)

	articles, err := sched.RunForCategory(context.Background(), models.CategoryTechAI)

	require.NoError(t, err)
	require.NotEmpty(t, articles, "at least one article should pass through pipeline")

	// All returned articles must have passed verification.
	for _, a := range articles {
		assert.True(t, a.Verification.Pass, "RunForCategory should return only passing articles")
	}
}

func TestScheduler_RunForCategory_ReturnsOnlyVerifiedArticles(t *testing.T) {
	// Trusted domain → passes; unknown domain with low score → skipped.
	items := []models.RawItem{
		makeRawItem("http://reuters.com/finance/1", "reuters.com"),
	}

	aiJSON := `[{"url":"http://reuters.com/finance/1","category":"金融","summary":"可信金融摘要","credibility_score":0.95,"tags":["finance"],"language":"en"}]`

	var pubCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&pubCallCount))
	defer pubSrv.Close()

	sched := buildTestScheduler(t, aiJSON, items, []string{"reuters.com"}, pubSrv)

	articles, err := sched.RunForCategory(context.Background(), models.CategoryFinance)
	require.NoError(t, err)

	for _, a := range articles {
		assert.True(t, a.Verification.Pass)
	}
}

// ---------------------------------------------------------------------------
// Verifier — skipped items not published
// ---------------------------------------------------------------------------

func TestScheduler_RunForCategories_SkippedItems_NotPassedToPublisher(t *testing.T) {
	// All items come from an untrusted domain with low credibility score.
	items := []models.RawItem{
		makeRawItem("http://low-credibility-blog.io/article-1", "low-credibility-blog.io"),
		makeRawItem("http://low-credibility-blog.io/article-2", "low-credibility-blog.io"),
	}

	// AI returns low credibility scores (below 0.7 threshold).
	aiJSON := `[
		{"url":"http://low-credibility-blog.io/article-1","category":"国际","summary":"低可信度摘要","credibility_score":0.2,"tags":[],"language":"en"},
		{"url":"http://low-credibility-blog.io/article-2","category":"国际","summary":"低可信度摘要2","credibility_score":0.3,"tags":[],"language":"en"}
	]`

	var publishCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&publishCallCount))
	defer pubSrv.Close()

	// No trusted domains → low scores cause skip.
	sched := buildTestScheduler(t, aiJSON, items, []string{}, pubSrv)

	result := sched.Run(context.Background())

	assert.NoError(t, result.FatalError)
	assert.Equal(t, 2, result.TotalFetched)
	assert.Equal(t, 2, result.TotalProcessed)
	assert.Equal(t, 0, result.TotalPublished)
	assert.Equal(t, 2, result.TotalSkipped)

	// Publisher must not have been called.
	assert.Equal(t, int32(0), publishCallCount.Load(),
		"publisher should never be called for skipped articles")
}

func TestScheduler_RunForCategories_MixedVerification_OnlyPassingPublished(t *testing.T) {
	items := []models.RawItem{
		makeRawItem("http://reuters.com/trusted-article", "reuters.com"),
		makeRawItem("http://spam-site.xyz/spam-article", "spam-site.xyz"),
	}

	aiJSON := `[
		{"url":"http://reuters.com/trusted-article","category":"政治","summary":"路透社摘要","credibility_score":0.9,"tags":["politics"],"language":"en"},
		{"url":"http://spam-site.xyz/spam-article","category":"政治","summary":"垃圾摘要","credibility_score":0.1,"tags":[],"language":"en"}
	]`

	var publishCallCount atomic.Int32
	var capturedBodies []models.PublishRequest
	pubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agent/articles" {
			publishCallCount.Add(1)
			var req models.PublishRequest
			json.NewDecoder(r.Body).Decode(&req)
			capturedBodies = append(capturedBodies, req)

			resp := models.PublishResponse{
				ID: publishCallCount.Load(),
				CreatedAt: time.Now().UTC().Format(time.RFC3339), Status: "published",
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer pubSrv.Close()

	// Only reuters.com is trusted.
	sched := buildTestScheduler(t, aiJSON, items, []string{"reuters.com"}, pubSrv)

	result := sched.Run(context.Background())

	assert.NoError(t, result.FatalError)
	assert.Equal(t, 2, result.TotalFetched)
	assert.Equal(t, 1, result.TotalPublished, "only the reuters.com article should be published")
	assert.Equal(t, 1, result.TotalSkipped)
	assert.Equal(t, int32(1), publishCallCount.Load())

	// The published article should be the trusted one.
	require.Len(t, capturedBodies, 1)
	assert.Equal(t, "http://reuters.com/trusted-article", capturedBodies[0].SourceURL)
}

// ---------------------------------------------------------------------------
// RunID uniqueness
// ---------------------------------------------------------------------------

func TestScheduler_MultipleRuns_UniqueRunIDs(t *testing.T) {
	var publishCallCount atomic.Int32
	pubSrv := httptest.NewServer(publishSuccessHandler(&publishCallCount))
	defer pubSrv.Close()

	sched := buildTestScheduler(t, `[]`, []models.RawItem{}, []string{}, pubSrv)

	r1 := sched.Run(context.Background())
	r2 := sched.Run(context.Background())

	assert.NotEmpty(t, r1.RunID)
	assert.NotEmpty(t, r2.RunID)
	assert.NotEqual(t, r1.RunID, r2.RunID, "each run should get a unique RunID")
}

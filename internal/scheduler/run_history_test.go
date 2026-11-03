package scheduler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/user/daily-info-agent/internal/extract"
	"github.com/user/daily-info-agent/pkg/models"
)

// TestScheduler_Run_RecordsExtractedCount: with an extractor wired, the run
// result (and thus the persisted run log) carries the extraction count.
func TestScheduler_Run_RecordsExtractedCount(t *testing.T) {
	// Article page server: serves extractable Chinese full text.
	var hits atomic.Int32
	pageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><article><p>" +
			strings.Repeat("这是可以被提取的正文内容。", 60) +
			"</p></article></body></html>"))
	}))
	defer pageSrv.Close()

	publishCalls := &atomic.Int32{}
	publishSrv := httptest.NewServer(publishSuccessHandler(publishCalls))
	defer publishSrv.Close()

	longFeed := makeRawItem("http://long-feed.example.com/article", "long-feed.example.com")
	longFeed.Content = strings.Repeat("自带的正文内容。", 100) // ≥ min runes → extractor skips
	items := []models.RawItem{
		makeRawItem(pageSrv.URL+"/post/1", "example.com"),
		longFeed,
	}
	sched := buildTestScheduler(
		t,
		`[{"url":"`+pageSrv.URL+`/post/1","category":"金融","summary":"摘要","credibility_score":0.95,"tags":["x"],"language":"en"},
		   {"url":"http://long-feed.example.com/article","category":"科技/AI","summary":"摘要","credibility_score":0.9,"tags":["y"],"language":"zh"}]`,
		items,
		[]string{"example.com", "long-feed.example.com"},
		publishSrv,
	)
	sched = sched.WithExtractor(extract.New(pageSrv.Client(), 10, 2, nil))

	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryFinance, models.CategoryTechAI})
	if result.FatalError != nil {
		t.Fatalf("unexpected fatal error: %v", result.FatalError)
	}

	if result.TotalExtracted != 1 {
		t.Errorf("TotalExtracted = %d, want 1 (one extractable page, one item whose feed already carries full text)", result.TotalExtracted)
	}
	if hits.Load() == 0 {
		t.Error("extractor never hit the article page server")
	}
}

// TestScheduler_Run_NoExtractor_ZeroExtracted: the default (no extractor)
// pipeline reports zero extracted instead of a misleading count.
func TestScheduler_Run_NoExtractor_ZeroExtracted(t *testing.T) {
	publishCalls := &atomic.Int32{}
	publishSrv := httptest.NewServer(publishSuccessHandler(publishCalls))
	defer publishSrv.Close()

	items := []models.RawItem{makeRawItem("http://reuters.com/article/health-1", "reuters.com")}
	sched := buildTestScheduler(
		t,
		`[{"url":"http://reuters.com/article/health-1","category":"金融","summary":"摘要","credibility_score":0.95,"tags":["x"],"language":"en"}]`,
		items,
		[]string{"reuters.com"},
		publishSrv,
	)

	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryFinance})
	if result.FatalError != nil {
		t.Fatalf("unexpected fatal error: %v", result.FatalError)
	}
	if result.TotalExtracted != 0 {
		t.Errorf("TotalExtracted = %d, want 0 without an extractor", result.TotalExtracted)
	}
	if result.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", result.DurationMs)
	}
}

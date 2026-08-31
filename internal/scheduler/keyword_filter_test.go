package scheduler_test

import (
	"github.com/user/daily-info-agent/pkg/config"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/daily-info-agent/pkg/metrics"
	"github.com/user/daily-info-agent/pkg/models"
)

// TestScheduler_KeywordWhitelist_FiltersBeforeProcessing verifies the keyword
// subscription filter runs after fetch (post-dedup) and before AI processing:
// non-matching items never reach the processor or the publisher, the metric
// counts the removals, and RunResult.TotalFetched reflects the survivors.
func TestScheduler_KeywordWhitelist_FiltersBeforeProcessing(t *testing.T) {
	publishCalls := &atomic.Int32{}
	publishSrv := httptest.NewServer(publishSuccessHandler(publishCalls))
	defer publishSrv.Close()

	items := []models.RawItem{
		{
			URL: "https://example.com/chip", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "国产芯片新突破", Description: "chip breakthrough", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
		{
			URL: "https://example.com/stocks", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "股市大盘走势分析", Description: "stock market", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
	}

	sched := buildTestScheduler(
		t,
		`{"category":"科技/AI","summary":"摘要","credibility_score":0.9,"tags":["芯片"]}`,
		items,
		[]string{"example.com"},
		publishSrv,
		func(cfg *config.Config) {
			cfg.KeywordWhitelistRaw = "芯片"
		},
	)

	metrics.App.ItemsKeywordFiltered.Store(0)
	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryTechAI})

	if result.TotalFetched != 1 {
		t.Errorf("TotalFetched = %d, want 1 (only whitelist match survives)", result.TotalFetched)
	}
	if got := metrics.App.ItemsKeywordFiltered.Load(); got != 1 {
		t.Errorf("ItemsKeywordFiltered = %d, want 1", got)
	}
	if result.FatalError != nil {
		t.Errorf("unexpected fatal error: %v", result.FatalError)
	}
}

// TestScheduler_KeywordBlacklist_DropsMatchingItems: blacklist removes only
// the matching item, the unrelated one flows through.
func TestScheduler_KeywordBlacklist_DropsMatchingItems(t *testing.T) {
	publishCalls := &atomic.Int32{}
	publishSrv := httptest.NewServer(publishSuccessHandler(publishCalls))
	defer publishSrv.Close()

	items := []models.RawItem{
		{
			URL: "https://example.com/ad", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "【广告】限时优惠", Description: "ad", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
		{
			URL: "https://example.com/news", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "正经科技新闻", Description: "tech news", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
	}

	sched := buildTestScheduler(
		t,
		`{"category":"科技/AI","summary":"摘要","credibility_score":0.9,"tags":["科技"]}`,
		items,
		[]string{"example.com"},
		publishSrv,
		func(cfg *config.Config) {
			cfg.KeywordBlacklistRaw = "广告"
		},
	)

	metrics.App.ItemsKeywordFiltered.Store(0)
	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryTechAI})

	if result.TotalFetched != 1 {
		t.Errorf("TotalFetched = %d, want 1", result.TotalFetched)
	}
	if got := metrics.App.ItemsKeywordFiltered.Load(); got != 1 {
		t.Errorf("ItemsKeywordFiltered = %d, want 1", got)
	}
}

// TestScheduler_KeywordFilter_FiltersEverything_CountsAsFailure: when the
// subscription matches nothing, zero items flow on — which the failure-tracker
// treats like a zero-fetch run (existing semantics, documented behaviour).
func TestScheduler_KeywordFilter_FiltersEverything_CountsAsFailure(t *testing.T) {
	publishSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer publishSrv.Close()

	items := []models.RawItem{
		{
			URL: "https://example.com/a", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "普通新闻一", Description: "d1", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
		{
			URL: "https://example.com/b", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "普通新闻二", Description: "d2", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
	}

	sched := buildTestScheduler(
		t,
		`{"category":"科技/AI","summary":"摘要","credibility_score":0.9,"tags":["x"]}`,
		items,
		[]string{"example.com"},
		publishSrv,
		func(cfg *config.Config) {
			cfg.KeywordWhitelistRaw = "完全不存在的关键词"
		},
	)

	metrics.App.ItemsKeywordFiltered.Store(0)
	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryTechAI})

	if result.TotalFetched != 0 {
		t.Errorf("TotalFetched = %d, want 0", result.TotalFetched)
	}
	if got := metrics.App.ItemsKeywordFiltered.Load(); got != 2 {
		t.Errorf("ItemsKeywordFiltered = %d, want 2", got)
	}
}

// TestScheduler_NoKeywords_BehaviourUnchanged: with no keyword config the
// filter is a no-op — every fetched item flows through exactly as before.
func TestScheduler_NoKeywords_BehaviourUnchanged(t *testing.T) {
	publishCalls := &atomic.Int32{}
	publishSrv := httptest.NewServer(publishSuccessHandler(publishCalls))
	defer publishSrv.Close()

	items := []models.RawItem{
		{
			URL: "https://example.com/1", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "新闻一", Description: "d1", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
		{
			URL: "https://example.com/2", SourceDomain: "example.com", SourceType: models.SourceTypeRSS,
			Title: "新闻二", Description: "d2", PublishedAt: nowUTC(), FetchedAt: nowUTC(), Language: "zh",
		},
	}

	sched := buildTestScheduler(
		t,
		`{"category":"科技/AI","summary":"摘要","credibility_score":0.9,"tags":["x"]}`,
		items,
		[]string{"example.com"},
		publishSrv,
	)

	metrics.App.ItemsKeywordFiltered.Store(0)
	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryTechAI})

	if result.TotalFetched != 2 {
		t.Errorf("TotalFetched = %d, want 2 (filter must be a no-op)", result.TotalFetched)
	}
	if got := metrics.App.ItemsKeywordFiltered.Load(); got != 0 {
		t.Errorf("ItemsKeywordFiltered = %d, want 0 when unconfigured", got)
	}
}

func nowUTC() time.Time { return time.Now().UTC() }

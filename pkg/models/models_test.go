package models

import (
	"strings"
	"testing"
	"time"
)

func TestCategory_DisplayName(t *testing.T) {
	tests := []struct {
		cat  Category
		want string
	}{
		{CategoryFinance, "金融 (Finance)"},
		{CategoryPolitics, "政治 (Politics)"},
		{CategoryEconomy, "经济 (Economy)"},
		{CategoryTechAI, "科技/AI (Tech/AI)"},
		{CategoryInternational, "国际 (International)"},
		{Category("未知"), "未知"},
	}
	for _, tt := range tests {
		if got := tt.cat.DisplayName(); got != tt.want {
			t.Errorf("%q.DisplayName() = %q, want %q", tt.cat, got, tt.want)
		}
	}
}

func TestCategory_IsValid(t *testing.T) {
	for _, c := range AllCategories {
		if !c.IsValid() {
			t.Errorf("%q should be valid", c)
		}
	}
	if Category("unknown").IsValid() {
		t.Error("'unknown' should not be valid")
	}
	if Category("").IsValid() {
		t.Error("empty string should not be valid")
	}
}

func TestRawItem_Fields(t *testing.T) {
	now := time.Now()
	item := &RawItem{
		URL:          "https://example.com/article",
		SourceDomain: "example.com",
		SourceType:   SourceTypeRSS,
		Title:        "Test Article",
		Description:  "A test description",
		Content:      "Full content here",
		PublishedAt:  now,
		FetchedAt:    now,
		Language:     "en",
	}
	if item.URL != "https://example.com/article" {
		t.Errorf("URL mismatch")
	}
	if item.SourceDomain != "example.com" {
		t.Errorf("SourceDomain mismatch")
	}
	if item.SourceType != SourceTypeRSS {
		t.Errorf("SourceType mismatch")
	}
}

func TestSourceType_Constants(t *testing.T) {
	if SourceTypeRSS != "rss" {
		t.Errorf("SourceTypeRSS = %q, want 'rss'", SourceTypeRSS)
	}
	if SourceTypeNewsAPI != "newsapi" {
		t.Errorf("SourceTypeNewsAPI = %q, want 'newsapi'", SourceTypeNewsAPI)
	}
	if SourceTypeRSSHub != "rsshub" {
		t.Errorf("SourceTypeRSSHub = %q, want 'rsshub'", SourceTypeRSSHub)
	}
}

func TestAIItemResult(t *testing.T) {
	r := AIItemResult{
		URL:              "https://example.com/a",
		Category:         CategoryTechAI,
		Summary:          "这是一个测试摘要。",
		CredibilityScore: 0.85,
		Tags:             []string{"AI", "test"},
		Language:         "zh",
	}
	if r.URL != "https://example.com/a" {
		t.Errorf("URL mismatch")
	}
	if r.CredibilityScore != 0.85 {
		t.Errorf("CredibilityScore mismatch")
	}
	if len(r.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(r.Tags))
	}
}

func TestVerificationResult(t *testing.T) {
	v := VerificationResult{Pass: true, DomainHit: true}
	if !v.Pass {
		t.Error("expected pass=true")
	}
	if !v.DomainHit {
		t.Error("expected DomainHit=true")
	}

	v2 := VerificationResult{
		Pass:       false,
		SkipReason: SkipReasonLowScore,
		DomainHit:  false,
	}
	if v2.Pass {
		t.Error("expected pass=false")
	}
	if v2.SkipReason != SkipReasonLowScore {
		t.Errorf("expected 'low_credibility_score', got %q", v2.SkipReason)
	}
}

func TestSkipReason_Constants(t *testing.T) {
	if SkipReasonLowScore != "low_credibility_score" {
		t.Errorf("SkipReasonLowScore = %q", SkipReasonLowScore)
	}
	if SkipReasonNotWhitelisted != "domain_not_whitelisted_and_score_below_threshold" {
		t.Errorf("SkipReasonNotWhitelisted = %q", SkipReasonNotWhitelisted)
	}
}

func TestProcessedArticle(t *testing.T) {
	item := &RawItem{URL: "https://example.com/a", Title: "Test"}
	a := ProcessedArticle{
		Raw:              item,
		Category:         CategoryEconomy,
		Summary:          "经济新闻摘要。",
		CredibilityScore: 0.9,
		Tags:             []string{"economy"},
		DetectedLanguage: "zh",
		Verification:     VerificationResult{Pass: true},
		RunID:            "run-123",
		AgentVersion:     "2.0.0",
	}
	if a.Raw.Title != "Test" {
		t.Errorf("embedded Raw.Title mismatch")
	}
	if a.RunID != "run-123" {
		t.Errorf("RunID mismatch")
	}
	if !a.Verification.Pass {
		t.Error("Verification.Pass should be true")
	}
}

func TestPublishRequest(t *testing.T) {
	req := PublishRequest{
		SourceURL:        "https://example.com/a",
		Title:            "Test",
		Summary:          "摘要",
		Category:         "科技/AI",
		SourceDomain:     "example.com",
		CredibilityScore: 0.85,
		PublishedAt:      "2026-07-03T01:00:00Z",
		FetchedAt:        "2026-07-03T01:05:00Z",
		RunID:            "run-123",
		Tags:             []string{"AI"},
		Language:         "zh",
		AgentVersion:     "2.0.0",
	}
	if req.SourceURL != "https://example.com/a" {
		t.Errorf("SourceURL mismatch")
	}
	if req.Category != "科技/AI" {
		t.Errorf("Category mismatch")
	}
}

func TestChatResponse_SessionBased(t *testing.T) {
	resp := ChatResponse{
		SessionID:  "sess-123",
		Reply:      "这是AI回复的内容。",
		Sources: []ChatSource{
			{URL: "https://example.com", Title: "Article 1", SourceDomain: "example.com"},
		},
		ToolCalled: true,
		FetchedAt:  "2026-07-03T01:00:00Z",
		LatencyMs:  4230,
	}
	if resp.SessionID != "sess-123" {
		t.Errorf("SessionID mismatch")
	}
	if resp.Reply != "这是AI回复的内容。" {
		t.Errorf("Reply mismatch")
	}
	if len(resp.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(resp.Sources))
	}
	if !resp.ToolCalled {
		t.Error("ToolCalled should be true")
	}
	if resp.LatencyMs != 4230 {
		t.Errorf("LatencyMs mismatch")
	}
}

func TestRunResult(t *testing.T) {
	r := RunResult{
		RunID:          "run-123",
		TotalFetched:   100,
		TotalProcessed: 90,
		TotalSaved:     85,
		TotalPublished: 80,
		TotalSkipped:   5,
		TotalFailed:    0,
		DurationMs:     12345,
	}
	if r.TotalFetched != 100 {
		t.Errorf("TotalFetched mismatch")
	}
	if r.TotalSaved+r.TotalSkipped+r.TotalFailed != r.TotalProcessed {
		t.Errorf("saved+skipped+failed (%d) != processed (%d)",
			r.TotalSaved+r.TotalSkipped+r.TotalFailed, r.TotalProcessed)
	}
	if r.FatalError != nil {
		t.Error("FatalError should be nil")
	}
}

func TestArticleFilter_PointerFields(t *testing.T) {
	cat := CategoryTechAI
	status := "published"
	f := ArticleFilter{
		Category: &cat,
		Status:   &status,
		Query:    "AI芯片",
		Page:     1,
		PageSize: 20,
	}
	if f.Page != 1 {
		t.Errorf("Page mismatch")
	}
	if f.PageSize != 20 {
		t.Errorf("PageSize mismatch")
	}
	if f.Category == nil || *f.Category != CategoryTechAI {
		t.Errorf("Category mismatch")
	}
	if f.Status == nil || *f.Status != "published" {
		t.Errorf("Status mismatch")
	}
	if f.Query != "AI芯片" {
		t.Errorf("Query mismatch")
	}
}

func TestArticleFilter_NilFilter(t *testing.T) {
	f := ArticleFilter{Page: 1, PageSize: 20}
	if f.Category != nil {
		t.Error("Category should be nil")
	}
	if f.Status != nil {
		t.Error("Status should be nil")
	}
}

func TestArticleRow(t *testing.T) {
	now := time.Now()
	row := ArticleRow{
		ID:               1,
		Title:            "Test",
		Summary:          "摘要",
		SourceURL:        "https://example.com/a",
		SourceDomain:     "example.com",
		Category:         CategoryTechAI,
		CredibilityScore: 0.85,
		Status:           "published",
		Tags:             []string{"AI"},
		Language:         "zh",
		RunID:            "run-123",
		FetchedAt:        now,
		PublishedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if row.ID != 1 {
		t.Errorf("ID mismatch")
	}
	if row.Status != "published" {
		t.Errorf("Status mismatch")
	}
	if row.PublishedAt == nil {
		t.Error("PublishedAt should not be nil")
	}
}

func TestStatsResult(t *testing.T) {
	s := StatsResult{
		ByDay: []DayStat{
			{Date: "2026-07-03", Count: 50},
		},
		ByCategory: []CategoryStat{
			{Category: "科技/AI", Count: 25},
		},
		RecentRuns: []RunLogRow{
			{RunID: "run-123", TotalFetched: 100},
		},
	}
	if len(s.ByDay) != 1 {
		t.Errorf("expected 1 day stat, got %d", len(s.ByDay))
	}
	if len(s.ByCategory) != 1 {
		t.Errorf("expected 1 category stat, got %d", len(s.ByCategory))
	}
	if len(s.RecentRuns) != 1 {
		t.Errorf("expected 1 run log, got %d", len(s.RecentRuns))
	}
	if s.ByDay[0].Date != "2026-07-03" {
		t.Errorf("Date mismatch")
	}
}

func TestFetchConfig(t *testing.T) {
	cfg := FetchConfig{
		Type:       SourceTypeRSS,
		URL:        "https://example.com/feed.xml",
		Categories: []Category{CategoryTechAI, CategoryFinance},
		Params:     map[string]string{"q": "AI"},
		Timeout:    10 * time.Second,
	}
	if cfg.Type != SourceTypeRSS {
		t.Errorf("Type mismatch")
	}
	if len(cfg.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(cfg.Categories))
	}
}

func TestAllCategories_Complete(t *testing.T) {
	expected := []Category{
		CategoryFinance,
		CategoryPolitics,
		CategoryEconomy,
		CategoryTechAI,
		CategoryInternational,
	}
	if len(AllCategories) != len(expected) {
		t.Errorf("expected %d categories, got %d", len(expected), len(AllCategories))
	}
	for i, c := range AllCategories {
		if c != expected[i] {
			t.Errorf("AllCategories[%d] = %q, want %q", i, c, expected[i])
		}
	}
}

func TestPublishResponse(t *testing.T) {
	resp := PublishResponse{
		ID:        12345,
		SourceURL: "https://example.com/a",
		CreatedAt: "2026-07-03T01:06:00Z",
		Status:    "published",
	}
	if resp.ID != 12345 {
		t.Errorf("ID mismatch")
	}
	if resp.Status != "published" {
		t.Errorf("Status mismatch")
	}
}

func TestPublishErrorResponse(t *testing.T) {
	errResp := PublishErrorResponse{
		Error:      "duplicate_article",
		Message:    "An article with this source_url already exists.",
		ExistingID: 12300,
	}
	if errResp.Error != "duplicate_article" {
		t.Errorf("Error code mismatch")
	}
	if errResp.ExistingID != 12300 {
		t.Errorf("ExistingID mismatch")
	}
}

func TestRunLogRow(t *testing.T) {
	now := time.Now()
	row := RunLogRow{
		RunID:          "run-123",
		TotalFetched:   100,
		TotalProcessed: 90,
		TotalSaved:     85,
		TotalPublished: 80,
		TotalSkipped:   5,
		TotalFailed:    5,
		DurationMs:     12345,
		StartedAt:      now,
		FinishedAt:     now.Add(12 * time.Second),
	}
	if row.RunID != "run-123" {
		t.Errorf("RunID mismatch")
	}
	if !row.FinishedAt.After(row.StartedAt) {
		t.Error("FinishedAt should be after StartedAt")
	}
}

func TestProgressEvent(t *testing.T) {
	e := ProgressEvent{
		Stage:   "fetch",
		Status:  "done",
		Message: "已完成抓取",
		Count:   50,
		Passed:  40,
		Skipped: 5,
		Failed:  5,
		RunID:   "run-123",
	}
	if e.Stage != "fetch" {
		t.Errorf("Stage mismatch")
	}
	if e.Count != 50 {
		t.Errorf("Count mismatch")
	}
	if e.RunID != "run-123" {
		t.Errorf("RunID mismatch")
	}
}

func TestFetchTriggerResponse(t *testing.T) {
	resp := FetchTriggerResponse{
		RunID:     "run-123",
		Triggered: true,
		Message:   "Fetch started",
	}
	if resp.RunID == "" {
		t.Error("RunID should not be empty")
	}
	if !resp.Triggered {
		t.Error("Triggered should be true")
	}
}

func TestCategoryStat(t *testing.T) {
	cs := CategoryStat{
		Category: "科技/AI",
		Count:    25,
	}
	if cs.Count < 0 {
		t.Error("Count should not be negative")
	}
}

func TestDayStat(t *testing.T) {
	ds := DayStat{
		Date:  "2026-07-03",
		Count: 100,
	}
	if ds.Count < 0 {
		t.Error("Count should not be negative")
	}
	if ds.Date != "2026-07-03" {
		t.Errorf("Date mismatch")
	}
}

func TestChatRequest(t *testing.T) {
	req := ChatRequest{Message: "帮我查一下今天AI新闻"}
	if req.Message == "" {
		t.Error("Message should not be empty")
	}
	if len(req.Message) > 500 {
		t.Error("Message exceeds 500 chars")
	}
}

func TestLongSummary(t *testing.T) {
	longSummary := strings.Repeat("测试摘要", 50) // 200 chars
	item := ProcessedArticle{Summary: longSummary}
	if len([]rune(item.Summary)) < 100 {
		t.Error("summary too short")
	}
}

func TestChatErrorResponse(t *testing.T) {
	e := ChatErrorResponse{
		Error:   "validation_error",
		Message: "Message is required",
	}
	if e.Error != "validation_error" {
		t.Errorf("Error code mismatch")
	}
}

func TestAIBatchRequest(t *testing.T) {
	req := AIBatchRequest{
		Items: []*RawItem{
			{URL: "https://example.com/1"},
			{URL: "https://example.com/2"},
		},
		RunID: "run-123",
	}
	if len(req.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(req.Items))
	}
	if req.RunID != "run-123" {
		t.Errorf("RunID mismatch")
	}
}

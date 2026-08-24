// Package scheduler orchestrates the full scheduled news-processing pipeline.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/user/daily-info-agent/internal/dedup"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/filter"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/metrics"
	"github.com/user/daily-info-agent/pkg/models"
)

// DigestSender sends a post-run email digest. *notifier.Notifier implements this.
type DigestSender interface {
	SendDailySummary(ctx context.Context, articles []models.ProcessedArticle, result models.RunResult) error
}

// Scheduler owns the full scheduled pipeline.
type Scheduler struct {
	mgr    *fetcher.Manager
	proc   *processor.Processor
	ver    *verifier.Verifier
	pub    *publisher.Client  // may be nil when Java API is not configured
	st     store.ArticleStore // may be nil when DATABASE_DSN is not set
	notif  DigestSender       // may be nil when SMTP is not configured
	cfg    *config.Config
	logger *slog.Logger
	filter *filter.KeywordFilter // nil-safe: pass-through when unconfigured

	// Consecutive-failure alerting.
	failureMu          sync.Mutex
	consecutiveFail    int
	onFailures         func(consecutiveFailures int) // called when threshold crossed (may be nil)
	onFailureThreshold int
}

// OnFailureThreshold returns the default number of consecutive failed runs
// before an alert fires. Exported for tests.
const DefaultFailureAlertThreshold = 3

// durationMs converts an elapsed duration to whole milliseconds, rounding up
// so a real (sub-millisecond) run never reports a misleading 0.
func durationMs(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()
	if ms == 0 {
		return 1
	}
	return ms
}

// New wires all pipeline stages together.
// pub and st may be nil; when nil those stages are skipped.
func New(
	mgr *fetcher.Manager,
	proc *processor.Processor,
	ver *verifier.Verifier,
	pub *publisher.Client,
	st store.ArticleStore,
	cfg *config.Config,
	logger *slog.Logger,
) *Scheduler {
	return &Scheduler{
		mgr:    mgr,
		proc:   proc,
		ver:    ver,
		pub:    pub,
		st:     st,
		cfg:    cfg,
		logger: logger,
		filter: filter.New(
			filter.SplitKeywords(cfg.KeywordWhitelistRaw),
			filter.SplitKeywords(cfg.KeywordBlacklistRaw),
		),
	}
}

// WithNotifier sets an optional digest sender called after each scheduled run.
func (s *Scheduler) WithNotifier(n DigestSender) *Scheduler {
	s.notif = n
	return s
}

// WithFailureAlert enables an alert callback that fires when a run fails
// (fatal error or zero items fetched) for failureThreshold consecutive times.
// The callback receives the current consecutive-failure count.
func (s *Scheduler) WithFailureAlert(failureThreshold int, cb func(consecutiveFailures int)) *Scheduler {
	if failureThreshold <= 0 {
		failureThreshold = DefaultFailureAlertThreshold
	}
	s.onFailureThreshold = failureThreshold
	s.onFailures = cb
	return s
}

// Run executes the full pipeline for the configured default categories.
// Returns a RunResult; RunResult.FatalError != nil signals exit 1.
func (s *Scheduler) Run(ctx context.Context) models.RunResult {
	return s.RunForCategories(ctx, s.cfg.DefaultCategories)
}

// RunForCategories executes the full pipeline for the given categories.
func (s *Scheduler) RunForCategories(ctx context.Context, categories []models.Category) models.RunResult {
	return s.runPipeline(ctx, categories, nil, uuid.New().String())
}

// RunWithProgress executes the full pipeline and calls emit after each stage.
// emit is called synchronously from the pipeline goroutine; implementations must not block.
func (s *Scheduler) RunWithProgress(ctx context.Context, categories []models.Category, emit func(models.ProgressEvent)) models.RunResult {
	return s.runPipeline(ctx, categories, emit, uuid.New().String())
}

// RunWithProgressAndID is like RunWithProgress but uses the given runID.
// Useful when the caller needs to correlate the run with an external trigger
// (e.g. an HTTP handler that returned the runID before the async run started).
// If runID is empty a new one is generated.
func (s *Scheduler) RunWithProgressAndID(ctx context.Context, categories []models.Category, emit func(models.ProgressEvent), runID string) models.RunResult {
	if runID == "" {
		runID = uuid.New().String()
	}
	return s.runPipeline(ctx, categories, emit, runID)
}

// runPipeline is the shared implementation of RunForCategories and RunWithProgress.
// Each stage is delegated to its own method for testability and readability.
func (s *Scheduler) runPipeline(ctx context.Context, categories []models.Category, emit func(models.ProgressEvent), runID string) models.RunResult {
	fire := func(e models.ProgressEvent) {
		if emit != nil {
			emit(e)
		}
	}

	start := time.Now()
	s.logger.Info("scheduler run starting",
		slog.String("run_id", runID),
		slog.Int("categories", len(categories)),
	)

	result := models.RunResult{RunID: runID}

	// ---- Fetch stage ----
	items, abort := s.fetchStage(ctx, categories, runID, fire)
	if abort {
		result.FatalError = fmt.Errorf("fetch stage failed")
		result.DurationMs = durationMs(time.Since(start))
		metrics.App.RunsCompleted.Add(1)
		metrics.App.RunsFailed.Add(1)
		s.trackFailure(&result)
		return result
	}
	result.TotalFetched = len(items)

	if len(items) == 0 {
		s.logger.Info("no new items fetched; run complete", slog.String("run_id", runID))
		result.DurationMs = durationMs(time.Since(start))
		metrics.App.RunsCompleted.Add(1)
		fire(models.ProgressEvent{Stage: "done", Status: "done", RunID: runID, Message: "任务完成（无新内容）"})
		s.trackFailure(&result)
		return result
	}

	// ---- Process stage ----
	articles := s.processStage(ctx, items, runID, fire)
	result.TotalProcessed = len(articles)

	// ---- Verify stage ----
	passing := s.verifyStage(articles, &result, fire)

	// ---- Persist stage ----
	s.persistStage(ctx, articles, runID, &result)

	// ---- Publish stage ----
	s.publishStage(ctx, passing, runID, &result, fire)

	result.DurationMs = durationMs(time.Since(start))

	// ---- Log run summary ----
	s.logRunSummary(ctx, &result, start)

	fire(models.ProgressEvent{Stage: "done", Status: "done", RunID: runID, Message: "任务完成"})

	// ---- Notify stage ----
	s.notifyStage(ctx, passing, result)

	metrics.App.RunsCompleted.Add(1)

	s.trackFailure(&result)

	return result
}

// trackFailure updates the consecutive-failure counter and fires the alert
// callback once the configured threshold is crossed. A run is considered a
// failure when it aborted (FatalError set) or fetched zero items. Successful
// runs reset the counter.
func (s *Scheduler) trackFailure(result *models.RunResult) {
	isFailure := result.FatalError != nil || result.TotalFetched == 0

	s.failureMu.Lock()
	defer s.failureMu.Unlock()

	if !isFailure {
		s.consecutiveFail = 0
		return
	}

	s.consecutiveFail++
	if s.onFailures != nil && s.onFailureThreshold > 0 && s.consecutiveFail == s.onFailureThreshold {
		s.logger.Warn("pipeline consecutive failure threshold reached",
			slog.Int("consecutive_failures", s.consecutiveFail),
			slog.String("run_id", result.RunID),
		)
		go s.onFailures(s.consecutiveFail)
	}
}

// fetchStage fetches items from all sources and deduplicates them.
// Returns (items, shouldAbort) where shouldAbort is true on fatal error.
func (s *Scheduler) fetchStage(ctx context.Context, categories []models.Category, runID string, fire func(models.ProgressEvent)) ([]models.RawItem, bool) {
	fire(models.ProgressEvent{Stage: "fetch", Status: "running", Message: "正在抓取新闻…"})
	fetchStart := time.Now()
	cfgs := s.buildFetchConfigs(categories)

	items, err := s.mgr.FetchAll(ctx, cfgs)
	fetchDuration := time.Since(fetchStart)

	if err != nil {
		s.logger.Error("all sources failed; aborting run",
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
		fire(models.ProgressEvent{Stage: "error", Status: "error", Message: err.Error()})
		return nil, true
	}

	metrics.App.ItemsFetched.Add(int64(len(items)))

	// Dedup stage
	dedupedItems, dedupRemoved := dedup.ByTitle(items, s.cfg.TrustedDomains)
	metrics.App.ItemsDeduped.Add(int64(dedupRemoved))
	if dedupRemoved > 0 {
		s.logger.Info("stage_complete",
			slog.String("stage", "dedup"),
			slog.String("run_id", runID),
			slog.Int("items_removed", dedupRemoved),
			slog.Int("items_remaining", len(dedupedItems)),
		)
	}
	items = dedupedItems

	// Keyword subscription filter — prune noise before it reaches the LLM.
	// Pass-through (and zero metric) when no keywords are configured.
	if s.filter.Enabled() {
		var filteredRemoved int
		items, filteredRemoved = s.filter.Apply(items)
		metrics.App.ItemsKeywordFiltered.Add(int64(filteredRemoved))
		if filteredRemoved > 0 {
			s.logger.Info("stage_complete",
				slog.String("stage", "keyword_filter"),
				slog.String("run_id", runID),
				slog.Int("items_removed", filteredRemoved),
				slog.Int("items_remaining", len(items)),
			)
		}
	}

	s.logger.Info("stage_complete",
		slog.String("stage", "fetch"),
		slog.String("run_id", runID),
		slog.Int64("duration_ms", fetchDuration.Milliseconds()),
		slog.Int("items_fetched", len(items)),
	)

	fetchMsg := fmt.Sprintf("抓取完成：%d 条", len(items))
	if dedupRemoved > 0 {
		fetchMsg = fmt.Sprintf("抓取完成：%d 条（去重移除 %d 条）", len(items), dedupRemoved)
	}
	fire(models.ProgressEvent{
		Stage:   "fetch",
		Status:  "done",
		Count:   len(items),
		Message: fetchMsg,
	})

	return items, false
}

// processStage sends items through AI processing.
func (s *Scheduler) processStage(ctx context.Context, items []models.RawItem, runID string, fire func(models.ProgressEvent)) []models.ProcessedArticle {
	fire(models.ProgressEvent{Stage: "process", Status: "running", Message: "AI 处理中…"})
	procStart := time.Now()
	articles, procErr := s.proc.ProcessBatch(ctx, items, runID)
	procDuration := time.Since(procStart)

	if procErr != nil {
		s.logger.Warn("process batch returned error (degraded mode)",
			slog.String("run_id", runID),
			slog.String("error", procErr.Error()),
		)
	}

	s.logger.Info("stage_complete",
		slog.String("stage", "process"),
		slog.String("run_id", runID),
		slog.Int64("duration_ms", procDuration.Milliseconds()),
		slog.Int("items_processed", len(articles)),
	)
	fire(models.ProgressEvent{
		Stage:   "process",
		Status:  "done",
		Count:   len(articles),
		Message: fmt.Sprintf("AI 处理完成：%d 条", len(articles)),
	})

	return articles
}

// verifyStage runs source verification and returns passing articles.
func (s *Scheduler) verifyStage(articles []models.ProcessedArticle, result *models.RunResult, fire func(models.ProgressEvent)) []models.ProcessedArticle {
	verStart := time.Now()
	articles = s.ver.Verify(articles)
	verDuration := time.Since(verStart)

	var passing []models.ProcessedArticle
	for _, a := range articles {
		if a.LLMSkipped {
			result.TotalSkipped++
			continue
		}
		if a.Verification.Pass {
			passing = append(passing, a)
		} else {
			result.TotalSkipped++
		}
	}
	metrics.App.ItemsPassed.Add(int64(len(passing)))
	metrics.App.ItemsSkipped.Add(int64(result.TotalSkipped))

	s.logger.Info("stage_complete",
		slog.String("stage", "verify"),
		slog.Int64("duration_ms", verDuration.Milliseconds()),
		slog.Int("items_passed", len(passing)),
		slog.Int("items_skipped", result.TotalSkipped),
	)
	fire(models.ProgressEvent{
		Stage:   "verify",
		Status:  "done",
		Passed:  len(passing),
		Skipped: result.TotalSkipped,
		Message: fmt.Sprintf("验证完成：%d 通过，%d 跳过", len(passing), result.TotalSkipped),
	})

	return passing
}

// persistStage saves articles to the database when configured.
func (s *Scheduler) persistStage(ctx context.Context, articles []models.ProcessedArticle, runID string, result *models.RunResult) {
	if s.st == nil {
		return
	}
	saved, err := s.st.SaveArticles(ctx, articles, runID)
	if err != nil {
		s.logger.Warn("failed to save articles to database",
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
	}
	result.TotalSaved = saved
	s.logger.Info("stage_complete",
		slog.String("stage", "persist"),
		slog.String("run_id", runID),
		slog.Int("items_saved", saved),
	)
}

// publishStage publishes passing articles to the website API when configured.
func (s *Scheduler) publishStage(ctx context.Context, passing []models.ProcessedArticle, runID string, result *models.RunResult, fire func(models.ProgressEvent)) {
	fire(models.ProgressEvent{Stage: "publish", Status: "running", Message: "正在发布…"})
	pubStart := time.Now()

	if s.pub != nil {
		for i, article := range passing {
			if i > 0 {
				time.Sleep(100 * time.Millisecond)
			}
			res := s.pub.Publish(ctx, article, runID)
			switch res.Outcome {
			case publisher.OutcomePublished:
				result.TotalPublished++
				metrics.App.ItemsPublished.Add(1)
			case publisher.OutcomeDuplicate:
				result.TotalSkipped++
			default:
				result.TotalFailed++
				metrics.App.PublishFailed.Add(1)
			}
		}
	}

	pubDuration := time.Since(pubStart)
	s.logger.Info("stage_complete",
		slog.String("stage", "publish"),
		slog.String("run_id", runID),
		slog.Int64("duration_ms", pubDuration.Milliseconds()),
		slog.Int("items_published", result.TotalPublished),
		slog.Int("items_failed", result.TotalFailed),
	)
	fire(models.ProgressEvent{
		Stage:   "publish",
		Status:  "done",
		Count:   result.TotalPublished,
		Failed:  result.TotalFailed,
		Message: fmt.Sprintf("发布完成：%d 篇", result.TotalPublished),
	})
}

// logRunSummary saves the run summary to the database.
func (s *Scheduler) logRunSummary(ctx context.Context, result *models.RunResult, start time.Time) {
	if s.st == nil {
		return
	}
	fatalErrStr := ""
	if result.FatalError != nil {
		fatalErrStr = result.FatalError.Error()
	}
	_ = s.st.SaveRunLog(ctx, models.RunLogRow{
		RunID:          result.RunID,
		TotalFetched:   result.TotalFetched,
		TotalProcessed: result.TotalProcessed,
		TotalSaved:     result.TotalSaved,
		TotalPublished: result.TotalPublished,
		TotalSkipped:   result.TotalSkipped,
		TotalFailed:    result.TotalFailed,
		DurationMs:     result.DurationMs,
		FatalError:     fatalErrStr,
		StartedAt:      start,
		FinishedAt:     time.Now(),
	})
	s.logger.Info("scheduler run complete",
		slog.String("run_id", result.RunID),
		slog.Int("total_fetched", result.TotalFetched),
		slog.Int("total_processed", result.TotalProcessed),
		slog.Int("total_saved", result.TotalSaved),
		slog.Int("total_published", result.TotalPublished),
		slog.Int("total_skipped", result.TotalSkipped),
		slog.Int("total_failed", result.TotalFailed),
		slog.Int64("duration_ms", result.DurationMs),
	)
}

// notifyStage sends the daily summary email when configured.
func (s *Scheduler) notifyStage(ctx context.Context, passing []models.ProcessedArticle, result models.RunResult) {
	if s.notif != nil && len(passing) > 0 {
		if err := s.notif.SendDailySummary(ctx, passing, result); err != nil {
			s.logger.Warn("failed to send daily summary email",
				slog.String("run_id", result.RunID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// buildFetchConfigs constructs the slice of FetchConfig from the app config and
// the requested categories.
func (s *Scheduler) buildFetchConfigs(categories []models.Category) []models.FetchConfig {
	var cfgs []models.FetchConfig

	// RSS feeds
	for _, feedURL := range s.cfg.RSSFeeds {
		cfgs = append(cfgs, models.FetchConfig{
			Type:       models.SourceTypeRSS,
			URL:        feedURL,
			Categories: categories,
			Timeout:    10 * time.Second,
		})
	}

	// NewsAPI — one query per category
	for _, cat := range categories {
		cfgs = append(cfgs, models.FetchConfig{
			Type:       models.SourceTypeNewsAPI,
			Categories: []models.Category{cat},
			Params: map[string]string{
				"q":        categoryToNewsAPIQuery(cat),
				"language": "en",
				"pageSize": "20",
			},
			Timeout: 10 * time.Second,
		})
	}

	// Search engine — one query per category
	if s.cfg.SearchEngineEnabled {
		for _, cat := range categories {
			cfgs = append(cfgs, models.FetchConfig{
				Type:       models.SourceTypeSearch,
				URL:        categoryToSearchQuery(cat),
				Categories: []models.Category{cat},
				Timeout:    15 * time.Second,
			})
		}
	}

	return cfgs
}

// categoryToNewsAPIQuery returns a simple keyword query for a given category.
func categoryToNewsAPIQuery(cat models.Category) string {
	switch cat {
	case models.CategoryFinance:
		return "finance stock market"
	case models.CategoryPolitics:
		return "politics government policy"
	case models.CategoryEconomy:
		return "economy GDP trade"
	case models.CategoryTechAI:
		return "technology AI artificial intelligence"
	case models.CategoryInternational:
		return "international world news"
	default:
		return string(cat)
	}
}

// categoryToSearchQuery returns a search-engine query string for the given category.
func categoryToSearchQuery(cat models.Category) string {
	switch cat {
	case models.CategoryFinance:
		return "finance stock market economy news today"
	case models.CategoryPolitics:
		return "politics government policy news today"
	case models.CategoryEconomy:
		return "economy GDP trade business news today"
	case models.CategoryTechAI:
		return "technology AI artificial intelligence news today"
	case models.CategoryInternational:
		return "world international breaking news today"
	default:
		return "latest news today " + string(cat)
	}
}

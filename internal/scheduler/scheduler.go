// Package scheduler orchestrates the full scheduled news-processing pipeline.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

// Scheduler owns the full scheduled pipeline.
type Scheduler struct {
	mgr    *fetcher.Manager
	proc   *processor.Processor
	ver    *verifier.Verifier
	pub    *publisher.Client     // may be nil when Java API is not configured
	st     store.ArticleStore   // may be nil when DATABASE_DSN is not set
	cfg    *config.Config
	logger *slog.Logger
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
	}
}

// Run executes the full pipeline for the configured default categories.
// Returns a RunResult; RunResult.FatalError != nil signals exit 1.
func (s *Scheduler) Run(ctx context.Context) models.RunResult {
	return s.RunForCategories(ctx, s.cfg.DefaultCategories)
}

// RunForCategory runs the pipeline for a single category (used by the chat handler).
func (s *Scheduler) RunForCategory(ctx context.Context, category models.Category) ([]models.ProcessedArticle, error) {
	runID := uuid.New().String()
	cfgs := buildFetchConfigs(s.cfg, []models.Category{category})

	// Fetch
	items, err := s.mgr.FetchAll(ctx, cfgs)
	if err != nil {
		return nil, err
	}

	// Process
	articles, err := s.proc.ProcessBatch(ctx, items, runID)
	if err != nil {
		return nil, err
	}

	// Verify
	articles = s.ver.Verify(articles)

	// Filter passing items
	var passing []models.ProcessedArticle
	for _, a := range articles {
		if a.Verification.Pass {
			passing = append(passing, a)
		}
	}

	return passing, nil
}

// RunForCategories executes the full fetch → process → verify → publish pipeline
// for the given categories and returns a RunResult.
func (s *Scheduler) RunForCategories(ctx context.Context, categories []models.Category) models.RunResult {
	runID := uuid.New().String()
	start := time.Now()

	s.logger.Info("scheduler run starting",
		slog.String("run_id", runID),
		slog.Int("categories", len(categories)),
	)

	result := models.RunResult{RunID: runID}

	// ---- Fetch stage ----
	fetchStart := time.Now()
	cfgs := buildFetchConfigs(s.cfg, categories)

	items, err := s.mgr.FetchAll(ctx, cfgs)
	fetchDuration := time.Since(fetchStart)

	if err != nil {
		s.logger.Error("all sources failed; aborting run",
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
		result.FatalError = err
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	result.TotalFetched = len(items)
	s.logger.Info("stage_complete",
		slog.String("stage", "fetch"),
		slog.String("run_id", runID),
		slog.Int64("duration_ms", fetchDuration.Milliseconds()),
		slog.Int("items_fetched", len(items)),
	)

	if len(items) == 0 {
		s.logger.Info("no new items fetched; run complete",
			slog.String("run_id", runID),
		)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// ---- Process stage ----
	procStart := time.Now()
	articles, procErr := s.proc.ProcessBatch(ctx, items, runID)
	procDuration := time.Since(procStart)

	if procErr != nil {
		// Non-fatal: ProcessBatch degrades gracefully; log and continue.
		s.logger.Warn("process batch returned error (degraded mode)",
			slog.String("run_id", runID),
			slog.String("error", procErr.Error()),
		)
	}

	result.TotalProcessed = len(articles)
	s.logger.Info("stage_complete",
		slog.String("stage", "process"),
		slog.String("run_id", runID),
		slog.Int64("duration_ms", procDuration.Milliseconds()),
		slog.Int("items_processed", len(articles)),
	)

	// ---- Verify stage ----
	verStart := time.Now()
	articles = s.ver.Verify(articles)
	verDuration := time.Since(verStart)

	var passing []models.ProcessedArticle
	for _, a := range articles {
		if a.Verification.Pass {
			passing = append(passing, a)
		} else {
			result.TotalSkipped++
		}
	}

	s.logger.Info("stage_complete",
		slog.String("stage", "verify"),
		slog.String("run_id", runID),
		slog.Int64("duration_ms", verDuration.Milliseconds()),
		slog.Int("items_passed", len(passing)),
		slog.Int("items_skipped", result.TotalSkipped),
	)

	// ---- Persist stage ----
	if s.st != nil {
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

	// ---- Publish stage (optional — only when Java API is configured) ----
	pubStart := time.Now()
	if s.pub != nil {
		for i, article := range passing {
			if i > 0 {
				time.Sleep(100 * time.Millisecond) // rate-limit courtesy
			}
			res := s.pub.Publish(ctx, article, runID)
			switch res.Outcome {
			case publisher.OutcomePublished:
				result.TotalPublished++
			case publisher.OutcomeDuplicate:
				result.TotalSkipped++
			default:
				result.TotalFailed++
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

	result.DurationMs = time.Since(start).Milliseconds()

	// ---- Log run summary to database ----
	if s.st != nil {
		fatalErrStr := ""
		if result.FatalError != nil {
			fatalErrStr = result.FatalError.Error()
		}
		_ = s.st.SaveRunLog(ctx, models.RunLogRow{
			RunID:          runID,
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
	}

	s.logger.Info("scheduler run complete",
		slog.String("run_id", runID),
		slog.Int("total_fetched", result.TotalFetched),
		slog.Int("total_processed", result.TotalProcessed),
		slog.Int("total_saved", result.TotalSaved),
		slog.Int("total_published", result.TotalPublished),
		slog.Int("total_skipped", result.TotalSkipped),
		slog.Int("total_failed", result.TotalFailed),
		slog.Int64("duration_ms", result.DurationMs),
	)

	return result
}

// buildFetchConfigs constructs the slice of FetchConfig from the app config and
// the requested categories.
func buildFetchConfigs(cfg *config.Config, categories []models.Category) []models.FetchConfig {
	var cfgs []models.FetchConfig

	// RSS feeds
	for _, feedURL := range cfg.RSSFeeds {
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

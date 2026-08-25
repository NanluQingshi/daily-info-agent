// Package api provides REST API handlers for article management.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/scheduler"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
	"github.com/user/daily-info-agent/pkg/ratelimit"
)

// Handler holds dependencies for all REST API endpoints.
type Handler struct {
	store     store.ArticleStore
	scheduler *scheduler.Scheduler
	publisher *publisher.Client // may be nil
	cfg       *config.Config
	logger    *slog.Logger

	limiter *ratelimit.Limiter // nil when API rate limiting is disabled

	pipelineMu      sync.Mutex
	pipelineRunning bool
	activeRunID     string
	runsMu          sync.RWMutex
	runs            map[string]models.RunResult // runID -> result (populated when a run finishes)
}

// parseCategories parses the optional comma-separated categories query value.
// It returns nil when the value is absent so the caller can use configured
// defaults. Unknown categories are rejected before a pipeline is started.
func parseCategories(raw string) ([]models.Category, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	cats := make([]models.Category, 0, len(parts))
	seen := make(map[models.Category]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		cat := models.Category(p)
		if !cat.IsValid() {
			valid := make([]string, 0, len(models.AllCategories))
			for _, known := range models.AllCategories {
				valid = append(valid, string(known))
			}
			return nil, fmt.Errorf("invalid category %q (valid: %s)", p, strings.Join(valid, ", "))
		}
		if _, ok := seen[cat]; ok {
			continue
		}
		seen[cat] = struct{}{}
		cats = append(cats, cat)
	}
	if len(cats) == 0 {
		return nil, errors.New("categories must include at least one valid category")
	}
	return cats, nil
}

// New creates a Handler.
func New(
	st store.ArticleStore,
	sched *scheduler.Scheduler,
	pub *publisher.Client,
	cfg *config.Config,
	logger *slog.Logger,
) *Handler {
	h := &Handler{
		store:     st,
		scheduler: sched,
		publisher: pub,
		cfg:       cfg,
		logger:    logger,
		runs:      make(map[string]models.RunResult),
	}
	// Optional per-IP rate limit for the management API.
	if cfg.APIRateLimitPerMin > 0 {
		h.limiter = ratelimit.New(cfg.APIRateLimitPerMin, time.Minute/time.Duration(cfg.APIRateLimitPerMin))
	}
	return h
}

// rateLimited wraps a handler with per-IP throttling when enabled.
func (h *Handler) rateLimited(next echo.HandlerFunc) echo.HandlerFunc {
	if h.limiter == nil {
		return next
	}
	return func(c echo.Context) error {
		if !h.limiter.Allow(c.RealIP()) {
			return errJSON(c, http.StatusTooManyRequests, "rate_limited", "too many requests, please slow down")
		}
		return next(c)
	}
}

// requireStore returns an error response if the database is not configured.
// It writes the JSON error body to the response AND returns a non-nil error
// so the caller stops processing (c.JSON alone may return nil on success).
func (h *Handler) requireStore(c echo.Context) error {
	if h.store == nil {
		errJSON(c, http.StatusServiceUnavailable, "db_disabled",
			"Database not configured. Set DATABASE_DSN to enable article management and statistics.")
		return errors.New("store: database not configured")
	}
	return nil
}

// Register attaches all article management routes to the given Echo group.
func (h *Handler) Register(g *echo.Group) {
	g.GET("/articles", h.rateLimited(h.ListArticles))
	g.GET("/articles/:id", h.rateLimited(h.GetArticle))
	g.POST("/articles/:id/publish", h.rateLimited(h.PublishArticle))
	g.POST("/articles/:id/retry", h.rateLimited(h.RetryArticle))
	g.DELETE("/articles/:id", h.rateLimited(h.DeleteArticle))
	g.PATCH("/articles/tags", h.rateLimited(h.BatchUpdateTags))
	g.POST("/fetch", h.rateLimited(h.TriggerFetch))
	g.GET("/fetch/stream", h.rateLimited(h.StreamFetch))
	g.GET("/fetch/:run_id", h.rateLimited(h.GetFetchStatus))
	g.PATCH("/articles/:id/flags", h.rateLimited(h.UpdateArticleFlags))
	g.GET("/stats", h.rateLimited(h.GetStats))
}

// ListArticles handles GET /api/articles
func (h *Handler) ListArticles(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	f := models.ArticleFilter{}

	if v := c.QueryParam("category"); v != "" {
		cat := models.Category(v)
		f.Category = &cat
	}
	if v := c.QueryParam("status"); v != "" {
		f.Status = &v
	}
	if v := c.QueryParam("date_from"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "date_from must be YYYY-MM-DD")
		}
		f.DateFrom = &t
	}
	if v := c.QueryParam("date_to"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "date_to must be YYYY-MM-DD")
		}
		end := t.Add(24*time.Hour - time.Second)
		f.DateTo = &end
	}
	if v := c.QueryParam("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "page must be a positive integer")
		}
		f.Page = n
	}
	if v := c.QueryParam("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "page_size must be between 1 and 100")
		}
		f.PageSize = n
	}
	f.Query = c.QueryParam("q")

	if v := c.QueryParam("bookmarked"); v != "" {
		b, err := parseBoolFilter(v)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "bookmarked must be true or false")
		}
		f.Bookmarked = b
	}
	if v := c.QueryParam("unread"); v != "" {
		b, err := parseBoolFilter(v)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "unread must be true or false")
		}
		f.Unread = b
	}

	articles, total, err := h.store.ListArticles(c.Request().Context(), f)
	if err != nil {
		h.logger.Error("list articles failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to list articles")
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := (total + pageSize - 1) / pageSize

	return c.JSON(http.StatusOK, models.ArticleListResponse{
		Articles:   articles,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// GetArticle handles GET /api/articles/:id
func (h *Handler) GetArticle(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := parseID(c)
	if err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
	}

	article, err := h.store.GetArticle(c.Request().Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "article not found")
	}
	if err != nil {
		h.logger.Error("get article failed", slog.Int64("id", id), slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to get article")
	}

	return c.JSON(http.StatusOK, article)
}

// PublishArticle handles POST /api/articles/:id/publish
func (h *Handler) PublishArticle(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	if h.publisher == nil {
		return errJSON(c, http.StatusServiceUnavailable, "publisher_disabled",
			"Java API publishing is not configured (WEBSITE_API_BASE_URL / WEBSITE_API_TOKEN not set)")
	}

	id, err := parseID(c)
	if err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
	}

	row, err := h.store.GetArticle(c.Request().Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "article not found")
	}
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to get article")
	}

	article := rowToProcessedArticle(row)
	result := h.publisher.Publish(c.Request().Context(), article, row.RunID)

	if result.Outcome == publisher.OutcomePublished {
		_ = h.store.MarkPublished(c.Request().Context(), id, result.RemoteID)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"published":   true,
			"external_id": result.RemoteID,
		})
	}

	_ = h.store.MarkFailed(c.Request().Context(), id)
	msg := "publish failed"
	if result.Err != nil {
		msg = result.Err.Error()
	}
	return errJSON(c, http.StatusBadGateway, string(result.Outcome), msg)
}

// RetryArticle handles POST /api/articles/:id/retry — resets a failed article
// to 'pending' so it can be published again.
func (h *Handler) RetryArticle(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := parseID(c)
	if err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
	}

	row, err := h.store.GetArticle(c.Request().Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "article not found")
	}
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to get article")
	}
	if row.Status != "failed" {
		return errJSON(c, http.StatusConflict, "invalid_status",
			"only articles with status 'failed' can be retried")
	}

	if err := h.store.MarkPending(c.Request().Context(), id); err != nil {
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to reset article status")
	}

	return c.JSON(http.StatusOK, map[string]any{"retried": true, "id": id})
}

// DeleteArticle handles DELETE /api/articles/:id
func (h *Handler) DeleteArticle(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := parseID(c)
	if err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
	}

	if err := h.store.DeleteArticle(c.Request().Context(), id); errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "article not found")
	} else if err != nil {
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to delete article")
	}

	return c.NoContent(http.StatusNoContent)
}

// BatchUpdateTags handles PATCH /api/articles/tags — overwrites the tags of
// multiple articles in one call.
func (h *Handler) BatchUpdateTags(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	var req models.BatchTagsRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "validation_error", "invalid request body")
	}
	if len(req.ArticleIDs) == 0 {
		return errJSON(c, http.StatusBadRequest, "validation_error", "article_ids must not be empty")
	}
	// Enforce the same tag count limit used by the AI processor (max 10 tags).
	if len(req.Tags) > 10 {
		return errJSON(c, http.StatusBadRequest, "validation_error", "tags must not exceed 10 items")
	}

	updated, err := h.store.UpdateArticlesTags(c.Request().Context(), req.ArticleIDs, req.Tags)
	if err != nil {
		h.logger.Error("batch update tags failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to update tags")
	}

	return c.JSON(http.StatusOK, models.BatchTagsResponse{Updated: updated})
}

// TriggerFetch handles POST /api/fetch — starts a pipeline run asynchronously.
func (h *Handler) TriggerFetch(c echo.Context) error {
	cats, err := parseCategories(c.QueryParam("categories"))
	if err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_param", err.Error())
	}
	if cats == nil {
		cats = h.cfg.DefaultCategories
	}

	h.pipelineMu.Lock()
	if h.pipelineRunning {
		h.pipelineMu.Unlock()
		return c.JSON(http.StatusConflict, models.FetchTriggerResponse{
			Triggered: false,
			Message:   "pipeline already running",
		})
	}
	runID := uuid.New().String()
	h.pipelineRunning = true
	h.activeRunID = runID
	h.pipelineMu.Unlock()

	go func() {
		defer h.finishPipeline(runID)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		result := h.scheduler.RunWithProgressAndID(ctx, cats, nil, runID)
		h.runsMu.Lock()
		h.runs[runID] = result
		h.runsMu.Unlock()
		h.logger.Info("triggered fetch complete",
			slog.String("run_id", result.RunID),
			slog.Int("fetched", result.TotalFetched),
			slog.Int("saved", result.TotalSaved),
			slog.Int("published", result.TotalPublished),
		)
	}()

	return c.JSON(http.StatusAccepted, models.FetchTriggerResponse{
		RunID:     runID,
		Triggered: true,
		Message:   "pipeline started",
	})
}

// StreamFetch handles GET /api/fetch/stream — starts a pipeline run and streams
// progress as SSE events until the run finishes or the client disconnects.
func (h *Handler) StreamFetch(c echo.Context) error {
	cats, err := parseCategories(c.QueryParam("categories"))
	if err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_param", err.Error())
	}
	if cats == nil {
		cats = h.cfg.DefaultCategories
	}

	w := c.Response().Writer
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return echo.ErrInternalServerError
	}

	writeEvent := func(e models.ProgressEvent) bool {
		data, _ := json.Marshal(e)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	h.pipelineMu.Lock()
	if h.pipelineRunning {
		h.pipelineMu.Unlock()
		writeEvent(models.ProgressEvent{Stage: "error", Status: "error", Message: "pipeline already running"})
		return nil
	}
	runID := uuid.New().String()
	h.pipelineRunning = true
	h.activeRunID = runID
	h.pipelineMu.Unlock()

	eventCh := make(chan models.ProgressEvent, 8)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Minute)
	defer cancel()

	go func() {
		defer close(eventCh)
		defer h.finishPipeline(runID)
		result := h.scheduler.RunWithProgressAndID(ctx, cats, func(e models.ProgressEvent) {
			select {
			case eventCh <- e:
			case <-ctx.Done():
			}
		}, runID)
		h.runsMu.Lock()
		h.runs[runID] = result
		h.runsMu.Unlock()
	}()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return nil
			}
			if !writeEvent(event) {
				return nil // client disconnected
			}
		case <-c.Request().Context().Done():
			return nil
		}
	}
}

// finishPipeline clears the active pipeline only when runID still owns the
// slot. The identity check prevents a delayed worker from clearing a newer run.
func (h *Handler) finishPipeline(runID string) {
	h.pipelineMu.Lock()
	defer h.pipelineMu.Unlock()
	if h.activeRunID == runID {
		h.pipelineRunning = false
		h.activeRunID = ""
	}
}

// GetFetchStatus handles GET /api/fetch/:run_id — returns the status of a
// triggered pipeline run. While the run is in flight, status is "running"
// (held in memory). Once it finishes, the in-memory result is returned;
// after a restart, the persisted run_logs row is the source of truth.
func (h *Handler) GetFetchStatus(c echo.Context) error {
	runID := c.Param("run_id")
	if runID == "" {
		return errJSON(c, http.StatusBadRequest, "invalid_id", "run_id is required")
	}

	// In-flight or recently-finished (in memory).
	h.runsMu.RLock()
	result, ok := h.runs[runID]
	h.runsMu.RUnlock()
	if ok {
		return c.JSON(http.StatusOK, fetchStatusFromResult(result))
	}

	// Still running (no in-memory result yet, and this is the active run)?
	h.pipelineMu.Lock()
	running := h.pipelineRunning && h.activeRunID == runID
	h.pipelineMu.Unlock()
	if running {
		return c.JSON(http.StatusOK, map[string]any{
			"run_id": runID,
			"status": "running",
		})
	}

	// Fall back to the persisted run log.
	if h.store == nil {
		return errJSON(c, http.StatusNotFound, "not_found", "no run record found and database is disabled")
	}
	log, err := h.store.GetRunLog(c.Request().Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "run not found")
	}
	if err != nil {
		h.logger.Error("get run log failed", slog.String("run_id", runID), slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to get run status")
	}

	status := "done"
	if log.FatalError != "" {
		status = "error"
	}
	return c.JSON(http.StatusOK, map[string]any{
		"run_id":      log.RunID,
		"status":      status,
		"fetched":     log.TotalFetched,
		"processed":   log.TotalProcessed,
		"saved":       log.TotalSaved,
		"published":   log.TotalPublished,
		"skipped":     log.TotalSkipped,
		"failed":      log.TotalFailed,
		"duration_ms": log.DurationMs,
		"fatal_error": log.FatalError,
		"started_at":  log.StartedAt.UTC().Format(time.RFC3339),
		"finished_at": log.FinishedAt.UTC().Format(time.RFC3339),
	})
}

// fetchStatusFromResult adapts a scheduler RunResult into the status payload.
func fetchStatusFromResult(r models.RunResult) map[string]any {
	status := "done"
	if r.FatalError != nil {
		status = "error"
	}
	return map[string]any{
		"run_id":      r.RunID,
		"status":      status,
		"fetched":     r.TotalFetched,
		"processed":   r.TotalProcessed,
		"saved":       r.TotalSaved,
		"published":   r.TotalPublished,
		"skipped":     r.TotalSkipped,
		"failed":      r.TotalFailed,
		"duration_ms": r.DurationMs,
		"fatal_error": func() string {
			if r.FatalError != nil {
				return r.FatalError.Error()
			}
			return ""
		}(),
	}
}

// GetStats handles GET /api/stats?since=YYYY-MM-DD (default: 90 days ago)
func (h *Handler) GetStats(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	since := time.Now().UTC().AddDate(0, -3, 0) // default: 90 days
	if v := c.QueryParam("since"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "since must be YYYY-MM-DD")
		}
		since = t
	}

	stats, err := h.store.GetStats(c.Request().Context(), since)
	if err != nil {
		h.logger.Error("get stats failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to get stats")
	}

	return c.JSON(http.StatusOK, stats)
}

// rowToProcessedArticle converts a flat ArticleRow back to a ProcessedArticle
// so the existing publisher.Client.Publish method can be reused unchanged.
func rowToProcessedArticle(row models.ArticleRow) models.ProcessedArticle {
	raw := &models.RawItem{
		URL:          row.SourceURL,
		SourceDomain: row.SourceDomain,
		SourceType:   models.SourceType(row.SourceType),
		Title:        row.Title,
		Description:  row.Description,
		Content:      row.Content,
		Language:     row.Language,
		FetchedAt:    row.FetchedAt,
	}
	if row.PublishedAt != nil {
		raw.PublishedAt = *row.PublishedAt
	}
	return models.ProcessedArticle{
		Raw:              raw,
		Category:         row.Category,
		Summary:          row.Summary,
		CredibilityScore: row.CredibilityScore,
		Tags:             row.Tags,
		DetectedLanguage: row.DetectedLanguage,
		AgentVersion:     row.AgentVersion,
		RunID:            row.RunID,
		Verification: models.VerificationResult{
			Pass:       row.VerificationPass,
			SkipReason: row.SkipReason,
			DomainHit:  row.DomainHit,
		},
	}
}

func parseID(c echo.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

func errJSON(c echo.Context, status int, code, message string) error {
	return c.JSON(status, map[string]string{"error": code, "message": message})
}

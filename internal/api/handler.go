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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/scheduler"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

// Handler holds dependencies for all REST API endpoints.
type Handler struct {
	store     store.ArticleStore
	scheduler *scheduler.Scheduler
	publisher *publisher.Client // may be nil
	cfg       *config.Config
	logger    *slog.Logger

	pipelineMu      sync.Mutex
	pipelineRunning bool
}

// New creates a Handler.
func New(
	st store.ArticleStore,
	sched *scheduler.Scheduler,
	pub *publisher.Client,
	cfg *config.Config,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		store:     st,
		scheduler: sched,
		publisher: pub,
		cfg:       cfg,
		logger:    logger,
	}
}

// Register attaches all article management routes to the given Echo group.
func (h *Handler) Register(g *echo.Group) {
	g.GET("/articles", h.ListArticles)
	g.GET("/articles/:id", h.GetArticle)
	g.POST("/articles/:id/publish", h.PublishArticle)
	g.DELETE("/articles/:id", h.DeleteArticle)
	g.POST("/fetch", h.TriggerFetch)
	g.GET("/fetch/stream", h.StreamFetch)
	g.GET("/stats", h.GetStats)
}

// ListArticles handles GET /api/articles
func (h *Handler) ListArticles(c echo.Context) error {
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

// DeleteArticle handles DELETE /api/articles/:id
func (h *Handler) DeleteArticle(c echo.Context) error {
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

// TriggerFetch handles POST /api/fetch — starts a pipeline run asynchronously.
func (h *Handler) TriggerFetch(c echo.Context) error {
	h.pipelineMu.Lock()
	if h.pipelineRunning {
		h.pipelineMu.Unlock()
		return c.JSON(http.StatusConflict, models.FetchTriggerResponse{
			Triggered: false,
			Message:   "pipeline already running",
		})
	}
	h.pipelineRunning = true
	h.pipelineMu.Unlock()

	runID := uuid.New().String()

	go func() {
		defer func() {
			h.pipelineMu.Lock()
			h.pipelineRunning = false
			h.pipelineMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		result := h.scheduler.Run(ctx)
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
	h.pipelineRunning = true
	h.pipelineMu.Unlock()

	defer func() {
		h.pipelineMu.Lock()
		h.pipelineRunning = false
		h.pipelineMu.Unlock()
	}()

	eventCh := make(chan models.ProgressEvent, 8)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Minute)
	defer cancel()

	go func() {
		defer close(eventCh)
		h.scheduler.RunWithProgress(ctx, h.cfg.DefaultCategories, func(e models.ProgressEvent) {
			select {
			case eventCh <- e:
			case <-ctx.Done():
			}
		})
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

// GetStats handles GET /api/stats?since=YYYY-MM-DD (default: 30 days ago)
func (h *Handler) GetStats(c echo.Context) error {
	since := time.Now().UTC().AddDate(0, -1, 0)
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

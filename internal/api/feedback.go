package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/metrics"
	"github.com/user/daily-info-agent/pkg/models"
)

// feedbackKindNames are the aspects users can rate (see migration 007 CHECK).
var feedbackKindNames = map[string]bool{"summary": true, "category": true}

// feedbackRequest is the body of POST /api/articles/:id/feedback.
type feedbackRequest struct {
	Kind   string `json:"kind"`   // "summary" | "category"
	Rating int16  `json:"rating"` // 1 = 👍, -1 = 👎
}

// SubmitFeedback handles POST /api/articles/:id/feedback (#61).
// Upsert semantics: one row per (article, kind); repeat clicks overwrite —
// idempotent, latest rating wins. Process-lifetime counters are exported
// via /metrics (dia_feedback_up / dia_feedback_down).
func (h *Handler) SubmitFeedback(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "id must be a positive integer")
	}

	var req feedbackRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_body", "failed to parse request body")
	}
	if !feedbackKindNames[req.Kind] {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "kind must be summary or category")
	}
	if req.Rating != 1 && req.Rating != -1 {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "rating must be 1 (up) or -1 (down)")
	}

	row, err := h.store.UpsertArticleFeedback(c.Request().Context(), id, req.Kind, req.Rating)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errJSON(c, http.StatusNotFound, "not_found", "article not found")
		}
		h.logger.Error("upsert feedback failed",
			slog.Int64("article_id", id),
			slog.String("error", err.Error()),
		)
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to save feedback")
	}

	if req.Rating == 1 {
		metrics.App.FeedbackUp.Add(1)
	} else {
		metrics.App.FeedbackDown.Add(1)
	}

	return c.JSON(http.StatusOK, row)
}

// GetFeedback handles GET /api/articles/:id/feedback — the current rating
// state of one article so the UI can echo back 👍/👎 selections.
func (h *Handler) GetFeedback(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "id must be a positive integer")
	}

	rows, err := h.store.GetArticleFeedback(c.Request().Context(), id)
	if err != nil {
		h.logger.Error("get feedback failed",
			slog.Int64("article_id", id),
			slog.String("error", err.Error()),
		)
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to get feedback")
	}
	if rows == nil {
		rows = []models.ArticleFeedbackRow{}
	}

	type feedbackResponse struct {
		Feedback []models.ArticleFeedbackRow `json:"feedback"`
	}
	return c.JSON(http.StatusOK, feedbackResponse{Feedback: rows})
}

// GetFeedbackStats handles GET /api/feedback/stats — aggregated up/down
// counts per kind, the export channel for prompt tuning (#61 optional).
func (h *Handler) GetFeedbackStats(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	stats, err := h.store.FeedbackStats(c.Request().Context())
	if err != nil {
		h.logger.Error("feedback stats failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to aggregate feedback")
	}
	if stats == nil {
		stats = []models.FeedbackStat{}
	}

	return c.JSON(http.StatusOK, models.FeedbackStatsResponse{Stats: stats})
}

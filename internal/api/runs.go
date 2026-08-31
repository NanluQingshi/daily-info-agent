package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/user/daily-info-agent/pkg/models"
)

// RunListResponse is the payload of GET /api/runs.
type RunListResponse struct {
	Runs []models.RunLogRow `json:"runs"`
}

// GetRuns handles GET /api/runs?limit=N (default 20, max 100) — the run
// history panel's data source. An empty database yields an empty list, not
// an error, so the panel can degrade gracefully.
func (h *Handler) GetRuns(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	limit := 20
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return errJSON(c, http.StatusBadRequest, "invalid_param", "limit must be an integer in [1, 100]")
		}
		limit = n
	}

	runs, err := h.store.ListRunLogs(c.Request().Context(), limit)
	if err != nil {
		h.logger.Error("list run logs failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to list runs")
	}
	if runs == nil {
		runs = []models.RunLogRow{}
	}

	return c.JSON(http.StatusOK, RunListResponse{Runs: runs})
}

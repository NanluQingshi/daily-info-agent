package api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/models"
)

// SourceListResponse is the payload of GET /api/sources.
type SourceListResponse struct {
	Sources []models.SourceRow `json:"sources"`
}

// sourcePayload is the request body of POST /api/sources and
// PATCH /api/sources/:id.
type sourcePayload struct {
	URL     *string `json:"url"`
	Enabled *bool   `json:"enabled"`
}

// ListSources handles GET /api/sources — every managed source, disabled ones
// included, oldest first. An empty table yields an empty list (the server is
// still running on the static RSS_FEEDS list in that case).
func (h *Handler) ListSources(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	sources, err := h.store.ListSources(c.Request().Context())
	if err != nil {
		h.logger.Error("list sources failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to list sources")
	}
	if sources == nil {
		sources = []models.SourceRow{}
	}

	return c.JSON(http.StatusOK, SourceListResponse{Sources: sources})
}

// AddSource handles POST /api/sources {url}. The URL must be absolute
// http(s); adding an existing URL yields 409. Changes apply on the next run
// without a restart.
func (h *Handler) AddSource(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	var p sourcePayload
	if err := c.Bind(&p); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_body", "body must be JSON")
	}
	raw := ""
	if p.URL != nil {
		raw = strings.TrimSpace(*p.URL)
	}
	if raw == "" {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "url must be an absolute http(s) URL")
	}

	row, err := h.store.AddSource(c.Request().Context(), raw)
	if errors.Is(err, store.ErrConflict) {
		return errJSON(c, http.StatusConflict, "conflict", "source URL already exists")
	}
	if err != nil {
		h.logger.Error("add source failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to add source")
	}

	return c.JSON(http.StatusCreated, row)
}

// SetSourceEnabled handles PATCH /api/sources/:id {enabled} — pause or
// resume fetching one source without losing its history or health data.
func (h *Handler) SetSourceEnabled(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "id must be a positive integer")
	}

	var p sourcePayload
	if err := c.Bind(&p); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_body", "body must be JSON")
	}
	if p.Enabled == nil {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "enabled is required")
	}

	row, err := h.store.SetSourceEnabled(c.Request().Context(), id, *p.Enabled)
	if errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "source not found")
	}
	if err != nil {
		h.logger.Error("set source enabled failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to update source")
	}

	return c.JSON(http.StatusOK, row)
}

// RemoveSource handles DELETE /api/sources/:id. Stored articles and their
// per-source health history are untouched; only future fetching stops.
func (h *Handler) RemoveSource(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "id must be a positive integer")
	}

	if err := h.store.RemoveSource(c.Request().Context(), id); errors.Is(err, store.ErrNotFound) {
		return errJSON(c, http.StatusNotFound, "not_found", "source not found")
	} else if err != nil {
		h.logger.Error("remove source failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to remove source")
	}

	return c.NoContent(http.StatusNoContent)
}

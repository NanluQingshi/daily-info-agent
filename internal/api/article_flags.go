package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/user/daily-info-agent/internal/store"
)

// articleFlagsRequest is the body of PATCH /api/articles/:id/flags.
// Both fields are optional; omitted fields keep their current value.
// Setting read=true stamps read_at=NOW(), read=false clears it — the call
// is idempotent and doubles as an undo.
type articleFlagsRequest struct {
	Bookmarked *bool `json:"bookmarked"`
	Read       *bool `json:"read"`
}

// UpdateArticleFlags handles PATCH /api/articles/:id/flags — bookmark star
// and read/unread tracking (#59). Returns the updated article so the client
// can re-render state without a refetch.
func (h *Handler) UpdateArticleFlags(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "id must be a positive integer")
	}

	var req articleFlagsRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid_body", "failed to parse request body")
	}
	if req.Bookmarked == nil && req.Read == nil {
		return errJSON(c, http.StatusBadRequest, "invalid_body", "at least one of bookmarked or read is required")
	}

	article, err := h.store.SetArticleFlags(c.Request().Context(), id, req.Bookmarked, req.Read)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return errJSON(c, http.StatusNotFound, "not_found", "article not found")
		}
		h.logger.Error("set article flags failed",
			slog.Int64("id", id),
			slog.String("error", err.Error()),
		)
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to update flags")
	}

	return c.JSON(http.StatusOK, article)
}

// parseBoolFilter parses "true"/"false" query params; empty string → nil.
func parseBoolFilter(v string) (*bool, error) {
	if v == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

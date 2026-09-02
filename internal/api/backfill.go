package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/user/daily-info-agent/pkg/models"
)

// backfillBatchSize is how many pages one call extracts per batch; batches
// run back-to-back until the per-call cap is reached, mirroring the fetch
// pipeline's FULLTEXT_MAX_ITEMS semantics.
const (
	backfillBatchSize  = 20
	backfillMaxPerCall = 200
)

// backfillResponse reports what one POST /api/articles/backfill-content did.
type backfillResponse struct {
	Processed int `json:"processed"` // articles attempted
	Updated   int `json:"updated"`   // articles whose content_text was filled
	Failed    int `json:"failed"`    // extraction failures (original data untouched)
	Remaining int `json:"remaining"` // articles still missing content_text after this call
}

// BackfillContent handles POST /api/articles/backfill-content?limit=N (#56):
// batch-extracts original-page full text for stored articles whose
// content_text is empty (i.e. saved before extraction shipped). Single-page
// failures degrade to the existing summary — nothing is overwritten.
func (h *Handler) BackfillContent(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	limit := backfillMaxPerCall
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > backfillMaxPerCall {
			return errJSON(c, http.StatusBadRequest, "invalid_param",
				"limit must be an integer in [1, 200]")
		}
		limit = n
	}

	if h.extractor == nil {
		return errJSON(c, http.StatusServiceUnavailable, "fulltext_disabled",
			"Full-text extraction is disabled. Set FULLTEXT_ENABLED=true and restart.")
	}

	refs, remaining, err := h.store.ArticlesMissingContentText(c.Request().Context(), limit)
	if err != nil {
		h.logger.Error("list articles missing content failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to list articles")
	}

	resp := backfillResponse{Remaining: remaining}
	for start := 0; start < len(refs); start += backfillBatchSize {
		end := start + backfillBatchSize
		if end > len(refs) {
			end = len(refs)
		}
		batch := refs[start:end]

		items := make([]models.RawItem, len(batch))
		for i, r := range batch {
			items[i] = models.RawItem{URL: r.SourceURL} // empty Content → extraction candidate
		}

		// Enrich fills items[i].ContentText in place; per-page failures keep
		// it empty and are counted, never aborting the batch (#56).
		_ = h.extractor.Enrich(c.Request().Context(), items)

		for i, it := range items {
			resp.Processed++
			if it.ContentText == "" {
				resp.Failed++
				continue
			}
			if err := h.store.UpdateArticleContentText(c.Request().Context(), batch[i].ID, it.ContentText); err != nil {
				h.logger.Error("update content_text failed",
					slog.Int64("id", batch[i].ID),
					slog.String("error", err.Error()),
				)
				resp.Failed++
				continue
			}
			resp.Updated++
			resp.Remaining--
		}
	}

	h.logger.Info("content backfill batch complete",
		slog.Int("processed", resp.Processed),
		slog.Int("updated", resp.Updated),
		slog.Int("failed", resp.Failed),
		slog.Int("remaining", resp.Remaining),
	)

	return c.JSON(http.StatusOK, resp)
}

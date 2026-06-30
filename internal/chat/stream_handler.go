package chat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/internal/agent"
	"github.com/user/daily-info-agent/pkg/models"
)

// HandleStream is the Echo HandlerFunc registered at POST /api/chat/stream.
// It validates the request then streams SSE events back to the client.
func (h *Handler) HandleStream(c echo.Context) error {
	if !h.checkAuth(c) {
		return h.unauthorized(c)
	}

	var req models.ChatRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "validation_error",
			Message: "invalid request body",
		})
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "validation_error",
			Message: "message is required",
		})
	}
	if len([]rune(req.Message)) > maxMessageLen {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "message_too_long",
			Message: "message must not exceed 500 characters",
		})
	}

	// ── SSE setup ─────────────────────────────────────────────────────────────
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if present
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.Writer.(http.Flusher)

	sendEvent := func(ev agent.StreamEvent) {
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	// ── Run agent ─────────────────────────────────────────────────────────────
	ctx := c.Request().Context()
	h.logger.Info("stream request received",
		"session_id", req.SessionID,
		"message_preview", truncate(req.Message, 80),
	)

	h.runner.RunStream(ctx, req.SessionID, req.Message, sendEvent)
	return nil
}

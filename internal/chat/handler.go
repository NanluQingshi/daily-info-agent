// Package chat implements the Echo HTTP handler for POST /api/chat.
package chat

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/internal/agent"
	"github.com/user/daily-info-agent/pkg/models"
)

const maxMessageLen = 500

// Handler implements the Echo handler for POST /api/chat.
type Handler struct {
	runner *agent.Runner
	logger *slog.Logger
}

// New creates a Handler backed by the given agent Runner.
func New(runner *agent.Runner, logger *slog.Logger) *Handler {
	return &Handler{runner: runner, logger: logger}
}

// Handle is the Echo HandlerFunc registered at POST /api/chat.
func (h *Handler) Handle(c echo.Context) error {
	reqStart := time.Now()

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

	ctx := c.Request().Context()

	h.logger.Info("chat request received",
		slog.String("session_id", req.SessionID),
		slog.String("message_preview", truncate(req.Message, 80)),
	)

	result, err := h.runner.Run(ctx, req.SessionID, req.Message)
	if err != nil {
		h.logger.Error("agent run failed", slog.String("error", err.Error()))
		return c.JSON(http.StatusInternalServerError, models.ChatErrorResponse{
			Error:   "agent_error",
			Message: "failed to process your message, please try again",
		})
	}

	sources := make([]models.ChatSource, 0, len(result.Sources))
	for _, item := range result.Sources {
		sources = append(sources, models.ChatSource{
			URL:          item.URL,
			Title:        item.Title,
			SourceDomain: item.SourceDomain,
		})
	}

	resp := models.ChatResponse{
		SessionID:  result.SessionID,
		Reply:      result.Reply,
		Sources:    sources,
		ToolCalled: result.ToolCalled,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		LatencyMs:  time.Since(reqStart).Milliseconds(),
	}

	h.logger.Info("chat response ready",
		slog.String("session_id", result.SessionID),
		slog.Bool("tool_called", result.ToolCalled),
		slog.Int("sources", len(sources)),
		slog.Int64("latency_ms", resp.LatencyMs),
	)

	return c.JSON(http.StatusOK, resp)
}

// HandleDeleteSession is the Echo HandlerFunc for DELETE /api/sessions/:id.
// It removes the session from the in-memory store. The call is idempotent.
func (h *Handler) HandleDeleteSession(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "validation_error",
			Message: "session id is required",
		})
	}
	h.runner.DeleteSession(id)
	return c.NoContent(http.StatusNoContent)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

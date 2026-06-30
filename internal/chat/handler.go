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
	runner   *agent.Runner
	logger   *slog.Logger
	apiToken string // when non-empty, requests must carry it in a header
}

// New creates a Handler backed by the given agent Runner.
// apiToken is optional; when non-empty it gates /api/chat and /api/chat/stream.
func New(runner *agent.Runner, apiToken string, logger *slog.Logger) *Handler {
	return &Handler{runner: runner, apiToken: apiToken, logger: logger}
}

// checkAuth returns true when the request is authorized to call the chat API.
// When no token is configured, all requests pass. Otherwise the request must
// carry the token in "X-Api-Token" or "Authorization: Bearer <token>".
func (h *Handler) checkAuth(c echo.Context) bool {
	if h.apiToken == "" {
		return true
	}
	if c.Request().Header.Get("X-Api-Token") == h.apiToken {
		return true
	}
	if bearer := c.Request().Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
		if strings.TrimPrefix(bearer, "Bearer ") == h.apiToken {
			return true
		}
	}
	return false
}

func (h *Handler) unauthorized(c echo.Context) error {
	return c.JSON(http.StatusUnauthorized, models.ChatErrorResponse{
		Error:   "unauthorized",
		Message: "missing or invalid API token",
	})
}

// Handle is the Echo HandlerFunc registered at POST /api/chat.
func (h *Handler) Handle(c echo.Context) error {
	if !h.checkAuth(c) {
		return h.unauthorized(c)
	}

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

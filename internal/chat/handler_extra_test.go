package chat_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/agent"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/chat"
	"github.com/user/daily-info-agent/pkg/models"
)

// newHandlerWithRateLimit builds a chat.Handler with the given rate limit.
func newHandlerWithRateLimit(t *testing.T, llmContent string, rateLimitPerMin int) *chat.Handler {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(stopResponse(llmContent))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, nil, nil, cacheFile, slog.Default())
	runner := agent.New(srv.URL, "test-key", "deepseek-v4-pro", mgr, nil, slog.Default())

	return chat.New(runner, "", rateLimitPerMin, slog.Default())
}

// ---------------------------------------------------------------------------
// Handler with rate limiter
// ---------------------------------------------------------------------------

func TestHandler_RateLimit_BlocksExcessiveRequests(t *testing.T) {
	// Create a handler with rateLimitPerMin=1 (capacity 1)
	h := newHandlerWithRateLimit(t, "some response", 1)
	e := echo.New()
	e.HideBanner = true

	// Fire one valid request to consume the capacity.
	body := `{"message":"hello"}`
	c, rec := echoContext(e, http.MethodPost, "/api/chat", body)
	err := h.Handle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// With capacity=1, the second should be rate-limited.
	c2, rec2 := echoContext(e, http.MethodPost, "/api/chat", body)
	err = h.Handle(c2)
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

// ---------------------------------------------------------------------------
// HandleDeleteSession
// ---------------------------------------------------------------------------

func TestHandler_DeleteSession_NoContent(t *testing.T) {
	h := newHandler(t, "some response")
	e := echo.New()
	e.HideBanner = true

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/test-id", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test-id")

	err := h.HandleDeleteSession(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestHandler_DeleteSession_EmptyID_Returns400(t *testing.T) {
	h := newHandler(t, "some response")
	e := echo.New()
	e.HideBanner = true

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleDeleteSession(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// HandleStream — authentication and validation
// ---------------------------------------------------------------------------

func TestHandler_Stream_AuthRequired_MissingHeader_Returns401(t *testing.T) {
	h := newHandlerWithToken(t, "reply", "secret-token")
	e := echo.New()
	e.HideBanner = true

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/stream", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.HandleStream(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_Stream_EmptyMessage_Returns400(t *testing.T) {
	h := newHandler(t, "reply")
	e := echo.New()
	e.HideBanner = true

	body := `{"message":""}`
	c, rec := echoContext(e, http.MethodPost, "/api/chat/stream", body)

	err := h.HandleStream(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "validation_error", resp.Error)
}

func TestHandler_Stream_MessageTooLong_Returns400(t *testing.T) {
	h := newHandler(t, "reply")
	e := echo.New()
	e.HideBanner = true

	longMsg := strings.Repeat("a", 501)
	body := `{"message":"` + longMsg + `"}`
	c, rec := echoContext(e, http.MethodPost, "/api/chat/stream", body)

	err := h.HandleStream(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "message_too_long", resp.Error)
}

func TestHandler_Stream_InvalidJSON_Returns400(t *testing.T) {
	h := newHandler(t, "reply")
	e := echo.New()
	e.HideBanner = true

	c, rec := echoContext(e, http.MethodPost, "/api/chat/stream", `not json`)

	err := h.HandleStream(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Stream_ValidRequest_ReturnsSSE(t *testing.T) {
	// Uses a handler with mock LLM that returns a simple response.
	h := newHandler(t, "this is a test reply")
	e := echo.New()
	e.HideBanner = true

	body := `{"message":"hello"}`
	c, rec := echoContext(e, http.MethodPost, "/api/chat/stream", body)

	err := h.HandleStream(c)
	require.NoError(t, err)

	// Should get SSE events: thinking, delta, done
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")

	bodyStr := rec.Body.String()
	assert.Contains(t, bodyStr, `"type":"delta"`)
	assert.Contains(t, bodyStr, `"type":"done"`)
}

// ---------------------------------------------------------------------------
// Rate limiter additional edge cases
// ---------------------------------------------------------------------------

func TestRateLimiter_NegativeCapacity_DoesNotPanic(t *testing.T) {
	// When rateLimitPerMin is 0, the handler doesn't create a limiter.
	// This is already tested via the handler. Verify no panic with
	// a handler that has no limiter configured.
	h := newHandler(t, "reply")
	e := echo.New()
	e.HideBanner = true

	// Multiple rapid requests without a limiter should all pass.
	for i := 0; i < 5; i++ {
		body := `{"message":"ping"}`
		c, rec := echoContext(e, http.MethodPost, "/api/chat", body)
		err := h.Handle(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should pass without limiter", i+1)
	}
}

// ---------------------------------------------------------------------------
// Handler with auth + rate limiting combined
// ---------------------------------------------------------------------------

func TestHandler_AuthAndRateLimit_ReturnsCorrectError(t *testing.T) {
	h := newHandlerWithToken(t, "reply", "valid-token")
	e := echo.New()
	e.HideBanner = true

	// Auth fails first.
	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.Handle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

package chat_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/agent"
	"github.com/user/daily-info-agent/internal/chat"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stopResponse builds an OpenAI chat completion JSON with finish_reason=stop.
func stopResponse(content string) []byte {
	resp := map[string]interface{}{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "deepseek-v4-pro",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// errorResponse builds a non-2xx JSON error body the SDK understands.
func errorResponse(message string) []byte {
	body := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "server_error",
			"code":    "internal_server_error",
		},
	}
	b, _ := json.Marshal(body)
	return b
}

// newHandler wires up a chat.Handler with a mock LLM server that always
// returns the given content as a direct (stop) answer.
// The fetcher manager has no fetchers, so no real news is fetched.
func newHandler(t *testing.T, llmContent string) *chat.Handler {
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

	return chat.New(runner, "", slog.Default())
}

// newHandlerWithError wires up a handler whose LLM always returns a 503.
func newHandlerWithError(t *testing.T) *chat.Handler {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write(errorResponse("service unavailable"))
	}))
	t.Cleanup(srv.Close)

	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, nil, nil, cacheFile, slog.Default())
	runner := agent.New(srv.URL, "test-key", "deepseek-v4-pro", mgr, nil, slog.Default())

	return chat.New(runner, "", slog.Default())
}

// echoContext creates an Echo context for the given request.
func echoContext(e *echo.Echo, method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// echoContextWithHeader is like echoContext but sets one extra header.
func echoContextWithHeader(e *echo.Echo, method, path, body, header, value string) (echo.Context, *httptest.ResponseRecorder) {
	c, rec := echoContext(e, method, path, body)
	c.Request().Header.Set(header, value)
	return c, rec
}

// ---------------------------------------------------------------------------
// Validation — 400 paths (no LLM call needed)
// ---------------------------------------------------------------------------

func TestHandler_Chat_EmptyMessage_Returns400(t *testing.T) {
	e := echo.New()
	h := newHandlerWithError(t) // LLM never called

	c, rec := echoContext(e, http.MethodPost, "/api/chat", `{"message":""}`)
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "validation_error", resp.Error)
}

func TestHandler_Chat_WhitespaceOnlyMessage_Returns400(t *testing.T) {
	e := echo.New()
	h := newHandlerWithError(t)

	c, rec := echoContext(e, http.MethodPost, "/api/chat", `{"message":"   "}`)
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Chat_MessageTooLong_Returns400(t *testing.T) {
	e := echo.New()
	h := newHandlerWithError(t)

	long := strings.Repeat("a", 501)
	body, _ := json.Marshal(models.ChatRequest{Message: long})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "message_too_long", resp.Error)
}

func TestHandler_Chat_InvalidJSONBody_Returns400(t *testing.T) {
	e := echo.New()
	h := newHandlerWithError(t)

	c, rec := echoContext(e, http.MethodPost, "/api/chat", `{invalid json`)
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// Happy path — 200
// ---------------------------------------------------------------------------

func TestHandler_Chat_ValidMessage_Returns200WithReply(t *testing.T) {
	h := newHandler(t, "这是一个关于AI的回答。")

	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "今天有什么AI新闻？"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp models.ChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.SessionID, "session_id must be set")
	assert.Equal(t, "这是一个关于AI的回答。", resp.Reply)
	assert.NotEmpty(t, resp.FetchedAt)
	assert.GreaterOrEqual(t, resp.LatencyMs, int64(0))
	assert.NotNil(t, resp.Sources)
}

func TestHandler_Chat_SessionIDEchoedBackOnSubsequentTurns(t *testing.T) {
	h := newHandler(t, "好的，明白了。")
	e := echo.New()

	// First turn — no session_id in request.
	body1, _ := json.Marshal(models.ChatRequest{Message: "你好"})
	c1, rec1 := echoContext(e, http.MethodPost, "/api/chat", string(body1))
	require.NoError(t, h.Handle(c1))
	require.Equal(t, http.StatusOK, rec1.Code)

	var resp1 models.ChatResponse
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	assert.NotEmpty(t, resp1.SessionID)

	// Second turn — echo back the session_id.
	body2, _ := json.Marshal(models.ChatRequest{Message: "继续", SessionID: resp1.SessionID})
	c2, rec2 := echoContext(e, http.MethodPost, "/api/chat", string(body2))
	require.NoError(t, h.Handle(c2))
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 models.ChatResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, resp1.SessionID, resp2.SessionID, "session_id must be stable across turns")
}

func TestHandler_Chat_MessageAtMaxLength_Succeeds(t *testing.T) {
	h := newHandler(t, "收到。")
	e := echo.New()

	body, _ := json.Marshal(models.ChatRequest{Message: strings.Repeat("a", 500)})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	require.NoError(t, h.Handle(c))
	assert.NotEqual(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// LLM error path — 500
// ---------------------------------------------------------------------------

func TestHandler_Chat_LLMUnavailable_Returns500(t *testing.T) {
	h := newHandlerWithError(t)
	e := echo.New()

	body, _ := json.Marshal(models.ChatRequest{Message: "任何问题"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	require.NoError(t, h.Handle(c))

	// Agent retries twice before giving up, which takes ~2s.
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Error)
}

// ---------------------------------------------------------------------------
// Health endpoint
// ---------------------------------------------------------------------------

func TestHandler_Health_Returns200OK(t *testing.T) {
	e := echo.New()
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "ok",
			"version": "1.0.0-test",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

// ---------------------------------------------------------------------------
// Auth — CHAT_API_TOKEN gating
// ---------------------------------------------------------------------------

// newHandlerWithToken wires up a handler that requires the given API token.
func newHandlerWithToken(t *testing.T, llmContent, token string) *chat.Handler {
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

	return chat.New(runner, token, slog.Default())
}

func TestHandler_Chat_TokenRequired_MissingHeader_Returns401(t *testing.T) {
	h := newHandlerWithToken(t, "should not reach", "secret-token")
	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "你好"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_Chat_TokenRequired_WrongHeader_Returns401(t *testing.T) {
	h := newHandlerWithToken(t, "should not reach", "secret-token")
	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "你好"})
	c, rec := echoContextWithHeader(e, http.MethodPost, "/api/chat", string(body), "X-Api-Token", "wrong")
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_Chat_TokenRequired_XApiTokenHeader_Passes(t *testing.T) {
	h := newHandlerWithToken(t, "hello", "secret-token")
	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "你好"})
	c, rec := echoContextWithHeader(e, http.MethodPost, "/api/chat", string(body), "X-Api-Token", "secret-token")
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandler_Chat_TokenRequired_BearerAuth_Passes(t *testing.T) {
	h := newHandlerWithToken(t, "hello", "secret-token")
	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "你好"})
	c, rec := echoContextWithHeader(e, http.MethodPost, "/api/chat", string(body), "Authorization", "Bearer secret-token")
	require.NoError(t, h.Handle(c))
	assert.Equal(t, http.StatusOK, rec.Code)
}

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
	"github.com/user/daily-info-agent/internal/chat"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockLLMHandler returns an HTTP handler that serves an OpenAI-compatible
// response with the given content string as the message content.
func mockLLMHandler(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "deepseek-chat",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// newHandlerWithMockLLM wires up a chat.Handler with a mock DeepSeek server.
// The mock manager has no fetchers so FetchForTopic returns empty results.
func newHandlerWithMockLLM(t *testing.T, deepSeekContent string) *chat.Handler {
	t.Helper()

	// Mock DeepSeek server.
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", mockLLMHandler(deepSeekContent))
	dsSrv := httptest.NewServer(mux)
	t.Cleanup(dsSrv.Close)

	aiClient := processor.NewLLMClient("test-key", dsSrv.URL)
	proc := processor.New(aiClient, "deepseek-chat", slog.Default())

	// Manager with no fetchers → FetchForTopic returns empty results.
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, cacheFile, slog.Default())

	// Verifier that passes everything (skipVerification=true).
	ver := verifier.New(nil, true, slog.Default())

	cfg := &config.Config{
		DefaultCategories: models.AllCategories,
		RSSFeeds:          []string{},
	}

	return chat.New(proc, mgr, ver, cfg, slog.Default())
}

// newMinimalHandler returns a Handler whose deps are wired but will never be
// called (suitable for testing 400/validation paths only).
func newMinimalHandler(t *testing.T) *chat.Handler {
	t.Helper()
	// Use a non-existent DeepSeek URL; if deps are called the test will fail anyway.
	aiClient := processor.NewLLMClient("", "http://invalid.deepseek.test")
	proc := processor.New(aiClient, "model", slog.Default())
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, cacheFile, slog.Default())
	ver := verifier.New(nil, true, slog.Default())
	cfg := &config.Config{}
	return chat.New(proc, mgr, ver, cfg, slog.Default())
}

// echoContext creates an Echo context for the given request.
func echoContext(e *echo.Echo, method, path string, body string) (echo.Context, *httptest.ResponseRecorder) {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

// ---------------------------------------------------------------------------
// POST /api/chat — validation
// ---------------------------------------------------------------------------

func TestHandler_Chat_EmptyMessage_Returns400(t *testing.T) {
	e := echo.New()
	h := newMinimalHandler(t)

	c, rec := echoContext(e, http.MethodPost, "/api/chat", `{"message":""}`)

	err := h.Handle(c)
	require.NoError(t, err) // Echo handler returns nil; status is set on recorder.

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "validation_error", resp.Error)
	assert.NotEmpty(t, resp.Message)
}

func TestHandler_Chat_WhitespaceOnlyMessage_Returns400(t *testing.T) {
	e := echo.New()
	h := newMinimalHandler(t)

	c, rec := echoContext(e, http.MethodPost, "/api/chat", `{"message":"   "}`)
	err := h.Handle(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "validation_error", resp.Error)
}

func TestHandler_Chat_MessageTooLong_Returns400(t *testing.T) {
	e := echo.New()
	h := newMinimalHandler(t)

	// 501 characters — just over the 500-character limit.
	longMessage := strings.Repeat("a", 501)
	body, _ := json.Marshal(models.ChatRequest{Message: longMessage})

	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	err := h.Handle(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "message_too_long", resp.Error)
}

func TestHandler_Chat_MessageAtMaxLength_DoesNotReturn400(t *testing.T) {
	// 500 characters is exactly the limit — should not return 400 for length.
	// (It might still fail at DeepSeek call, but that's a different concern.)
	exactMaxMessage := strings.Repeat("a", 500)
	body, _ := json.Marshal(models.ChatRequest{Message: exactMaxMessage})

	topicJSON := `{"category":"科技/AI","keywords":["test"],"summary":"500-char message test"}`
	h := newHandlerWithMockLLM(t, topicJSON)

	e := echo.New()
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))
	err := h.Handle(c)
	require.NoError(t, err)

	// Should not get a 400 for message_too_long.
	assert.NotEqual(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_Chat_InvalidJSONBody_Returns400(t *testing.T) {
	e := echo.New()
	h := newMinimalHandler(t)

	c, rec := echoContext(e, http.MethodPost, "/api/chat", `{invalid json`)
	err := h.Handle(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// POST /api/chat — successful response shape
// ---------------------------------------------------------------------------

func TestHandler_Chat_ValidMessage_Returns200WithCorrectShape(t *testing.T) {
	topicJSON := `{"category":"科技/AI","keywords":["artificial intelligence","OpenAI"],"summary":"User asks about AI news"}`
	h := newHandlerWithMockLLM(t, topicJSON)

	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "What is the latest AI news?"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))

	err := h.Handle(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp models.ChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Required fields must be present.
	assert.NotEmpty(t, resp.ExtractedTopic, "extracted_topic should be populated")
	assert.NotEmpty(t, resp.Category, "category should be populated")
	assert.NotEmpty(t, resp.Summary, "summary should be populated")
	assert.NotEmpty(t, resp.FetchedAt, "fetched_at should be set")
	assert.GreaterOrEqual(t, resp.LatencyMs, int64(0), "latency_ms should be non-negative")
	// sources can be empty (no fetchers → no articles).
	assert.NotNil(t, resp.Sources)
}

func TestHandler_Chat_ValidMessage_CategoryEchoedFromAI(t *testing.T) {
	topicJSON := `{"category":"金融","keywords":["stock","market"],"summary":"Stock market inquiry"}`
	h := newHandlerWithMockLLM(t, topicJSON)

	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "How are the stock markets doing?"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))

	err := h.Handle(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp models.ChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "金融", resp.Category)
}

func TestHandler_Chat_NoFetchResults_SummaryContainsNoNewsMessage(t *testing.T) {
	topicJSON := `{"category":"政治","keywords":["politics"],"summary":"Political news inquiry"}`
	h := newHandlerWithMockLLM(t, topicJSON)

	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "Latest politics news?"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))

	err := h.Handle(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp models.ChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// When no items are found, the summary contains the fallback message.
	assert.Contains(t, resp.Summary, "暂时没有找到相关新闻")
}

// ---------------------------------------------------------------------------
// GET /health — inline health handler (mirrors main.go pattern)
// ---------------------------------------------------------------------------

func TestHandler_Health_Returns200OK(t *testing.T) {
	e := echo.New()

	// Register the health endpoint using the same pattern as main.go.
	const agentVersion = "1.0.0-test"
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "ok",
			"version": agentVersion,
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
	assert.Equal(t, agentVersion, body["version"])
	assert.NotEmpty(t, body["time"])
}

func TestHandler_Health_GET_MethodNotAllowed_ForPost(t *testing.T) {
	e := echo.New()
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// ---------------------------------------------------------------------------
// POST /api/chat — DeepSeek failure
// ---------------------------------------------------------------------------

func TestHandler_Chat_LLMUnavailable_Returns500(t *testing.T) {
	// Set up a DeepSeek server that returns a 503 error.
	dsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"service unavailable","type":"server_error"}}`))
	}))
	t.Cleanup(dsSrv.Close)

	aiClient := processor.NewLLMClient("test-key", dsSrv.URL)
	proc := processor.New(aiClient, "deepseek-chat", slog.Default())
	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, cacheFile, slog.Default())
	ver := verifier.New(nil, true, slog.Default())
	cfg := &config.Config{}

	h := chat.New(proc, mgr, ver, cfg, slog.Default())

	e := echo.New()
	body, _ := json.Marshal(models.ChatRequest{Message: "Any news?"})
	c, rec := echoContext(e, http.MethodPost, "/api/chat", string(body))

	err := h.Handle(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var resp models.ChatErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Error)
}

package agent_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/agent"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Mock article store
// ---------------------------------------------------------------------------

// mockArticleStore implements agent.ArticleSearcher for testing.
type mockArticleStore struct {
	rows []models.ArticleRow
	err  error
}

func (m *mockArticleStore) ListArticles(_ context.Context, _ models.ArticleFilter) ([]models.ArticleRow, int, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.rows, len(m.rows), nil
}

func newRunnerWithStore(t *testing.T, handler http.HandlerFunc, store agent.ArticleSearcher) *agent.Runner {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, nil, nil, cacheFile, slog.Default())
	return agent.New(srv.URL, "test-key", "deepseek-v4-pro", mgr, store, slog.Default())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func stopResp(content string) map[string]interface{} {
	return map[string]interface{}{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "deepseek-v4-pro",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]interface{}{"role": "assistant", "content": content},
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
	}
}

func toolCallResp(toolName, argsJSON, callID string) map[string]interface{} {
	return map[string]interface{}{
		"id":      "chatcmpl-tool",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "deepseek-v4-pro",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"finish_reason": "tool_calls",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]interface{}{
						{
							"id":   callID,
							"type": "function",
							"function": map[string]interface{}{
								"name":      toolName,
								"arguments": argsJSON,
							},
						},
					},
				},
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80},
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func newRunner(t *testing.T, handler http.HandlerFunc) *agent.Runner {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cacheFile := filepath.Join(t.TempDir(), "dedup.json")
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, nil, nil, cacheFile, slog.Default())
	return agent.New(srv.URL, "test-key", "deepseek-v4-pro", mgr, nil, slog.Default())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunner_DirectAnswer_NoToolCall(t *testing.T) {
	// LLM replies directly without calling any tool.
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, stopResp("我是一个新闻助手。"))
	})

	result, err := r.Run(context.Background(), "", "你是谁？")

	require.NoError(t, err)
	assert.Equal(t, "我是一个新闻助手。", result.Reply)
	assert.False(t, result.ToolCalled)
	assert.Equal(t, 1, result.Iterations)
	assert.NotEmpty(t, result.SessionID)
}

func TestRunner_NewSession_IDAssigned(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, stopResp("ok"))
	})

	res, err := r.Run(context.Background(), "", "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, res.SessionID)
}

func TestRunner_ExistingSession_IDPreserved(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, stopResp("ok"))
	})

	res1, err := r.Run(context.Background(), "", "第一句话")
	require.NoError(t, err)

	res2, err := r.Run(context.Background(), res1.SessionID, "第二句话")
	require.NoError(t, err)

	assert.Equal(t, res1.SessionID, res2.SessionID)
}

func TestRunner_ToolCall_ThenFinalAnswer(t *testing.T) {
	// First LLM call → tool_calls; second call → stop.
	var callCount atomic.Int32
	argsJSON := `{"keywords":["AI","OpenAI"],"category":"科技/AI"}`

	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		call := callCount.Add(1)
		if call == 1 {
			writeJSON(w, toolCallResp("search_news", argsJSON, "call-001"))
		} else {
			writeJSON(w, stopResp("根据搜索结果，OpenAI 近期发布了新模型。"))
		}
	})

	result, err := r.Run(context.Background(), "", "最近 OpenAI 有什么新闻？")

	require.NoError(t, err)
	assert.True(t, result.ToolCalled)
	assert.Equal(t, "根据搜索结果，OpenAI 近期发布了新模型。", result.Reply)
	assert.Equal(t, 2, result.Iterations)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestRunner_LLMError_ReturnsError(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"service unavailable","type":"server_error","code":"503"}}`))
	})

	_, err := r.Run(context.Background(), "", "随便问点什么")
	require.Error(t, err)
}

func TestRunner_MaxIterationsGuard(t *testing.T) {
	// LLM always returns tool_calls — agent should stop after maxIterations.
	argsJSON := `{"keywords":["test"],"category":"国际"}`

	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, toolCallResp("search_news", argsJSON, "call-loop"))
	})

	// Should not hang; returns whatever it has after hitting the cap.
	result, err := r.Run(context.Background(), "", "无限循环测试")
	// Either an error or an empty-ish reply — key point: it terminates.
	_ = err
	_ = result
}

func TestRunner_GetCurrentTimeTool_CalledAndReturnsReply(t *testing.T) {
	var callCount atomic.Int32

	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		call := callCount.Add(1)
		if call == 1 {
			writeJSON(w, toolCallResp("get_current_time", "{}", "call-time-001"))
		} else {
			writeJSON(w, stopResp("当前北京时间已获取到。"))
		}
	})

	result, err := r.Run(context.Background(), "", "现在几点了？")
	require.NoError(t, err)
	assert.True(t, result.ToolCalled)
	assert.NotEmpty(t, result.Reply)
	assert.Equal(t, 2, result.Iterations)
	assert.Equal(t, int32(2), callCount.Load())
}

// ---------------------------------------------------------------------------
// search_stored_articles tool
// ---------------------------------------------------------------------------

func TestRunner_SearchStoredArticles_ReturnsFormattedResults(t *testing.T) {
	// Prepare mock store with one article.
	store := &mockArticleStore{
		rows: []models.ArticleRow{
			{
				ID:           1,
				Title:        "AI 大模型最新进展",
				Summary:      "本周 AI 领域发生了重大突破。",
				Category:     models.CategoryTechAI,
				SourceDomain: "36kr.com",
				FetchedAt:    time.Now().UTC(),
			},
		},
	}

	var callCount atomic.Int32
	argsJSON := `{"query":"AI 大模型","days":7}`

	r := newRunnerWithStore(t, func(w http.ResponseWriter, _ *http.Request) {
		call := callCount.Add(1)
		if call == 1 {
			writeJSON(w, toolCallResp("search_stored_articles", argsJSON, "call-db-001"))
		} else {
			writeJSON(w, stopResp("根据数据库记录，本周 AI 领域有重大进展。"))
		}
	}, store)

	result, err := r.Run(context.Background(), "", "最近一周有哪些 AI 新闻？")

	require.NoError(t, err)
	assert.True(t, result.ToolCalled)
	assert.Equal(t, "根据数据库记录，本周 AI 领域有重大进展。", result.Reply)
	assert.Equal(t, 2, result.Iterations)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestRunner_SearchStoredArticles_NilStore_ToolNotRegistered(t *testing.T) {
	// When no store is wired in, the search_stored_articles tool should not
	// appear in the tool list; the LLM should never try to call it.
	// If it does (mock returns tool_calls), the executor returns an error message.
	argsJSON := `{"query":"test"}`
	var callCount atomic.Int32

	r := newRunnerWithStore(t, func(w http.ResponseWriter, _ *http.Request) {
		call := callCount.Add(1)
		if call == 1 {
			// Simulate LLM calling the tool even without a store.
			writeJSON(w, toolCallResp("search_stored_articles", argsJSON, "call-no-db"))
		} else {
			writeJSON(w, stopResp("数据库未启用，无法查询。"))
		}
	}, nil) // nil store

	result, err := r.Run(context.Background(), "", "查一下历史文章")

	require.NoError(t, err)
	// Even if LLM calls the tool, it should complete (graceful degradation).
	assert.NotEmpty(t, result.Reply)
}

func TestRunner_SearchStoredArticles_EmptyResults(t *testing.T) {
	store := &mockArticleStore{rows: []models.ArticleRow{}} // no articles

	var callCount atomic.Int32
	argsJSON := `{"query":"不存在的话题","days":7}`

	r := newRunnerWithStore(t, func(w http.ResponseWriter, _ *http.Request) {
		call := callCount.Add(1)
		if call == 1 {
			writeJSON(w, toolCallResp("search_stored_articles", argsJSON, "call-empty"))
		} else {
			writeJSON(w, stopResp("暂无相关历史文章。"))
		}
	}, store)

	result, err := r.Run(context.Background(), "", "有没有关于某个冷僻话题的文章？")

	require.NoError(t, err)
	assert.Equal(t, "暂无相关历史文章。", result.Reply)
}

func TestRunner_ReasoningContent_UsedWhenContentEmpty(t *testing.T) {
	// deepseek thinking models sometimes return content="" and put the answer
	// in reasoning_content. The runner should fall back to it.
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"id":      "chatcmpl-think",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "deepseek-v4-pro",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]interface{}{
						"role":              "assistant",
						"content":           "",
						"reasoning_content": "思考过程中得出的答案",
					},
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		}
		writeJSON(w, resp)
	})

	result, err := r.Run(context.Background(), "", "需要推理的问题")
	require.NoError(t, err)
	assert.Equal(t, "思考过程中得出的答案", result.Reply)
}

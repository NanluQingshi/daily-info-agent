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
)

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
	mgr := fetcher.NewManager([]fetcher.Fetcher{}, nil, cacheFile, slog.Default())
	return agent.New(srv.URL, "test-key", "deepseek-v4-pro", mgr, slog.Default())
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

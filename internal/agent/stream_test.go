package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/agent"
)

// ── SSE helpers ───────────────────────────────────────────────────────────────

// sseTokens writes an OpenAI-compatible SSE stream of tokens, then [DONE].
func sseTokens(w http.ResponseWriter, tokens ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	for _, tok := range tokens {
		chunk := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"delta":         map[string]interface{}{"content": tok},
					"finish_reason": nil,
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	// final chunk
	finalChunk := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{}, "finish_reason": "stop"},
		},
	}
	data, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// collectStream runs RunStream and returns all events in order.
func collectStream(r *agent.Runner, ctx context.Context, sessionID, msg string) []agent.StreamEvent {
	var events []agent.StreamEvent
	r.RunStream(ctx, sessionID, msg, func(ev agent.StreamEvent) {
		events = append(events, ev)
	})
	return events
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestRunStream_DirectAnswer_EmitsThinkingDeltaDone(t *testing.T) {
	// LLM streams a two-token answer with no tool calls.
	r := newRunner(t, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(req.Body).Decode(&body)
		if body["stream"] == true {
			sseTokens(w, "Hello", " World")
			return
		}
		writeJSON(w, stopResp("Hello World"))
	})

	events := collectStream(r, context.Background(), "", "hi")

	types := make([]string, len(events))
	for i, e := range events {
		types[i] = string(e.Type)
	}

	assert.Contains(t, types, "thinking")
	assert.Contains(t, types, "delta")
	assert.Contains(t, types, "done")
	assert.NotContains(t, types, "error")

	// Reconstruct the full reply from delta events.
	var reply strings.Builder
	for _, e := range events {
		if e.Type == agent.EventDelta {
			reply.WriteString(e.Content)
		}
	}
	assert.Equal(t, "Hello World", reply.String())
}

func TestRunStream_DoneEvent_ContainsSessionIDAndLatency(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(req.Body).Decode(&body)
		if body["stream"] == true {
			sseTokens(w, "ok")
			return
		}
		writeJSON(w, stopResp("ok"))
	})

	events := collectStream(r, context.Background(), "", "test")

	var done *agent.StreamEvent
	for i := range events {
		if events[i].Type == agent.EventDone {
			done = &events[i]
			break
		}
	}
	require.NotNil(t, done, "expected a done event")
	assert.NotEmpty(t, done.SessionID)
	assert.GreaterOrEqual(t, done.LatencyMs, int64(0))
}

func TestRunStream_ToolCallThenStream_EmitsToolAndDeltas(t *testing.T) {
	// First call → tool_calls; second call → SSE stream.
	var callCount atomic.Int32
	argsJSON := `{"keywords":["AI"],"category":"科技/AI"}`

	r := newRunner(t, func(w http.ResponseWriter, req *http.Request) {
		call := callCount.Add(1)
		var body map[string]interface{}
		json.NewDecoder(req.Body).Decode(&body)

		if call == 1 {
			writeJSON(w, toolCallResp("search_news", argsJSON, "call-stream-001"))
			return
		}
		if body["stream"] == true {
			sseTokens(w, "根据", "搜索", "结果")
			return
		}
		writeJSON(w, stopResp("根据搜索结果"))
	})

	events := collectStream(r, context.Background(), "", "最近有什么AI新闻？")

	var types []string
	for _, e := range events {
		types = append(types, string(e.Type))
	}

	assert.Contains(t, types, "tool")
	assert.Contains(t, types, "delta")
	assert.Contains(t, types, "done")

	// done event should report tool_called=true.
	for _, e := range events {
		if e.Type == agent.EventDone {
			assert.True(t, e.ToolCalled)
		}
	}

	// Collect tool event name.
	for _, e := range events {
		if e.Type == agent.EventTool {
			assert.Equal(t, "search_news", e.ToolName)
		}
	}
}

func TestRunStream_SameSession_IDPreservedAcrossTurns(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(req.Body).Decode(&body)
		if body["stream"] == true {
			sseTokens(w, "ok")
			return
		}
		writeJSON(w, stopResp("ok"))
	})

	events1 := collectStream(r, context.Background(), "", "first message")
	var sid1 string
	for _, e := range events1 {
		if e.Type == agent.EventDone {
			sid1 = e.SessionID
		}
	}
	require.NotEmpty(t, sid1)

	events2 := collectStream(r, context.Background(), sid1, "second message")
	var sid2 string
	for _, e := range events2 {
		if e.Type == agent.EventDone {
			sid2 = e.SessionID
		}
	}
	assert.Equal(t, sid1, sid2, "session ID must be stable across turns")
}

func TestRunStream_LLMError_EmitsErrorEvent(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"overloaded","type":"server_error","code":"503"}}`))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := collectStream(r, ctx, "", "will fail")

	var hasError bool
	for _, e := range events {
		if e.Type == agent.EventError {
			hasError = true
			assert.NotEmpty(t, e.Content)
		}
	}
	assert.True(t, hasError, "expected an error event")
}

// TestParseLLMStream_ReasoningContentFallback tests ParseLLMStream directly
// (without going through RunStream) to verify that chunks with empty content
// but non-empty reasoning_content still deliver tokens to the caller.
// This covers deepseek thinking-model output where reasoning_content carries
// the answer and content is an empty string.
func TestParseLLMStream_ReasoningContentFallback(t *testing.T) {
	// Build a minimal SSE body.
	chunks := []map[string]interface{}{
		{"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "", "reasoning_content": "思考中"}, "finish_reason": nil},
		}},
		{"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "答案"}, "finish_reason": nil},
		}},
		{"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{}, "finish_reason": "stop"},
		}},
	}

	var sb strings.Builder
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		sb.WriteString("data: ")
		sb.Write(data)
		sb.WriteString("\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")

	body := strings.NewReader(sb.String())

	var tokens []string
	err := agent.ParseLLMStream(body, func(tok string) { tokens = append(tokens, tok) })
	require.NoError(t, err)
	assert.Equal(t, []string{"思考中", "答案"}, tokens)
}

// TestRunStream_EmptyStopReply_EmitsFallback verifies that when the LLM returns
// a stop response with empty content (and no reasoning_content) on a
// non-streaming iteration, the stream emits the fallback reply instead of a
// blank delta — matching the non-stream Run path.
func TestRunStream_EmptyStopReply_EmitsFallback(t *testing.T) {
	r := newRunner(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, stopResp(""))
	})

	events := collectStream(r, context.Background(), "", "hi")

	var reply strings.Builder
	for _, e := range events {
		if e.Type == agent.EventDelta {
			reply.WriteString(e.Content)
		}
	}
	assert.Equal(t, "抱歉，我暂时无法生成回复，请稍后再试。", reply.String())

	// done should still fire so the client finalizes the turn.
	var hasDone bool
	for _, e := range events {
		if e.Type == agent.EventDone {
			hasDone = true
		}
	}
	assert.True(t, hasDone, "expected a done event after fallback")
}

// TestRunStream_EmptyTokenStream_EmitsFallback drives the loop to the final
// (streaming) iteration by returning tool_calls on every non-streaming call,
// then streams zero tokens. The fallback reply must be emitted so the client
// never renders an empty assistant bubble.
func TestRunStream_EmptyTokenStream_EmitsFallback(t *testing.T) {
	argsJSON := `{"keywords":["test"],"category":"国际"}`

	r := newRunner(t, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(req.Body).Decode(&body)

		if body["stream"] == true {
			// Final streaming call: emit only the stop chunk, no content tokens.
			sseTokens(w)
			return
		}
		// Non-streaming calls: always tool_calls to reach the final iteration.
		writeJSON(w, toolCallResp("search_news", argsJSON, "call-loop"))
	})

	events := collectStream(r, context.Background(), "", "test")

	var reply strings.Builder
	for _, e := range events {
		if e.Type == agent.EventDelta {
			reply.WriteString(e.Content)
		}
	}
	assert.Equal(t, "抱歉，我暂时无法生成回复，请稍后再试。", reply.String())
}

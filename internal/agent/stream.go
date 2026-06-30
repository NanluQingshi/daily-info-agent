package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	openai "github.com/sashabaranov/go-openai"
	"github.com/user/daily-info-agent/pkg/backoff"
	"github.com/user/daily-info-agent/pkg/models"
)

// ── Event types ───────────────────────────────────────────────────────────────

// EventType identifies a single SSE event sent to the client.
type EventType string

const (
	EventThinking EventType = "thinking" // LLM is processing; no content
	EventTool     EventType = "tool"     // a tool was invoked
	EventDelta    EventType = "delta"    // one token of the final answer
	EventDone     EventType = "done"     // stream complete; includes metadata
	EventError    EventType = "error"    // unrecoverable error
)

// StreamEvent is one unit pushed over SSE.
type StreamEvent struct {
	Type       EventType        `json:"type"`
	Content    string           `json:"content,omitempty"`  // delta text or error message
	ToolName   string           `json:"tool,omitempty"`     // tool event: which tool
	SessionID  string           `json:"session_id,omitempty"`
	Sources    []models.ChatSource `json:"sources,omitempty"`
	ToolCalled bool             `json:"tool_called,omitempty"`
	LatencyMs  int64            `json:"latency_ms,omitempty"`
}

// Sender is a function that pushes one event toward the client.
// Implementations must be safe to call from a single goroutine.
type Sender func(StreamEvent)

// ── RunStream ─────────────────────────────────────────────────────────────────

// RunStream executes the agent loop and delivers events via send.
//
//   - Tool-call iterations use the regular (non-streaming) LLM path and emit
//     EventThinking + EventTool events.
//   - The final (answer) iteration uses a streaming LLM call, emitting one
//     EventDelta per token and a single EventDone at the end.
func (r *Runner) RunStream(ctx context.Context, sessionID, userMessage string, send Sender) {
	start := time.Now()

	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	messages := r.sessions.Get(sessionID)
	if len(messages) == 0 {
		messages = []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		}
	}
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userMessage,
	})

	var (
		allSources []models.ChatSource
		toolCalled bool
		fullReply  strings.Builder
	)

	for iteration := 0; iteration < maxIterations; iteration++ {
		send(StreamEvent{Type: EventThinking})

		// ── Tool-discovery pass: non-streaming call WITH tools ─────────────
		// We need to see tool_calls before we can execute them; streaming
		// tool-call assembly is fiddly and not worth the complexity here.
		// Tools stay attached on every iteration so the model can still
		// request another tool call late in the loop.
		resp, err := r.callLLM(ctx, messages)
		if err != nil {
			send(StreamEvent{Type: EventError, Content: err.Error()})
			return
		}
		choice := resp.Choices[0]

		if choice.FinishReason == "tool_calls" && len(choice.Message.ToolCalls) > 0 {
			toolCalled = true
			messages = append(messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})
			for _, tc := range choice.Message.ToolCalls {
				send(StreamEvent{Type: EventTool, ToolName: tc.Function.Name})
				r.logger.Info("tool call (stream)", "tool", tc.Function.Name)

				result, items := r.executor.Execute(ctx, tc)
				for _, it := range items {
					allSources = append(allSources, models.ChatSource{
						URL:          it.URL,
						Title:        it.Title,
						SourceDomain: it.SourceDomain,
					})
				}
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			// After executing tools, loop back for another discovery pass —
			// unless we've hit the iteration cap, in which case fall through
			// to a forced final-answer stream below.
			if iteration < maxIterations-1 {
				continue
			}
			r.logger.Warn("stream agent hit max iterations mid-tool; forcing final answer",
				slog.Int("iteration", iteration+1),
			)
		} else {
			// Non-streaming stop: the model answered without tools (e.g. a
			// reasoning model). Emit whatever text it produced and finish.
			finalReply := choice.Message.Content
			if finalReply == "" {
				finalReply = choice.Message.ReasoningContent
			}
			if finalReply == "" {
				finalReply = fallbackReply
			}
			send(StreamEvent{Type: EventDelta, Content: finalReply})
			fullReply.WriteString(finalReply)
			messages = append(messages, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant, Content: finalReply,
			})
			break
		}

		// ── Forced final-answer stream (tools omitted) ─────────────────────
		// Reached only when the cap was hit mid-tool-call. Stream a direct
		// answer without tools so the model is forced to summarise what it
		// has gathered rather than request yet another tool.
		err = r.streamLLM(ctx, messages, func(token string) {
			send(StreamEvent{Type: EventDelta, Content: token})
			fullReply.WriteString(token)
		})
		if err != nil {
			send(StreamEvent{Type: EventError, Content: err.Error()})
			return
		}
		if fullReply.Len() == 0 {
			send(StreamEvent{Type: EventDelta, Content: fallbackReply})
			fullReply.WriteString(fallbackReply)
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleAssistant, Content: fullReply.String(),
		})
		break
	}

	r.sessions.Set(sessionID, messages)

	send(StreamEvent{
		Type:       EventDone,
		SessionID:  sessionID,
		Sources:    allSources,
		ToolCalled: toolCalled,
		LatencyMs:  time.Since(start).Milliseconds(),
	})
}

// ── Streaming LLM call ────────────────────────────────────────────────────────

// streamLLM makes a streaming POST to the LLM endpoint and calls onToken for
// each content delta. It blocks until the stream is fully consumed.
func (r *Runner) streamLLM(ctx context.Context, messages []openai.ChatCompletionMessage, onToken func(string)) error {
	llmMsgs := make([]llmMessage, len(messages))
	for i, m := range messages {
		llmMsgs[i] = llmMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}

	reqBody := map[string]interface{}{
		"model":    r.modelID,
		"messages": llmMsgs,
		"stream":   true,
		// Tools omitted on the final pass — the model should just write the answer.
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal stream request: %w", err)
	}

	// Establish the SSE connection with retries for transient failures
	// (429, 5xx, network). Once a 2xx response is received we commit to
	// consuming the body — mid-stream errors cannot be retried without
	// emitting duplicate tokens.
	var httpResp *http.Response
	err = backoff.Retry(ctx, 3, 2*time.Second, func() error {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			r.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("build stream request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := r.streamClient.Do(httpReq)
		if err != nil {
			return &backoff.RetryableError{Cause: fmt.Errorf("stream http do: %w", err)}
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return &backoff.RetryableError{
				Cause: fmt.Errorf("stream api http %d: %s", resp.StatusCode, string(raw)),
			}
		}
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("stream api http %d: %s", resp.StatusCode, string(raw))
		}
		httpResp = resp
		return nil
	})
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	return ParseLLMStream(httpResp.Body, onToken)
}

// ParseLLMStream reads an OpenAI-compatible SSE stream and calls onToken for
// each non-empty content delta. Exported for testing.
func ParseLLMStream(body io.Reader, onToken func(string)) error {
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		token := chunk.Choices[0].Delta.Content
		if token == "" {
			token = chunk.Choices[0].Delta.ReasoningContent
		}
		if token != "" {
			onToken(token)
		}
	}

	return scanner.Err()
}

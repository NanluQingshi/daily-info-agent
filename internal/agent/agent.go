// Package agent implements an LLM-driven agent that uses tool calling to
// answer user questions. The LLM decides whether to call tools (e.g.
// search_news) or reply directly; the agent loop runs until the model
// produces a final text response or the iteration cap is reached.
package agent

import (
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
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/backoff"
	"github.com/user/daily-info-agent/pkg/models"
)

const maxIterations = 5 // guard against runaway tool-call loops

// fallbackReply is sent when the LLM produces no final text after the agent
// loop completes (e.g. empty content on a stop response, or a streaming pass
// that delivered zero tokens). Keeps the UX consistent with the non-stream
// path and gives the user something actionable instead of a blank bubble.
const fallbackReply = "抱歉，我暂时无法生成回复，请稍后再试。"

// RunResult is returned by Runner.Run after the agent loop completes.
type RunResult struct {
	SessionID  string
	Reply      string           // final text reply from the LLM
	Sources    []models.RawItem // articles fetched during this turn (may be empty)
	ToolCalled bool             // true if at least one tool was invoked
	Iterations int              // number of LLM calls made
}

// Runner is the stateful agent that manages sessions and drives the LLM loop.
type Runner struct {
	baseURL       string
	apiKey        string
	modelID       string
	httpClient    *http.Client // non-streaming calls; bounded by Timeout
	streamClient  *http.Client // streaming calls; no overall timeout (ctx controls lifetime)
	sessions      *SessionStore
	executor      *toolExecutor
	logger        *slog.Logger
}

// New creates a Runner.
// baseURL should be the OpenAI-compatible endpoint root, e.g. "https://api.llm.ustc.edu.cn/v1".
// apiKey is the Bearer token sent in the Authorization header.
// db may be nil; when nil, the search_stored_articles tool is not registered.
func New(
	baseURL, apiKey, modelID string,
	mgr *fetcher.Manager,
	db ArticleSearcher,
	logger *slog.Logger,
) *Runner {
	return &Runner{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		modelID:      modelID,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		streamClient: &http.Client{}, // no overall timeout; SSE lifetime is bounded by ctx
		sessions:     NewSessionStore(),
		executor:     newToolExecutor(mgr, db),
		logger:       logger,
	}
}

// DeleteSession removes a session from the store, freeing its memory.
// Safe to call with an unknown id (no-op).
func (r *Runner) DeleteSession(sessionID string) { r.sessions.Delete(sessionID) }

// Run executes one user turn of the agent loop.
//
//   - If sessionID is empty, a new session is created and its ID is returned
//     in RunResult.SessionID.
//   - The loop continues until the LLM produces a stop response or
//     maxIterations is reached.
func (r *Runner) Run(ctx context.Context, sessionID, userMessage string) (RunResult, error) {
	// ── Session bootstrap ─────────────────────────────────────────────────────
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

	// ── Agent loop ────────────────────────────────────────────────────────────
	var (
		allSources []models.RawItem
		toolCalled bool
		iterations int
		finalReply string
	)

	for iterations = 0; iterations < maxIterations; iterations++ {
		r.logger.Debug("agent iteration",
			slog.Int("iteration", iterations+1),
			slog.Int("messages", len(messages)),
		)

		resp, err := r.callLLM(ctx, messages)
		if err != nil {
			return RunResult{}, fmt.Errorf("llm call failed (iteration %d): %w", iterations+1, err)
		}

		choice := resp.Choices[0]

		// ── Tool calls ────────────────────────────────────────────────────────
		if choice.FinishReason == "tool_calls" && len(choice.Message.ToolCalls) > 0 {
			toolCalled = true
			messages = append(messages, openai.ChatCompletionMessage{
				Role:      openai.ChatMessageRoleAssistant,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			})

			for _, tc := range choice.Message.ToolCalls {
				r.logger.Info("tool call",
					slog.String("tool", tc.Function.Name),
					slog.String("args", tc.Function.Arguments),
				)
				result, items := r.executor.Execute(ctx, tc)
				allSources = append(allSources, items...)
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			continue
		}

		// ── Final answer ──────────────────────────────────────────────────────
		finalReply = choice.Message.Content
		if finalReply == "" {
			// Some thinking models put the answer in reasoning_content when
			// content is empty.
			finalReply = choice.Message.ReasoningContent
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: finalReply,
		})
		break
	}

	if finalReply == "" {
		finalReply = fallbackReply
	}

	r.sessions.Set(sessionID, messages)

	r.logger.Info("agent run complete",
		slog.String("session_id", sessionID),
		slog.Bool("tool_called", toolCalled),
		slog.Int("iterations", iterations+1),
		slog.Int("sources", len(allSources)),
	)

	return RunResult{
		SessionID:  sessionID,
		Reply:      finalReply,
		Sources:    allSources,
		ToolCalled: toolCalled,
		Iterations: iterations + 1,
	}, nil
}

// ── Raw HTTP LLM call ─────────────────────────────────────────────────────────

// llmMessage mirrors openai.ChatCompletionMessage but also exposes the
// deepseek-specific reasoning_content field that the SDK struct omits.
type llmMessage struct {
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	ReasoningContent string             `json:"reasoning_content,omitempty"`
	ToolCalls        []openai.ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string             `json:"tool_call_id,omitempty"`
}

// llmResponse is the minimal shape of an OpenAI chat completion response that
// we need, extended with reasoning_content.
type llmResponse struct {
	Choices []struct {
		FinishReason string     `json:"finish_reason"`
		Message      llmMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// callLLM makes a raw HTTP POST to the LLM endpoint so that non-standard
// fields (e.g. reasoning_content) are preserved in the parsed response.
// Transient failures (429, 5xx, network errors) are retried with exponential
// backoff; non-retryable errors (4xx other than 429) surface immediately.
func (r *Runner) callLLM(ctx context.Context, messages []openai.ChatCompletionMessage) (llmResponse, error) {
	var resp llmResponse
	err := backoff.Retry(ctx, 3, 2*time.Second, func() error {
		var err error
		resp, err = r.callLLMOnce(ctx, messages)
		return err
	})
	if err != nil {
		return llmResponse{}, err
	}
	return resp, nil
}

func (r *Runner) callLLMOnce(ctx context.Context, messages []openai.ChatCompletionMessage) (llmResponse, error) {
	// Convert messages to our llmMessage slice (which serialises identically
	// to openai.ChatCompletionMessage for the fields we use).
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
		"model":       r.modelID,
		"messages":    llmMsgs,
		"tools":       r.executor.activeTools(),
		"tool_choice": "auto",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return llmResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	url := r.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return llmResponse{}, fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	httpResp, err := r.httpClient.Do(httpReq)
	if err != nil {
		// Network / connection errors are transient — retry.
		r.logger.Warn("llm http request failed",
			slog.String("error", err.Error()),
		)
		return llmResponse{}, &backoff.RetryableError{Cause: fmt.Errorf("http do: %w", err)}
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		r.logger.Warn("llm read body failed",
			slog.Int("status", httpResp.StatusCode),
			slog.String("error", err.Error()),
		)
		return llmResponse{}, &backoff.RetryableError{Cause: fmt.Errorf("read body: %w", err)}
	}

	var resp llmResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		r.logger.Warn("llm unmarshal failed",
			slog.Int("status", httpResp.StatusCode),
			slog.String("body_preview", truncateForLog(string(raw), 200)),
		)
		return llmResponse{}, &backoff.RetryableError{Cause: fmt.Errorf("unmarshal response: %w", err)}
	}

	if resp.Error != nil {
		// Provider-side error envelope: treat 5xx-class types as retryable.
		r.logger.Warn("llm api error",
			slog.String("error_type", resp.Error.Type),
			slog.String("message", resp.Error.Message),
			slog.Int("status", httpResp.StatusCode),
		)
		if isRetryableErrorType(resp.Error.Type) {
			return llmResponse{}, &backoff.RetryableError{
				Cause: fmt.Errorf("api error (%s): %s", resp.Error.Type, resp.Error.Message),
			}
		}
		return llmResponse{}, fmt.Errorf("api error (%s): %s", resp.Error.Type, resp.Error.Message)
	}
	if httpResp.StatusCode == 429 || httpResp.StatusCode >= 500 {
		r.logger.Warn("llm retryable http error",
			slog.Int("status", httpResp.StatusCode),
			slog.String("body_preview", truncateForLog(string(raw), 200)),
		)
		return llmResponse{}, &backoff.RetryableError{
			Cause: fmt.Errorf("api http %d: %s", httpResp.StatusCode, string(raw)),
		}
	}
	if httpResp.StatusCode >= 400 {
		r.logger.Warn("llm non-retryable http error",
			slog.Int("status", httpResp.StatusCode),
			slog.String("body_preview", truncateForLog(string(raw), 200)),
		)
		return llmResponse{}, fmt.Errorf("api http %d: %s", httpResp.StatusCode, string(raw))
	}
	if len(resp.Choices) == 0 {
		return llmResponse{}, fmt.Errorf("empty choices in response")
	}

	return resp, nil
}

// isRetryableErrorType returns true for provider error types that usually
// resolve on retry (rate limits, server-side overload).
func isRetryableErrorType(t string) bool {
	switch t {
	case "rate_limit_exceeded", "server_error", "overloaded_error", "temporarily_overloaded":
		return true
	}
	return false
}

// truncateForLog returns the first n runes of s, for embedding in log fields
// without dumping multi-KB error bodies.
func truncateForLog(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

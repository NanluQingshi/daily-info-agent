// Package processor calls the DeepSeek API to categorise, summarise, and score news items.
package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/user/daily-info-agent/pkg/models"
)

const (
	maxBatchSize      = 10
	interCallDelay    = 100 * time.Millisecond
	deepSeekRetryWait = 2 * time.Second
	maxContentLen     = 500 // chars sent to AI per item
)

// LLMUnavailableError is returned (and logged) when all retries to DeepSeek fail.
type LLMUnavailableError struct {
	Cause error
}

func (e *LLMUnavailableError) Error() string {
	return fmt.Sprintf("deepseek unavailable: %v", e.Cause)
}
func (e *LLMUnavailableError) Unwrap() error { return e.Cause }

// LLMParseError is returned when the AI response cannot be parsed.
type LLMParseError struct {
	Raw   string
	Cause error
}

func (e *LLMParseError) Error() string {
	return fmt.Sprintf("parse deepseek response: %v (raw=%q)", e.Cause, e.Raw)
}
func (e *LLMParseError) Unwrap() error { return e.Cause }

// TopicResult holds the structured output of topic extraction.
type TopicResult struct {
	Category models.Category
	Keywords []string // search terms to pass to FetchForTopic
	Summary  string   // one-sentence description of what the user wants
}

// Processor calls DeepSeek for AI enrichment.
type Processor struct {
	client  *openai.Client
	modelID string
	logger  *slog.Logger
}

// New creates a Processor using the given go-openai client pointed at DeepSeek.
func New(client *openai.Client, modelID string, logger *slog.Logger) *Processor {
	return &Processor{
		client:  client,
		modelID: modelID,
		logger:  logger,
	}
}

// NewLLMClient creates an OpenAI-compatible client configured for DeepSeek.
func NewLLMClient(apiKey, baseURL string) *openai.Client {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	return openai.NewClientWithConfig(cfg)
}

// ProcessBatch enriches a batch of raw items with AI outputs.
// Items are split internally into sub-batches of up to 10 before each API call.
// If DeepSeek is unavailable, affected items are returned with zero-value AI fields
// and logged at WARN; the function does not return a fatal error in that case.
func (p *Processor) ProcessBatch(ctx context.Context, items []models.RawItem, runID string) ([]models.ProcessedArticle, error) {
	if len(items) == 0 {
		return nil, nil
	}

	articles := make([]models.ProcessedArticle, 0, len(items))

	for start := 0; start < len(items); start += maxBatchSize {
		end := start + maxBatchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]

		// Rate-limiting courtesy delay between calls (skip for the very first batch)
		if start > 0 {
			select {
			case <-ctx.Done():
				return articles, ctx.Err()
			case <-time.After(interCallDelay):
			}
		}

		results, err := p.processBatchCall(ctx, batch)
		if err != nil {
			p.logger.Warn("deepseek batch call failed; degrading gracefully",
				slog.String("run_id", runID),
				slog.String("error", err.Error()),
				slog.Bool("deepseek_unavailable", true),
				slog.Int("batch_start", start),
				slog.Int("batch_size", len(batch)),
			)
			// Promote each raw item to a ProcessedArticle with zero AI fields.
			for i := range batch {
				articles = append(articles, models.ProcessedArticle{
					Raw:          &items[start+i],
					RunID:        runID,
					AgentVersion: "1.0.0",
					LLMSkipped:   true,
				})
			}
			continue
		}

		// Correlate results back to input items by URL.
		resultsByURL := make(map[string]models.AIItemResult, len(results))
		for _, r := range results {
			resultsByURL[r.URL] = r
		}

		for i := range batch {
			item := &items[start+i]
			article := models.ProcessedArticle{
				Raw:          item,
				RunID:        runID,
				AgentVersion: "1.0.0",
			}
			if r, ok := resultsByURL[item.URL]; ok {
				article.Category = r.Category
				article.Summary = r.Summary
				article.CredibilityScore = r.CredibilityScore
				article.Tags = r.Tags
				article.DetectedLanguage = r.Language
			} else {
				p.logger.Warn("no AI result for item; using zero values",
					slog.String("url", item.URL),
					slog.String("run_id", runID),
				)
				article.LLMSkipped = true
			}
			articles = append(articles, article)
		}
	}

	return articles, nil
}

// processBatchCall sends one batch of items to DeepSeek and returns parsed results.
// It retries once after deepSeekRetryWait on non-2xx responses.
func (p *Processor) processBatchCall(ctx context.Context, items []models.RawItem) ([]models.AIItemResult, error) {
	inputJSON, err := buildBatchInput(items)
	if err != nil {
		return nil, fmt.Errorf("build batch input: %w", err)
	}

	prompt := strings.Replace(batchPromptTemplate, "{{INPUT}}", inputJSON, 1)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(deepSeekRetryWait):
			}
		}

		resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: p.modelID,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
				{Role: openai.ChatMessageRoleUser, Content: prompt},
			},
			// response_format omitted — not universally supported by OpenAI-compatible APIs.
		})
		if err != nil {
			lastErr = err
			p.logger.Debug("deepseek call error; will retry",
				slog.Int("attempt", attempt+1),
				slog.String("error", err.Error()),
			)
			continue
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("empty choices in response")
			continue
		}

		raw := resp.Choices[0].Message.Content
		results, parseErr := parseBatchResponse(raw)
		if parseErr != nil {
			// Try individual item fallback
			p.logger.Debug("batch parse failed; trying individual items",
				slog.String("parse_error", parseErr.Error()),
			)
			return p.processBatchIndividually(ctx, items)
		}
		return results, nil
	}

	return nil, &LLMUnavailableError{Cause: lastErr}
}

// processBatchIndividually falls back to one API call per item in the batch.
func (p *Processor) processBatchIndividually(ctx context.Context, items []models.RawItem) ([]models.AIItemResult, error) {
	var results []models.AIItemResult
	for _, item := range items {
		single := []models.RawItem{item}
		inputJSON, err := buildBatchInput(single)
		if err != nil {
			continue
		}
		prompt := strings.Replace(batchPromptTemplate, "{{INPUT}}", inputJSON, 1)

		resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: p.modelID,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
				{Role: openai.ChatMessageRoleUser, Content: prompt},
			},
			// response_format omitted — not universally supported by OpenAI-compatible APIs.
		})
		if err != nil {
			p.logger.Warn("individual item deepseek call failed",
				slog.String("url", item.URL),
				slog.String("error", err.Error()),
			)
			continue
		}
		if len(resp.Choices) == 0 {
			continue
		}

		parsed, parseErr := parseBatchResponse(resp.Choices[0].Message.Content)
		if parseErr != nil {
			p.logger.Warn("individual item parse failed",
				slog.String("url", item.URL),
				slog.String("error", parseErr.Error()),
			)
			continue
		}
		results = append(results, parsed...)

		select {
		case <-ctx.Done():
			return results, ctx.Err()
		case <-time.After(interCallDelay):
		}
	}
	return results, nil
}

// ExtractTopic asks the LLM to identify the topic and most relevant category
// from a free-form user message (used by the chat handler).
//
// It retries once after deepSeekRetryWait on transport/API errors.
// response_format is intentionally omitted: many OpenAI-compatible endpoints
// (including the USTC LLM platform) reject that parameter with a 4xx error.
// Instead, the prompt explicitly requests JSON, and extractJSON strips any
// accidental markdown code-fence wrappers from the response.
func (p *Processor) ExtractTopic(ctx context.Context, message string) (TopicResult, error) {
	prompt := strings.Replace(topicExtractionPromptTemplate, "{{MESSAGE}}", message, 1)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return TopicResult{}, ctx.Err()
			case <-time.After(deepSeekRetryWait):
			}
		}

		resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: p.modelID,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: "You are a helpful assistant. Output ONLY valid JSON — no markdown, no explanation."},
				{Role: openai.ChatMessageRoleUser, Content: prompt},
			},
			// response_format is omitted — see function comment.
		})
		if err != nil {
			lastErr = err
			p.logger.Debug("extract topic call error; will retry",
				slog.Int("attempt", attempt+1),
				slog.String("error", err.Error()),
			)
			continue
		}
		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("empty choices in response")
			continue
		}

		raw := extractJSON(resp.Choices[0].Message.Content)

		var parsed struct {
			Category string   `json:"category"`
			Keywords []string `json:"keywords"`
			Summary  string   `json:"summary"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			// Parse errors are not retried — the same model would return the
			// same malformed output. Return immediately.
			return TopicResult{}, &LLMParseError{Raw: raw, Cause: err}
		}

		cat := models.Category(parsed.Category)
		if !cat.IsValid() {
			cat = models.CategoryTechAI // sensible default
		}

		return TopicResult{
			Category: cat,
			Keywords: parsed.Keywords,
			Summary:  parsed.Summary,
		}, nil
	}

	return TopicResult{}, &LLMUnavailableError{Cause: lastErr}
}

// -----------------------------------------------------------------------
// Helper types and functions
// -----------------------------------------------------------------------

// batchInputItem is the trimmed representation of a RawItem sent to DeepSeek.
type batchInputItem struct {
	URL          string `json:"url"`
	SourceDomain string `json:"source_domain"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Content      string `json:"content,omitempty"`
	Language     string `json:"language,omitempty"`
}

func buildBatchInput(items []models.RawItem) (string, error) {
	batch := make([]batchInputItem, len(items))
	for i, item := range items {
		content := item.Content
		if len(content) > maxContentLen {
			content = content[:maxContentLen]
		}
		batch[i] = batchInputItem{
			URL:          item.URL,
			SourceDomain: item.SourceDomain,
			Title:        item.Title,
			Description:  item.Description,
			Content:      content,
			Language:     item.Language,
		}
	}
	data, err := json.Marshal(batch)
	if err != nil {
		return "", fmt.Errorf("marshal batch input: %w", err)
	}
	return string(data), nil
}

// extractJSON strips markdown code fences and advances to the first JSON
// container character ({ or [) so that callers can pass the raw LLM response
// directly to json.Unmarshal even when the model wraps its output in
// ```json ... ``` blocks or adds leading prose.
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	// Strip opening ``` fence (with optional language tag, e.g. ```json).
	if strings.HasPrefix(raw, "```") {
		if idx := strings.Index(raw, "\n"); idx != -1 {
			raw = raw[idx+1:]
		}
		// Strip trailing ``` fence.
		if idx := strings.LastIndex(raw, "```"); idx != -1 {
			raw = strings.TrimSpace(raw[:idx])
		}
	}

	// Advance to the first JSON container character.
	if start := strings.IndexAny(raw, "{["); start > 0 {
		raw = raw[start:]
	}

	// Trim any trailing garbage after the last closing character.
	if end := strings.LastIndexAny(raw, "]}"); end != -1 && end < len(raw)-1 {
		raw = raw[:end+1]
	}

	return strings.TrimSpace(raw)
}

// parseBatchResponse parses the AI JSON response into []AIItemResult.
// The response may be a JSON array directly, or a JSON object wrapping an array.
// Markdown code fences and leading/trailing prose are stripped via extractJSON.
func parseBatchResponse(raw string) ([]models.AIItemResult, error) {
	raw = extractJSON(strings.TrimSpace(raw))

	// Try direct array first
	var results []models.AIItemResult
	if err := json.Unmarshal([]byte(raw), &results); err == nil {
		return results, nil
	}

	// Try wrapped object: {"items": [...]} or {"results": [...]}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, &LLMParseError{Raw: raw, Cause: fmt.Errorf("not an array or object: %w", err)}
	}

	for _, key := range []string{"items", "results", "data", "articles"} {
		if arr, ok := wrapper[key]; ok {
			if err := json.Unmarshal(arr, &results); err == nil {
				return results, nil
			}
		}
	}

	return nil, &LLMParseError{Raw: raw, Cause: fmt.Errorf("could not locate array in response")}
}

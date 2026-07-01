package processor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// chatCompletionResponse builds the minimal JSON envelope go-openai expects.
func chatCompletionResponse(content string) []byte {
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
			"prompt_tokens":     100,
			"completion_tokens": 50,
			"total_tokens":      150,
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

// openAIErrorResponse builds a non-2xx JSON error body go-openai understands.
func openAIErrorResponse(message string) []byte {
	body := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "server_error",
			"code":    "internal_server_error",
		},
	}
	data, _ := json.Marshal(body)
	return data
}

// buildAIResults constructs a JSON array of AIItemResult for the given items.
func buildAIResults(items []models.RawItem) string {
	results := make([]models.AIItemResult, len(items))
	for i, item := range items {
		results[i] = models.AIItemResult{
			URL:              item.URL,
			Category:         models.CategoryTechAI,
			Summary:          fmt.Sprintf("AI摘要：%s", item.Title),
			CredibilityScore: 0.85,
			Tags:             []string{"technology", "AI"},
			Language:         "en",
		}
	}
	data, _ := json.Marshal(results)
	return string(data)
}

// newMockLLMServer creates an httptest.Server that handles /chat/completions.
// The handler function receives the request and writes the response.
func newMockLLMServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newProcessor creates a Processor pointed at the given base URL.
func newProcessor(t *testing.T, baseURL string) *processor.Processor {
	t.Helper()
	client := processor.NewLLMClient("test-api-key", baseURL)
	return processor.New(client, "deepseek-chat", slog.Default())
}

func makeRawItem(url, title, domain string) models.RawItem {
	return models.RawItem{
		URL:          url,
		SourceDomain: domain,
		SourceType:   models.SourceTypeRSS,
		Title:        title,
		Description:  "Description for " + title,
		PublishedAt:  time.Now().UTC(),
		FetchedAt:    time.Now().UTC(),
		Language:     "en",
	}
}

// ---------------------------------------------------------------------------
// ProcessBatch — success path
// ---------------------------------------------------------------------------

func TestProcessor_ProcessBatch_SuccessfulCategorisationAndSummarisation(t *testing.T) {
	items := []models.RawItem{
		makeRawItem("http://reuters.com/tech/1", "OpenAI releases GPT-5", "reuters.com"),
		makeRawItem("http://bbc.com/finance/1", "Markets hit record high", "bbc.com"),
	}

	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		aiJSON := buildAIResults(items)
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionResponse(aiJSON))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), items, "test-run-1")

	require.NoError(t, err)
	require.Len(t, articles, 2)

	// Verify AI fields are populated.
	assert.Equal(t, models.CategoryTechAI, articles[0].Category)
	assert.NotEmpty(t, articles[0].Summary)
	assert.InDelta(t, 0.85, articles[0].CredibilityScore, 0.001)
	assert.NotEmpty(t, articles[0].Tags)

	// Verify run provenance.
	assert.Equal(t, "test-run-1", articles[0].RunID)
	assert.NotNil(t, articles[0].Raw)
}

func TestProcessor_ProcessBatch_CorrectURLCorrelation(t *testing.T) {
	items := []models.RawItem{
		makeRawItem("http://example.com/a", "Article A", "example.com"),
		makeRawItem("http://example.com/b", "Article B", "example.com"),
	}

	// Return results with explicit category per URL.
	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		content := `[
			{"url":"http://example.com/a","category":"金融","summary":"金融摘要","credibility_score":0.9,"tags":["finance"],"language":"en"},
			{"url":"http://example.com/b","category":"科技/AI","summary":"科技摘要","credibility_score":0.7,"tags":["tech"],"language":"en"}
		]`
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionResponse(content))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), items, "run-correlation")
	require.NoError(t, err)
	require.Len(t, articles, 2)

	// Correlate by URL.
	byURL := make(map[string]models.ProcessedArticle)
	for _, a := range articles {
		byURL[a.Raw.URL] = a
	}

	assert.Equal(t, models.CategoryFinance, byURL["http://example.com/a"].Category)
	assert.Equal(t, models.CategoryTechAI, byURL["http://example.com/b"].Category)
}

func TestProcessor_ProcessBatch_WrappedResponseObject_ParsedSuccessfully(t *testing.T) {
	items := []models.RawItem{
		makeRawItem("http://example.com/wrapped", "Wrapped Response Test", "example.com"),
	}

	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		// AI returns a {"results": [...]} wrapper instead of a bare array.
		content := `{"results":[{"url":"http://example.com/wrapped","category":"经济","summary":"经济摘要","credibility_score":0.75,"tags":["economy"],"language":"en"}]}`
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionResponse(content))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), items, "run-wrapped")
	require.NoError(t, err)
	require.Len(t, articles, 1)
	assert.Equal(t, models.CategoryEconomy, articles[0].Category)
}

func TestProcessor_ProcessBatch_EmptyItems_ReturnsNil(t *testing.T) {
	proc := newProcessor(t, "http://unused.invalid")

	articles, err := proc.ProcessBatch(context.Background(), nil, "run-empty")
	require.NoError(t, err)
	assert.Nil(t, articles)
}

func TestProcessor_ProcessBatch_RawItemPreservedInResult(t *testing.T) {
	item := makeRawItem("http://source.example.com/item", "Source Item", "source.example.com")

	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionResponse(buildAIResults([]models.RawItem{item})))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), []models.RawItem{item}, "run-raw")
	require.NoError(t, err)
	require.Len(t, articles, 1)

	assert.Equal(t, item.URL, articles[0].Raw.URL)
	assert.Equal(t, item.Title, articles[0].Raw.Title)
	assert.Equal(t, item.SourceDomain, articles[0].Raw.SourceDomain)
}

// ---------------------------------------------------------------------------
// ProcessBatch — error / degraded path
// ---------------------------------------------------------------------------

func TestProcessor_ProcessBatch_ServerError500_DegradeGracefully(t *testing.T) {
	// NOTE: This test exercises 2 retry attempts with a 2-second delay between
	// them (deepSeekRetryWait constant in the processor package). The test will
	// take approximately 2 seconds to complete — this is expected behaviour.
	items := []models.RawItem{
		makeRawItem("http://example.com/error-item", "Error Item", "example.com"),
	}

	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(openAIErrorResponse("internal server error"))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), items, "run-error")

	// ProcessBatch degrades gracefully — returns articles with zero AI fields, no error.
	require.NoError(t, err)
	require.Len(t, articles, 1)

	// Zero AI fields indicate degraded mode.
	assert.Equal(t, models.Category(""), articles[0].Category)
	assert.Equal(t, "", articles[0].Summary)
	assert.Equal(t, float64(0), articles[0].CredibilityScore)

	// Raw item and RunID are still populated.
	assert.NotNil(t, articles[0].Raw)
	assert.Equal(t, "run-error", articles[0].RunID)
}

func TestProcessor_ProcessBatch_MalformedJSONContent_DegradeGracefully(t *testing.T) {
	items := []models.RawItem{
		makeRawItem("http://example.com/malformed", "Malformed JSON Item", "example.com"),
	}

	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Return a valid HTTP 200 with valid OpenAI structure, but the content
		// field itself is not parseable as AIItemResult.
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionResponse("this is not json at all"))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), items, "run-malformed")

	// Processor falls back to individual processing, which also fails, then
	// returns articles with zero AI fields (no fatal error).
	require.NoError(t, err)
	require.Len(t, articles, 1)
	assert.Equal(t, models.Category(""), articles[0].Category)
}

// ---------------------------------------------------------------------------
// ProcessBatch — code-fence stripping
// ---------------------------------------------------------------------------

func TestProcessor_ProcessBatch_CodeFencedJSONArray_ParsedSuccessfully(t *testing.T) {
	items := []models.RawItem{
		makeRawItem("http://example.com/fenced", "Code Fence Article", "example.com"),
	}

	srv := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		// The LLM wraps the JSON array in a markdown code fence.
		content := "```json\n[{\"url\":\"http://example.com/fenced\",\"category\":\"经济\",\"summary\":\"Economy summary\",\"credibility_score\":0.8,\"tags\":[\"economy\"],\"language\":\"en\"}]\n```"
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatCompletionResponse(content))
	})

	proc := newProcessor(t, srv.URL)
	articles, err := proc.ProcessBatch(context.Background(), items, "run-fenced")

	require.NoError(t, err)
	require.Len(t, articles, 1)
	assert.Equal(t, models.CategoryEconomy, articles[0].Category)
	assert.Equal(t, "Economy summary", articles[0].Summary)
}

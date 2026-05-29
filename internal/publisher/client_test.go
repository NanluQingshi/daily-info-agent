package publisher_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeProcessedArticle(url, title, domain, category string, score float64) models.ProcessedArticle {
	now := time.Now().UTC()
	return models.ProcessedArticle{
		Raw: &models.RawItem{
			URL:          url,
			Title:        title,
			SourceDomain: domain,
			PublishedAt:  now,
			FetchedAt:    now,
		},
		Category:         models.Category(category),
		Summary:          "Test summary for " + title,
		CredibilityScore: score,
		Tags:             []string{"test", "news"},
		DetectedLanguage: "en",
		RunID:            "run-test-001",
		AgentVersion:     "1.0.0",
	}
}

// successResponse writes a 201 with a PublishResponse body.
func successResponse(w http.ResponseWriter, remoteID int64, sourceURL string) {
	resp := models.PublishResponse{
		ID:        remoteID,
		SourceURL: sourceURL,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Status:    "published",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func newPublisherClient(t *testing.T, srv *httptest.Server) *publisher.Client {
	t.Helper()
	return publisher.New(srv.URL, "test-bearer-token", srv.Client(), slog.Default())
}

// ---------------------------------------------------------------------------
// Successful publish
// ---------------------------------------------------------------------------

func TestPublisher_Publish_Success_ReturnsPublishedOutcome(t *testing.T) {
	article := makeProcessedArticle(
		"http://reuters.com/article/1",
		"Reuters Article",
		"reuters.com",
		"金融",
		0.95,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successResponse(w, 42, article.Raw.URL)
	}))
	defer srv.Close()

	client := newPublisherClient(t, srv)
	result := client.Publish(context.Background(), article, "run-001")

	assert.Equal(t, publisher.OutcomePublished, result.Outcome)
	assert.Equal(t, int64(42), result.RemoteID)
	assert.Nil(t, result.Err)
	assert.Equal(t, http.StatusCreated, result.StatusCode)
	assert.Equal(t, 1, result.Attempts)
}

func TestPublisher_Publish_Success_CorrectJSONBodySent(t *testing.T) {
	article := makeProcessedArticle(
		"http://bbc.com/news/world-1",
		"BBC World News",
		"bbc.com",
		"国际",
		0.9,
	)

	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		successResponse(w, 100, article.Raw.URL)
	}))
	defer srv.Close()

	client := newPublisherClient(t, srv)
	result := client.Publish(context.Background(), article, "run-body-check")
	require.Equal(t, publisher.OutcomePublished, result.Outcome)

	var req models.PublishRequest
	err := json.Unmarshal(capturedBody, &req)
	require.NoError(t, err)

	assert.Equal(t, article.Raw.URL, req.SourceURL)
	assert.Equal(t, article.Raw.Title, req.Title)
	assert.Equal(t, article.Summary, req.Summary)
	assert.Equal(t, string(article.Category), req.Category)
	assert.Equal(t, article.Raw.SourceDomain, req.SourceDomain)
	assert.InDelta(t, article.CredibilityScore, req.CredibilityScore, 0.001)
	assert.NotEmpty(t, req.PublishedAt)
	assert.NotEmpty(t, req.FetchedAt)
	assert.Equal(t, "run-body-check", req.RunID)
}

func TestPublisher_Publish_Success_AuthorizationHeaderSent(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		successResponse(w, 1, "http://example.com/1")
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/1", "Test", "example.com", "科技/AI", 0.8)
	client := newPublisherClient(t, srv)
	client.Publish(context.Background(), article, "run-auth")

	assert.Equal(t, "Bearer test-bearer-token", capturedAuth)
}

func TestPublisher_Publish_Success_POSTsToCorrectPath(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod := r.Method
		assert.Equal(t, http.MethodPost, capturedMethod)
		successResponse(w, 1, "http://example.com/1")
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/1", "Test", "example.com", "科技/AI", 0.8)
	client := newPublisherClient(t, srv)
	client.Publish(context.Background(), article, "run-path")

	assert.Equal(t, "/api/agent/articles", capturedPath)
}

// ---------------------------------------------------------------------------
// Duplicate (409)
// ---------------------------------------------------------------------------

func TestPublisher_Publish_Conflict409_ReturnsDuplicateOutcome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(models.PublishErrorResponse{
			Error:      "duplicate",
			Message:    "article already exists",
			ExistingID: 77,
		})
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/dup", "Duplicate", "example.com", "经济", 0.8)
	client := newPublisherClient(t, srv)
	result := client.Publish(context.Background(), article, "run-dup")

	assert.Equal(t, publisher.OutcomeDuplicate, result.Outcome)
	assert.Nil(t, result.Err)
	assert.Equal(t, http.StatusConflict, result.StatusCode)
}

// ---------------------------------------------------------------------------
// Non-retryable 4xx error
// ---------------------------------------------------------------------------

func TestPublisher_Publish_BadRequest400_PermanentFail_NoRetry(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.PublishErrorResponse{
			Error:   "validation_error",
			Message: "source_url is required",
			Field:   "source_url",
		})
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/bad", "Bad Article", "example.com", "政治", 0.7)
	client := newPublisherClient(t, srv)
	result := client.Publish(context.Background(), article, "run-bad")

	assert.Equal(t, publisher.OutcomePermanentFail, result.Outcome)
	assert.NotNil(t, result.Err)

	// Must NOT retry on 400 — exactly one attempt.
	assert.Equal(t, int32(1), callCount.Load(), "400 error must not be retried")
}

func TestPublisher_Publish_Forbidden403_PermanentFail_NoRetry(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/forbidden", "Forbidden", "example.com", "政治", 0.7)
	client := newPublisherClient(t, srv)
	result := client.Publish(context.Background(), article, "run-403")

	assert.Equal(t, publisher.OutcomePermanentFail, result.Outcome)
	assert.Equal(t, int32(1), callCount.Load())
}

// ---------------------------------------------------------------------------
// Retry logic (5xx)
// ---------------------------------------------------------------------------

func TestPublisher_Publish_ServerError500TwiceThen201_Succeeds(t *testing.T) {
	// NOTE: This test exercises exponential backoff — delays of 1s then 2s
	// between attempts mean it takes approximately 3 seconds to complete.
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"server_error","message":"temporarily unavailable"}`)
			return
		}
		// Third attempt succeeds.
		successResponse(w, 200, "http://example.com/retry")
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/retry", "Retry Article", "example.com", "金融", 0.9)
	client := newPublisherClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := client.Publish(ctx, article, "run-retry")

	assert.Equal(t, publisher.OutcomePublished, result.Outcome)
	assert.Equal(t, int32(3), callCount.Load())
	assert.Equal(t, 3, result.Attempts)
}

func TestPublisher_Publish_AllAttemptsFail_MaxRetriesHit(t *testing.T) {
	// NOTE: 3 attempts with exponential backoff — approximately 3 seconds total.
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"server_error","message":"persistent failure"}`)
	}))
	defer srv.Close()

	article := makeProcessedArticle("http://example.com/fail", "Failing Article", "example.com", "国际", 0.8)
	client := newPublisherClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := client.Publish(ctx, article, "run-all-fail")

	assert.Equal(t, publisher.OutcomeMaxRetriesHit, result.Outcome)
	assert.NotNil(t, result.Err)
	assert.Equal(t, int32(3), callCount.Load(), "should attempt exactly maxPublishAttempts=3 times")
}

// ---------------------------------------------------------------------------
// New — constructor
// ---------------------------------------------------------------------------

func TestPublisher_New_NilHTTPClient_UsesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successResponse(w, 1, "http://example.com/1")
	}))
	defer srv.Close()

	// nil httpClient should not panic; uses internal default.
	client := publisher.New(srv.URL, "token", nil, slog.Default())
	require.NotNil(t, client)

	article := makeProcessedArticle("http://example.com/1", "Test", "example.com", "科技/AI", 0.8)
	result := client.Publish(context.Background(), article, "run-nil-client")
	assert.Equal(t, publisher.OutcomePublished, result.Outcome)
}

func TestPublisher_New_TrailingSlashInBaseURL_NormalisedCorrectly(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		successResponse(w, 1, "http://example.com/1")
	}))
	defer srv.Close()

	// Pass URL with trailing slash — should not result in double slash in path.
	client := publisher.New(srv.URL+"/", "token", srv.Client(), slog.Default())
	article := makeProcessedArticle("http://example.com/1", "Test", "example.com", "科技/AI", 0.8)
	client.Publish(context.Background(), article, "run-slash")

	assert.Equal(t, "/api/agent/articles", capturedPath)
}

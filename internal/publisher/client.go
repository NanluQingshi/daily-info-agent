// Package publisher posts processed articles to the Java website API.
package publisher

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

	"github.com/user/daily-info-agent/pkg/backoff"
	"github.com/user/daily-info-agent/pkg/models"
)

const (
	publishPath         = "/api/agent/articles"
	maxPublishAttempts  = 3
	publishBaseDelay    = time.Second
	interPublishDelay   = 100 * time.Millisecond // rate-limit courtesy
)

// PublishOutcome is a machine-readable result code.
type PublishOutcome string

const (
	OutcomePublished     PublishOutcome = "published"
	OutcomeDuplicate     PublishOutcome = "duplicate"      // HTTP 409
	OutcomePermanentFail PublishOutcome = "permanent_fail" // 4xx (non-409)
	OutcomeMaxRetriesHit PublishOutcome = "max_retries_hit" // 5xx after 3 attempts
)

// PublishResult describes the outcome of a single publish attempt.
type PublishResult struct {
	Outcome    PublishOutcome
	ArticleURL string
	Attempts   int
	StatusCode int    // final HTTP status code; 0 on network error
	RemoteID   int64  // populated on OutcomePublished
	Err        error
}

// PublishHTTPError is a typed error for non-retryable HTTP failures.
type PublishHTTPError struct {
	StatusCode int
	Body       string
	URL        string
	Attempt    int
}

func (e *PublishHTTPError) Error() string {
	return fmt.Sprintf("publish http error status=%d url=%q attempt=%d body=%q",
		e.StatusCode, e.URL, e.Attempt, e.Body)
}

// Client posts articles to the Java website API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a Client for the given base URL, auth token, and HTTP client.
func New(baseURL, token string, httpClient *http.Client, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Publish sends a single ProcessedArticle to the website API.
// Implements 3-attempt exponential backoff for 5xx and network errors.
func (c *Client) Publish(ctx context.Context, article models.ProcessedArticle, runID string) PublishResult {
	req := articleToPublishRequest(article, runID)
	articleURL := req.SourceURL

	var result PublishResult
	result.ArticleURL = articleURL

	err := backoff.Retry(ctx, maxPublishAttempts, publishBaseDelay, func() error {
		result.Attempts++

		statusCode, remoteID, err := c.doPublish(ctx, req, result.Attempts)
		result.StatusCode = statusCode

		c.logger.Info("publish attempt",
			slog.String("url", articleURL),
			slog.Int("attempt", result.Attempts),
			slog.Int("status", statusCode),
			slog.String("run_id", runID),
		)

		if err != nil {
			// Network-level error — retryable
			result.Err = err
			return &backoff.RetryableError{Cause: err}
		}

		switch {
		case statusCode >= 200 && statusCode < 300:
			result.Outcome = OutcomePublished
			result.RemoteID = remoteID
			c.logger.Info("article published",
				slog.String("url", articleURL),
				slog.Int64("remote_id", remoteID),
				slog.String("run_id", runID),
			)
			return nil // success

		case statusCode == http.StatusConflict:
			result.Outcome = OutcomeDuplicate
			c.logger.Info("article already published",
				slog.String("url", articleURL),
				slog.Bool("already_published", true),
				slog.String("run_id", runID),
			)
			return nil // not an error; don't retry

		case statusCode >= 400 && statusCode < 500:
			// Client error — permanent failure, do not retry
			result.Outcome = OutcomePermanentFail
			result.Err = &PublishHTTPError{StatusCode: statusCode, URL: articleURL, Attempt: result.Attempts}
			c.logger.Error("permanent publish failure",
				slog.String("url", articleURL),
				slog.Int("status", statusCode),
				slog.String("run_id", runID),
			)
			return result.Err // non-RetryableError stops retries

		default:
			// 5xx or unexpected — retryable
			httpErr := &PublishHTTPError{StatusCode: statusCode, URL: articleURL, Attempt: result.Attempts}
			result.Err = httpErr
			c.logger.Warn("server error; will retry",
				slog.String("url", articleURL),
				slog.Int("status", statusCode),
				slog.Int("attempt", result.Attempts),
				slog.String("run_id", runID),
			)
			return &backoff.RetryableError{Cause: httpErr}
		}
	})

	if err != nil && result.Outcome == "" {
		result.Outcome = OutcomeMaxRetriesHit
		result.Err = err
		c.logger.Error("max retries hit for publish",
			slog.String("url", articleURL),
			slog.Int("attempts", result.Attempts),
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
	}

	return result
}

// doPublish executes one HTTP POST and returns (statusCode, remoteID, networkErr).
// remoteID is non-zero only on HTTP 2xx.
func (c *Client) doPublish(ctx context.Context, req models.PublishRequest, attempt int) (int, int64, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal publish request: %w", err)
	}

	endpoint := c.baseURL + publishPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, 0, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, 0, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var pubResp models.PublishResponse
		if jsonErr := json.Unmarshal(respBody, &pubResp); jsonErr == nil {
			return resp.StatusCode, pubResp.ID, nil
		}
	}

	return resp.StatusCode, 0, nil
}

// articleToPublishRequest converts a ProcessedArticle into the wire format.
func articleToPublishRequest(a models.ProcessedArticle, runID string) models.PublishRequest {
	return models.PublishRequest{
		SourceURL:        a.Raw.URL,
		Title:            a.Raw.Title,
		Summary:          a.Summary,
		Category:         string(a.Category),
		SourceDomain:     a.Raw.SourceDomain,
		CredibilityScore: a.CredibilityScore,
		PublishedAt:      a.Raw.PublishedAt.UTC().Format(time.RFC3339),
		FetchedAt:        a.Raw.FetchedAt.UTC().Format(time.RFC3339),
		RunID:            runID,
		Tags:             a.Tags,
		Language:         a.DetectedLanguage,
		AgentVersion:     a.AgentVersion,
	}
}

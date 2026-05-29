// Package chat implements the Echo HTTP handler for POST /api/chat.
package chat

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/internal/processor"
	"github.com/user/daily-info-agent/internal/verifier"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

const (
	maxMessageLen  = 500
	maxFetchItems  = 20
	maxSources     = 5
)

// Handler implements the Echo handler for POST /api/chat.
type Handler struct {
	proc   *processor.Processor
	mgr    *fetcher.Manager
	ver    *verifier.Verifier
	cfg    *config.Config
	logger *slog.Logger
}

// New creates a Handler wired to the processor, fetcher manager, and verifier.
func New(
	proc *processor.Processor,
	mgr *fetcher.Manager,
	ver *verifier.Verifier,
	cfg *config.Config,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		proc:   proc,
		mgr:    mgr,
		ver:    ver,
		cfg:    cfg,
		logger: logger,
	}
}

// Handle is the Echo HandlerFunc registered at POST /api/chat.
// It validates the request, extracts a topic via DeepSeek, fetches relevant
// news items, processes them with AI, and returns a ChatResponse.
func (h *Handler) Handle(c echo.Context) error {
	reqStart := time.Now()

	var req models.ChatRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "validation_error",
			Message: "invalid request body",
		})
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "validation_error",
			Message: "message is required",
		})
	}
	if len(req.Message) > maxMessageLen {
		return c.JSON(http.StatusBadRequest, models.ChatErrorResponse{
			Error:   "message_too_long",
			Message: "message must not exceed 500 characters",
		})
	}

	ctx := c.Request().Context()

	h.logger.Info("chat request received",
		slog.String("message_preview", truncate(req.Message, 80)),
	)

	// Step 1: Extract topic/intent from the user's message.
	topicResult, err := h.proc.ExtractTopic(ctx, req.Message)
	if err != nil {
		h.logger.Error("failed to extract topic",
			slog.String("error", err.Error()),
		)
		return c.JSON(http.StatusInternalServerError, models.ChatErrorResponse{
			Error:   "internal_error",
			Message: "failed to understand the request topic",
		})
	}

	h.logger.Info("topic extracted",
		slog.String("category", string(topicResult.Category)),
		slog.String("keywords", strings.Join(topicResult.Keywords, ",")),
	)

	// Step 2: Fetch news items for the extracted keywords.
	items, err := h.mgr.FetchForTopic(ctx, topicResult.Keywords, maxFetchItems)
	if err != nil {
		h.logger.Warn("fetch for topic failed; returning empty sources",
			slog.String("error", err.Error()),
		)
		items = nil // degrade gracefully — return empty result
	}

	// Step 3: AI-process fetched items.
	var articles []models.ProcessedArticle
	if len(items) > 0 {
		articles, err = h.proc.ProcessBatch(ctx, items, "chat")
		if err != nil {
			h.logger.Warn("process batch failed in chat mode",
				slog.String("error", err.Error()),
			)
		}
	}

	// Step 4: Apply verifier — conversational mode still filters by credibility.
	articles = h.ver.Verify(articles)

	var passing []models.ProcessedArticle
	for _, a := range articles {
		if a.Verification.Pass {
			passing = append(passing, a)
		}
	}

	// Step 5: Sort by credibility score (descending) and take top N.
	sort.Slice(passing, func(i, j int) bool {
		return passing[i].CredibilityScore > passing[j].CredibilityScore
	})
	if len(passing) > maxSources {
		passing = passing[:maxSources]
	}

	// Step 6: Build aggregate summary and source list.
	summary := buildAggregateSummary(topicResult, passing)
	sources := buildSources(passing)

	resp := models.ChatResponse{
		ExtractedTopic: topicResult.Summary,
		Category:       string(topicResult.Category),
		Summary:        summary,
		Sources:        sources,
		FetchedAt:      time.Now().UTC().Format(time.RFC3339),
		LatencyMs:      time.Since(reqStart).Milliseconds(),
	}

	h.logger.Info("chat response ready",
		slog.Int("sources", len(sources)),
		slog.Int64("latency_ms", resp.LatencyMs),
	)

	return c.JSON(http.StatusOK, resp)
}

// buildAggregateSummary creates a Chinese summary from the top articles.
// If no articles are available, returns a polite message.
func buildAggregateSummary(topic processor.TopicResult, articles []models.ProcessedArticle) string {
	if len(articles) == 0 {
		return "暂时没有找到相关新闻，请稍后再试。"
	}

	var sb strings.Builder
	sb.WriteString("关于「")
	sb.WriteString(topic.Summary)
	sb.WriteString("」的最新动态：")

	for i, a := range articles {
		if i >= 3 {
			break
		}
		if a.Summary != "" {
			sb.WriteString(a.Summary)
			if i < len(articles)-1 && i < 2 {
				sb.WriteString(" ")
			}
		}
	}

	return sb.String()
}

// buildSources converts passing articles into ChatSource structs.
func buildSources(articles []models.ProcessedArticle) []models.ChatSource {
	sources := make([]models.ChatSource, 0, len(articles))
	for _, a := range articles {
		sources = append(sources, models.ChatSource{
			URL:          a.Raw.URL,
			Title:        a.Raw.Title,
			SourceDomain: a.Raw.SourceDomain,
			CredScore:    a.CredibilityScore,
		})
	}
	return sources
}

// truncate returns at most n runes of s.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

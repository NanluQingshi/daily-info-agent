// Package extract fetches original article pages and extracts readable full
// text via go-readability. Extraction is best-effort: any failure degrades
// gracefully to the feed-provided description/content.
package extract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/go-shiori/go-readability"
	"github.com/user/daily-info-agent/pkg/metrics"
	"github.com/user/daily-info-agent/pkg/models"
)

const (
	// DefaultConcurrency is the number of pages fetched in parallel.
	DefaultConcurrency = 4
	// DefaultMaxItems caps how many pages a single run will fetch, keeping
	// run duration and outbound traffic bounded.
	DefaultMaxItems = 20
	// DefaultMinLength is the minimum extracted text length (in runes) for
	// the result to be accepted. Shorter output usually means a paywall,
	// JS-only page, or cookie wall — better to fall back to the summary.
	DefaultMinLength = 200
	// maxBodySize caps how many bytes of the original page are read (2 MiB).
	maxBodySize = 2 << 20
	// defaultTimeout bounds a single page fetch.
	defaultTimeout = 15 * time.Second
	// maxStoreLen truncates extracted text stored in the database (runes).
	maxStoreLen = 50_000
)

// Extractor fetches original article pages and fills RawItem.ContentText.
type Extractor struct {
	client      *http.Client
	logger      *slog.Logger
	concurrency int
	maxItems    int
	minLen      int
}

// New creates an Extractor reusing the given HTTP client (typically the
// shared fetcher client, which already carries the project User-Agent).
// maxItems <= 0 falls back to DefaultMaxItems; concurrency <= 1 uses 1.
func New(client *http.Client, maxItems, concurrency int, logger *slog.Logger) *Extractor {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	if maxItems <= 0 {
		maxItems = DefaultMaxItems
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Extractor{
		client:      client,
		logger:      logger,
		concurrency: concurrency,
		maxItems:    maxItems,
		minLen:      DefaultMinLength,
	}
}

// Enrich fetches original pages for items that lack full text and fills
// item.ContentText in place. Items whose feed Content is already long enough
// are skipped, as are items beyond maxItems. Returns the number of items
// successfully enriched. Failures are logged and counted, never returned.
func (x *Extractor) Enrich(ctx context.Context, items []models.RawItem) int {
	type job struct {
		idx int
		url string
	}
	var jobs []job
	for i := range items {
		if utf8.RuneCountInString(strings.TrimSpace(items[i].Content)) >= x.minLen {
			continue // feed already carried usable full text
		}
		if strings.TrimSpace(items[i].URL) == "" {
			continue
		}
		jobs = append(jobs, job{idx: i, url: items[i].URL})
		if len(jobs) >= x.maxItems {
			break
		}
	}
	if len(jobs) == 0 {
		return 0
	}

	var okCount atomic.Int64
	sem := make(chan struct{}, x.concurrency)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			// Stop launching new fetches once the run is cancelled.
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			text, err := x.extractOne(ctx, j.url)
			if err != nil {
				metrics.App.ExtractFailed.Add(1)
				x.logger.Debug("fulltext extraction failed; keeping feed content",
					slog.String("url", j.url),
					slog.String("error", err.Error()),
				)
				return
			}
			items[j.idx].ContentText = text
			okCount.Add(1)
			metrics.App.ItemsExtracted.Add(1)
		}(j)
	}
	wg.Wait()
	return int(okCount.Load())
}

// extractOne fetches a single URL and returns its readable text.
func (x *Extractor) extractOne(ctx context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := x.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch page: http %d", resp.StatusCode)
	}

	article, err := readability.FromReader(io.LimitReader(resp.Body, maxBodySize), parsed)
	if err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}

	text := strings.TrimSpace(article.TextContent)
	if utf8.RuneCountInString(text) < x.minLen {
		return "", fmt.Errorf("extracted text too short (%d runes; likely paywall or JS-only page)", utf8.RuneCountInString(text))
	}

	// Truncate pathological pages so a single article cannot dominate the row.
	if runes := []rune(text); len(runes) > maxStoreLen {
		text = string(runes[:maxStoreLen])
	}
	return text, nil
}

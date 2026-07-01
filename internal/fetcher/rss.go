package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mmcdole/gofeed"
	"github.com/user/daily-info-agent/pkg/models"
)

// rssFetcher implements Fetcher using the gofeed library.
type rssFetcher struct {
	httpClient *http.Client
}

// NewRSSFetcher constructs a Fetcher backed by gofeed for RSS 2.0 / Atom feeds.
// If httpClient is nil, a default client with a 10-second timeout is used.
func NewRSSFetcher(httpClient *http.Client) Fetcher {
	if httpClient == nil {
		httpClient = newHTTPClient(defaultFetchTimeout)
	}
	return &rssFetcher{httpClient: httpClient}
}

func (r *rssFetcher) Name() string { return "rss" }

// Fetch retrieves and parses an RSS or Atom feed at cfg.URL.
// It respects cfg.Timeout (falls back to 10 s). Each returned RawItem is
// populated from the gofeed Item; SourceDomain is extracted from the feed URL.
func (r *rssFetcher) Fetch(ctx context.Context, cfg models.FetchConfig) ([]models.RawItem, error) {
	feedURL := cfg.URL
	if feedURL == "" {
		return nil, &FetchError{Source: r.Name(), URL: feedURL, Wrapped: fmt.Errorf("empty feed URL")}
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultFetchTimeout
	}

	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fp := gofeed.NewParser()
	fp.Client = r.httpClient

	feed, err := fp.ParseURLWithContext(feedURL, fetchCtx)
	if err != nil {
		return nil, &FetchError{Source: r.Name(), URL: feedURL, Wrapped: fmt.Errorf("parse feed: %w", err)}
	}

	domain := extractDomain(feedURL)
	now := time.Now().UTC()

	items := make([]models.RawItem, 0, len(feed.Items))
	for _, item := range feed.Items {
		if item == nil {
			continue
		}

		itemURL := item.Link
		if itemURL == "" {
			// Some feeds put the GUID as a link
			itemURL = item.GUID
		}
		if itemURL == "" {
			continue // skip items with no addressable URL
		}

		published := now
		if item.PublishedParsed != nil {
			published = item.PublishedParsed.UTC()
		} else if item.UpdatedParsed != nil {
			published = item.UpdatedParsed.UTC()
		}

		description := item.Description
		if description == "" {
			description = item.Content
		}

		content := item.Content
		if len(content) > 2000 {
			content = truncateAtRuneBoundary(content, 2000)
		}

		lang := feed.Language
		if lang == "" {
			lang = "en"
		}

		items = append(items, models.RawItem{
			URL:          itemURL,
			SourceDomain: domain,
			SourceType:   models.SourceTypeRSS,
			Title:        strings.TrimSpace(item.Title),
			Description:  strings.TrimSpace(description),
			Content:      strings.TrimSpace(content),
			PublishedAt:  published,
			FetchedAt:    now,
			Language:     normalizeLang(lang),
		})
	}

	return items, nil
}

// extractDomain returns the registered domain from a URL string.
// On parse failure, returns the raw URL.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	host := u.Hostname()
	// Strip www. prefix
	host = strings.TrimPrefix(host, "www.")
	return host
}

// normalizeLang converts a feed language tag to a simple BCP-47 primary subtag.
func normalizeLang(lang string) string {
	if lang == "" {
		return "en"
	}
	// Accept "en-US", "zh-CN", etc. — return only the primary subtag.
	parts := strings.SplitN(lang, "-", 2)
	return strings.ToLower(parts[0])
}

// truncateAtRuneBoundary returns the longest prefix of s whose byte length
// is ≤ maxBytes, cut on a UTF-8 rune boundary. A naive s[:maxBytes] can split
// a multi-byte rune and yield an invalid string that breaks JSON marshalling
// and DB insertion.
func truncateAtRuneBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk back from maxBytes to the start of a rune.
	i := maxBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}

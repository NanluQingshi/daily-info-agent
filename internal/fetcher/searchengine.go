package fetcher

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
	"golang.org/x/net/html"
)

const (
	// DefaultSearchEngineURL is the DuckDuckGo HTML search endpoint.
	// No API key required. Returns clean HTML that we parse for results.
	DefaultSearchEngineURL = "https://html.duckduckgo.com/html"

	searchResultLimit = 10 // max results per query
)

// SearchFetcher fetches news items via a web search engine (DuckDuckGo by default).
// It sends keyword queries and parses the HTML result page into RawItems.
// No API key is required — it uses the public HTML endpoint.
type SearchFetcher struct {
	baseURL string
	client  *http.Client
	logger  *slog.Logger
}

// NewSearchFetcher creates a SearchFetcher backed by the given search URL.
// If baseURL is empty, DuckDuckGo's HTML endpoint is used.
func NewSearchFetcher(baseURL string, httpClient *http.Client, logger *slog.Logger) *SearchFetcher {
	if baseURL == "" {
		baseURL = DefaultSearchEngineURL
	}
	return &SearchFetcher{
		baseURL: baseURL,
		client:  httpClient,
		logger:  logger,
	}
}

// Name returns the fetcher identifier ("search").
func (s *SearchFetcher) Name() string { return "search" }

// Fetch sends a keyword query to the search engine and parses results into RawItems.
// The FetchConfig.URL is used as the search query (keywords).
// If URL is empty, the category display name + default keywords are used.
func (s *SearchFetcher) Fetch(ctx context.Context, cfg models.FetchConfig) ([]models.RawItem, error) {
	query := cfg.URL
	if query == "" {
		// Build a query from the category if no explicit URL/query is given.
		query = buildSearchQuery(cfg.Categories)
	}
	if query == "" {
		s.logger.Debug("search fetcher: empty query, skipping")
		return nil, nil
	}

	doc, err := s.doSearch(ctx, query)
	if err != nil {
		return nil, &FetchError{
			Source:  s.Name(),
			URL:     s.baseURL,
			Wrapped: fmt.Errorf("search request failed: %w", err),
		}
	}

	results := parseSearchResults(doc, query, cfg.Categories)
	if len(results) > searchResultLimit {
		results = results[:searchResultLimit]
	}

	s.logger.Debug("search fetcher results",
		slog.String("query", query),
		slog.Int("results", len(results)),
	)
	return results, nil
}

// doSearch sends a POST request to the search engine and returns the parsed HTML.
func (s *SearchFetcher) doSearch(ctx context.Context, query string) (*html.Node, error) {
	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search engine returned HTTP %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse search results HTML: %w", err)
	}
	return doc, nil
}

// parseSearchResults extracts search result links and snippets from DuckDuckGo HTML.
func parseSearchResults(doc *html.Node, query string, categories []models.Category) []models.RawItem {
	var items []models.RawItem
	now := time.Now().UTC()

	// DuckDuckGo HTML results are in <a class="result__a"> with parent <h2 class="result__title">
	// Snippets are in <a class="result__snippet"> or <span class="result__snippet">
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Look for result links: <a class="result__a" href="...">
			if n.Data == "a" && hasClassAttr(n, "result__a") {
				href := getAttr(n, "href")
				title := extractText(n)
				if href != "" && title != "" {
					// The href from DuckDuckGo is a redirect URL — extract the actual URL.
					actualURL := decodeDuckDuckGoURL(href)
					domain := extractSearchDomain(actualURL)

					// Find the sibling snippet element
					var snippet string
					for sibling := n.Parent.NextSibling; sibling != nil; sibling = sibling.NextSibling {
						if sibling.Type == html.ElementNode {
							// Look for result snippet in various common selectors
							snippet = findSnippet(sibling)
							if snippet != "" {
								break
							}
						}
					}
					// Also check parent's next sibling for the snippet container
					if snippet == "" {
						snippet = findSnippetAround(n)
					}

					items = append(items, models.RawItem{
						URL:          actualURL,
						SourceDomain: domain,
						SourceType:   models.SourceTypeSearch,
						Title:        title,
						Description:  truncateText(snippet, 300),
						Content:      truncateText(snippet, 500),
						PublishedAt:  now,
						FetchedAt:    now,
						Language:     detectLanguage(title + " " + snippet),
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)

	// Assign categories if none was set
	if len(items) > 0 && len(categories) > 0 {
		cat := categories[0]
		for i := range items {
			items[i].SourceDomain = catTargetDomain(string(cat), items[i].SourceDomain)
		}
	}

	return items
}

// findSnippet searches for a snippet text node in the given element subtree.
func findSnippet(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "a" && hasClassAttr(n, "result__snippet") {
		return extractText(n)
	}
	if n.Type == html.ElementNode && n.Data == "span" && hasClassAttr(n, "result__snippet") {
		return extractText(n)
	}
	if n.Type == html.ElementNode && hasClassAttr(n, "result__snippet") {
		return extractText(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if s := findSnippet(c); s != "" {
			return s
		}
	}
	return ""
}

// findSnippetAround searches for a snippet near the given result link node.
func findSnippetAround(linkNode *html.Node) string {
	// Check parent's siblings
	for parent := linkNode.Parent; parent != nil; parent = parent.Parent {
		for sib := parent.NextSibling; sib != nil; sib = sib.NextSibling {
			if s := findSnippet(sib); s != "" {
				return s
			}
		}
	}
	return ""
}

// decodeDuckDuckGoURL extracts the actual URL from a DuckDuckGo redirect URL.
func decodeDuckDuckGoURL(href string) string {
	// DuckDuckGo redirect URLs look like:
	// //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Farticle&rut=...
	if strings.Contains(href, "uddg=") {
		parsed, err := url.Parse(href)
		if err == nil {
			if encoded := parsed.Query().Get("uddg"); encoded != "" {
				if decoded, err := url.QueryUnescape(encoded); err == nil {
					return decoded
				}
			}
		}
	}
	// Also handle direct URLs (some result types)
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return href
}

// extractSearchDomain returns the registered domain from a URL string for search results.
func extractSearchDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return host
}

// extractText returns the concatenated text content of a node.
func extractText(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(buf.String())
}

// hasClassAttr reports whether the node has a class attribute containing the given class.
func hasClassAttr(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			classes := strings.Fields(attr.Val)
			for _, c := range classes {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

// getAttr returns the value of an attribute by key.
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// truncateText truncates text to maxLen runes.
func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}

// detectLanguage is a simple heuristic for BCP-47 language detection.
func detectLanguage(text string) string {
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF { // CJK Unified Ideographs
			return "zh"
		}
	}
	return "en"
}

// catTargetDomain returns a category hint for the source domain.
func catTargetDomain(cat string, domain string) string {
	if domain != "" {
		return domain
	}
	switch cat {
	case "科技/AI":
		return "tech-search"
	case "金融":
		return "finance-search"
	case "政治":
		return "politics-search"
	case "经济":
		return "economy-search"
	case "国际":
		return "world-search"
	default:
		return "web-search"
	}
}

// buildSearchQuery builds a search query string from a list of categories.
func buildSearchQuery(categories []models.Category) string {
	if len(categories) == 0 {
		return ""
	}
	cat := categories[0]
	switch cat {
	case models.CategoryFinance:
		return "finance stock market economy news today"
	case models.CategoryPolitics:
		return "politics government policy news today"
	case models.CategoryEconomy:
		return "economy GDP trade business news today"
	case models.CategoryTechAI:
		return "technology AI artificial intelligence news today"
	case models.CategoryInternational:
		return "world international news today"
	default:
		return "latest news today " + string(cat)
	}
}

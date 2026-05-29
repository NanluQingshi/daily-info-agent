package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
)

const newsAPIBaseURL = "https://newsapi.org/v2/top-headlines"

// newsAPIResponse mirrors the NewsAPI v2 top-headlines JSON structure.
type newsAPIResponse struct {
	Status       string           `json:"status"`
	TotalResults int              `json:"totalResults"`
	Articles     []newsAPIArticle `json:"articles"`
	Code         string           `json:"code,omitempty"`
	Message      string           `json:"message,omitempty"`
}

type newsAPIArticle struct {
	Source      newsAPISource `json:"source"`
	Author      string        `json:"author"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	URLToImage  string        `json:"urlToImage"`
	PublishedAt string        `json:"publishedAt"`
	Content     string        `json:"content"`
}

type newsAPISource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// newsAPIFetcher implements Fetcher using the NewsAPI v2 endpoint.
type newsAPIFetcher struct {
	apiKey     string
	httpClient *http.Client
}

// NewNewsAPIFetcher constructs a Fetcher for the NewsAPI v2/top-headlines endpoint.
func NewNewsAPIFetcher(apiKey string, httpClient *http.Client) Fetcher {
	if httpClient == nil {
		httpClient = newHTTPClient(defaultFetchTimeout)
	}
	return &newsAPIFetcher{
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (n *newsAPIFetcher) Name() string { return "newsapi" }

// Fetch queries NewsAPI top-headlines. cfg.Params may include:
//   - "q":        keyword query
//   - "category": one of business, entertainment, health, science, sports, technology
//   - "language": two-letter language code (e.g. "en", "zh")
//   - "pageSize":  max results (default 20, max 100)
//
// Returns an empty slice (not an error) when the API returns 0 results.
func (n *newsAPIFetcher) Fetch(ctx context.Context, cfg models.FetchConfig) ([]models.RawItem, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultFetchTimeout
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := newsAPIBaseURL
	if cfg.URL != "" {
		endpoint = cfg.URL
	}

	params := url.Values{}
	params.Set("apiKey", n.apiKey)
	// Copy caller-supplied params
	for k, v := range cfg.Params {
		params.Set(k, v)
	}
	// Default language and page size if not provided
	if params.Get("language") == "" {
		params.Set("language", "en")
	}
	if params.Get("pageSize") == "" {
		params.Set("pageSize", "20")
	}

	fullURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, &FetchError{Source: n.Name(), URL: fullURL, Wrapped: fmt.Errorf("build request: %w", err)}
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return nil, &FetchError{Source: n.Name(), URL: fullURL, Wrapped: fmt.Errorf("http do: %w", err)}
	}
	defer resp.Body.Close()

	var apiResp newsAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, &FetchError{Source: n.Name(), URL: fullURL, Wrapped: fmt.Errorf("decode response: %w", err)}
	}

	if apiResp.Status != "ok" {
		return nil, &FetchError{
			Source:  n.Name(),
			URL:     fullURL,
			Wrapped: fmt.Errorf("api error code=%q message=%q", apiResp.Code, apiResp.Message),
		}
	}

	now := time.Now().UTC()
	items := make([]models.RawItem, 0, len(apiResp.Articles))
	for _, a := range apiResp.Articles {
		if a.URL == "" || a.Title == "" || a.Title == "[Removed]" {
			continue
		}

		published := now
		if a.PublishedAt != "" {
			if t, err := time.Parse(time.RFC3339, a.PublishedAt); err == nil {
				published = t.UTC()
			}
		}

		domain := extractDomain(a.URL)

		lang := params.Get("language")
		if lang == "" {
			lang = "en"
		}

		items = append(items, models.RawItem{
			URL:          a.URL,
			SourceDomain: domain,
			SourceType:   models.SourceTypeNewsAPI,
			Title:        strings.TrimSpace(a.Title),
			Description:  strings.TrimSpace(a.Description),
			Content:      strings.TrimSpace(a.Content),
			PublishedAt:  published,
			FetchedAt:    now,
			Language:     lang,
		})
	}

	return items, nil
}

package fetcher

import (
	"context"
	"net/http"
	"strings"

	"github.com/user/daily-info-agent/pkg/models"
)

// rssHubFetcher delegates to an RSS fetcher but prepends the RSSHub base URL
// to relative route paths supplied in FetchConfig.URL.
type rssHubFetcher struct {
	baseURL string
	inner   Fetcher // delegates to rssFetcher
}

// NewRSSHubFetcher constructs a Fetcher that uses the given RSSHub baseURL.
// For absolute URLs in cfg.URL the baseURL is ignored. For relative paths
// (e.g. "/wechat/mp/article/xxx") the baseURL is prepended.
func NewRSSHubFetcher(baseURL string, httpClient *http.Client) Fetcher {
	return &rssHubFetcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		inner:   NewRSSFetcher(httpClient),
	}
}

func (r *rssHubFetcher) Name() string { return "rsshub" }

// Fetch resolves the cfg.URL against the RSSHub base URL if necessary,
// then delegates to the underlying RSS fetcher.
func (r *rssHubFetcher) Fetch(ctx context.Context, cfg models.FetchConfig) ([]models.RawItem, error) {
	resolved := cfg
	resolved.Type = models.SourceTypeRSSHub

	if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
		// Relative route path — prepend base URL
		route := cfg.URL
		if !strings.HasPrefix(route, "/") {
			route = "/" + route
		}
		resolved.URL = r.baseURL + route
	}

	items, err := r.inner.Fetch(ctx, resolved)
	if err != nil {
		return nil, err
	}

	// Relabel source type to rsshub so logs are accurate.
	for i := range items {
		items[i].SourceType = models.SourceTypeRSSHub
	}
	return items, nil
}

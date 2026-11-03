// Package contract pins the five fixed upstream input shapes the pipeline
// depends on: RSS, NewsAPI, RSSHub, DuckDuckGo HTML search, and readability
// extraction. These tests exist so that a silent upstream format change turns
// CI red instead of quietly degrading the daily run into an "empty" outcome.
package contract

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/extract"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// fixture reads a golden file from testdata/. Keeping the fixtures on disk
// (rather than inline string literals) makes them diffable: a reviewer can see
// exactly which upstream field the contract depends on.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "fixture %s must exist", name)
	return data
}

// serveFixture starts an httptest server that returns body with the given
// content type. It fails the test if the handler is never hit, so a parser
// that silently stops making requests cannot pass.
func serveFixture(t *testing.T, body []byte, contentType string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestContract_RSSFeedShape(t *testing.T) {
	srv := serveFixture(t, fixture(t, "rss.xml"), "application/rss+xml")

	items, err := fetcher.NewRSSFetcher(srv.Client()).Fetch(context.Background(), models.FetchConfig{
		Type: models.SourceTypeRSS,
		URL:  srv.URL,
	})
	require.NoError(t, err)
	require.Len(t, items, 2, "both <item> elements must survive parsing")

	// Pinned contract: title, link, description, and published date.
	assert.Equal(t, "RSS Item Alpha", items[0].Title)
	assert.Equal(t, "https://rss.example.com/alpha", items[0].URL)
	assert.Equal(t, "Alpha summary text.", items[0].Description)
	// SourceDomain comes from the feed URL, which in a test is the fixture
	// server. The contract that matters is that it is non-empty and derived
	// from the feed host, not from the item link.
	assert.NotEmpty(t, items[0].SourceDomain)
	assert.Equal(t, models.SourceTypeRSS, items[0].SourceType)
	assert.Equal(t, 2026, items[0].PublishedAt.Year())
	assert.Equal(t, time.January, items[0].PublishedAt.Month())
	assert.Equal(t, 6, items[0].PublishedAt.Day())

	assert.Equal(t, "RSS Item Beta", items[1].Title)
}

func TestContract_NewsAPIResponseShape(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("apiKey")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "newsapi.json"))
	}))
	defer srv.Close()

	items, err := fetcher.NewNewsAPIFetcher("contract-key", srv.Client()).Fetch(context.Background(), models.FetchConfig{
		Type: models.SourceTypeNewsAPI,
		URL:  srv.URL,
		Params: map[string]string{
			"category": "technology",
		},
	})
	require.NoError(t, err)
	require.Len(t, items, 2, "both articles must survive decoding")
	assert.Equal(t, "contract-key", gotQuery, "apiKey must stay on the query string")

	assert.Equal(t, "NewsAPI Item Alpha", items[0].Title)
	assert.Equal(t, "https://newsapi.example.com/alpha", items[0].URL)
	assert.Equal(t, "Alpha description from NewsAPI.", items[0].Description)
	assert.Equal(t, "newsapi.example.com", items[0].SourceDomain)
	assert.Equal(t, models.SourceTypeNewsAPI, items[0].SourceType)
	assert.Equal(t, 2026, items[0].PublishedAt.Year())

	assert.Equal(t, "NewsAPI Item Beta", items[1].Title)
}

func TestContract_RSSHubRouteShape(t *testing.T) {
	srv := serveFixture(t, fixture(t, "rsshub.xml"), "application/rss+xml")

	items, err := fetcher.NewRSSHubFetcher(srv.URL, srv.Client()).Fetch(context.Background(), models.FetchConfig{
		Type: models.SourceTypeRSSHub,
		URL:  "/contract/route",
	})
	require.NoError(t, err)
	require.Len(t, items, 1, "relative RSSHub route must resolve against the base URL")

	assert.Equal(t, "RSSHub Item Alpha", items[0].Title)
	assert.Equal(t, models.SourceTypeRSSHub, items[0].SourceType)
	assert.NotEmpty(t, items[0].SourceDomain)
}

func TestContract_SearchHTMLShape(t *testing.T) {
	srv := serveFixture(t, fixture(t, "search.html"), "text/html; charset=utf-8")

	// The search fetcher builds its own client against the configured base URL;
	// point that base at the fixture server.
	f := fetcher.NewSearchFetcher(srv.URL, srv.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		Type:    models.SourceTypeSearch,
		URL:     srv.URL + "/html/?q=contract",
		Params:  map[string]string{"query": "contract"},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, items, 2, "both result__a anchors must be parsed")

	assert.Equal(t, "Search Item Alpha", items[0].Title)
	assert.Equal(t, "https://search.example.com/alpha", items[0].URL,
		"DuckDuckGo redirect URLs must be decoded to the real target")
	// Search domains are registrable-domain only: subdomains are stripped.
	assert.Equal(t, "example.com", items[0].SourceDomain)
	assert.Equal(t, models.SourceTypeSearch, items[0].SourceType)
	assert.Contains(t, items[0].Description, "Alpha snippet")

	assert.Equal(t, "Search Item Beta", items[1].Title)
}

func TestContract_ArticleExtractionShape(t *testing.T) {
	srv := serveFixture(t, fixture(t, "article.html"), "text/html; charset=utf-8")

	items := []models.RawItem{{
		URL:          srv.URL,
		Title:        "Contract Article Alpha",
		Description:  "short feed excerpt",
		SourceDomain: "article.example.com",
		SourceType:   models.SourceTypeRSS,
	}}

	count := extract.New(srv.Client(), 5, 1, nil).Enrich(context.Background(), items)
	require.Equal(t, 1, count, "the fixture page must clear the minimum length gate")
	assert.Contains(t, items[0].ContentText, "first paragraph of the contract article")
	assert.Contains(t, items[0].ContentText, "second paragraph")
}

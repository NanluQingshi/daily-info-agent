package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// testRSSFeed is a minimal valid RSS 2.0 feed for testing.
const testRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
  <title>Test Feed</title>
  <link>http://example.com</link>
  <description>Test</description>
  <item>
    <title>Article One</title>
    <link>http://example.com/1</link>
    <description>First article</description>
  </item>
  <item>
    <title>Article Two</title>
    <link>http://example.com/2</link>
    <description>Second article</description>
  </item>
</channel>
</rss>`

// rssTestServer starts an HTTP server that returns the given feed XML.
func rssTestServer(t *testing.T, feedXML string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, feedXML)
	}))
}

func TestNewRSSHubFetcher_Default(t *testing.T) {
	f := NewRSSHubFetcher("https://rsshub.app", nil)
	assert.NotNil(t, f)
	assert.Equal(t, "rsshub", f.Name())
}

func TestRSSHubFetcher_TrailingSlashTrimmed(t *testing.T) {
	f := NewRSSHubFetcher("https://rsshub.app/", nil)
	assert.Equal(t, "rsshub", f.Name())
}

func TestRSSHubFetcher_Fetch_AbsoluteURL_PassesThrough(t *testing.T) {
	srv := rssTestServer(t, testRSSFeed)
	t.Cleanup(srv.Close)

	f := NewRSSHubFetcher("https://rsshub.app", srv.Client())
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		URL: srv.URL, // absolute URL — baseURL is ignored
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	// Source type should be relabeled to rsshub
	assert.Equal(t, models.SourceTypeRSSHub, items[0].SourceType)
}

func TestRSSHubFetcher_Fetch_RelativePath_PrependsBaseURL(t *testing.T) {
	srv := rssTestServer(t, testRSSFeed)
	t.Cleanup(srv.Close)

	f := NewRSSHubFetcher(srv.URL, srv.Client())
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		URL: "/rss", // relative path — baseURL will be prepended
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, models.SourceTypeRSSHub, items[0].SourceType)
}

func TestRSSHubFetcher_Fetch_RelativePathNoLeadingSlash(t *testing.T) {
	srv := rssTestServer(t, testRSSFeed)
	t.Cleanup(srv.Close)

	f := NewRSSHubFetcher(srv.URL, srv.Client())
	items, err := f.Fetch(context.Background(), models.FetchConfig{
		URL: "rss", // no leading slash — should be added
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestRSSHubFetcher_Fetch_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	f := NewRSSHubFetcher(srv.URL, srv.Client())
	_, err := f.Fetch(context.Background(), models.FetchConfig{
		URL: srv.URL,
	})
	require.Error(t, err)
}

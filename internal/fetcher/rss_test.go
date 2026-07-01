package fetcher_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// Sample XML feeds
// ---------------------------------------------------------------------------

const validRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test News</title>
    <link>http://testnews.example.com</link>
    <description>Test feed</description>
    <language>en</language>
    <item>
      <title>Article One</title>
      <link>http://testnews.example.com/article-1</link>
      <description>Summary of article one</description>
      <pubDate>Mon, 01 Jan 2024 10:00:00 +0000</pubDate>
    </item>
    <item>
      <title>  Article Two With Whitespace  </title>
      <link>http://testnews.example.com/article-2</link>
      <description>Summary of article two</description>
      <pubDate>Tue, 02 Jan 2024 11:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`

const validAtomFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xml:lang="zh">
  <title>Atom Test Feed</title>
  <link href="http://atomfeed.example.com"/>
  <entry>
    <title>Atom Entry One</title>
    <link href="http://atomfeed.example.com/entry-1"/>
    <summary>Summary of atom entry one</summary>
    <published>2024-03-15T08:30:00Z</published>
    <updated>2024-03-15T09:00:00Z</updated>
    <content type="text">Full content of atom entry one — this is longer text</content>
  </entry>
</feed>`

const emptyRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty Feed</title>
    <link>http://empty.example.com</link>
    <description>No items here</description>
  </channel>
</rss>`

// rssWithNoLinkGUIDFallback has items where link is empty but GUID is a URL.
const rssWithGUIDFallback = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>GUID Test</title>
    <link>http://guid.example.com</link>
    <item>
      <title>GUID Item</title>
      <guid>http://guid.example.com/item-guid-1</guid>
      <description>Item using GUID as URL</description>
    </item>
  </channel>
</rss>`

// rssWithItemsNoURL has items with no link and no GUID — should be skipped.
const rssWithItemsNoURL = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>No URL Feed</title>
    <link>http://nourl.example.com</link>
    <item>
      <title>No URL Item</title>
      <description>This item has no link or GUID</description>
    </item>
  </channel>
</rss>`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func serveFeed(t *testing.T, body string, contentType string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		fmt.Fprint(w, body)
	}))
}

// ---------------------------------------------------------------------------
// Tests — happy path
// ---------------------------------------------------------------------------

func TestFetcher_ParseRSSFeed_ValidXML(t *testing.T) {
	srv := serveFeed(t, validRSSFeed, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{
		Type: models.SourceTypeRSS,
		URL:  srv.URL,
	}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, items, 2)

	first := items[0]
	assert.Equal(t, "Article One", first.Title)
	assert.Equal(t, "http://testnews.example.com/article-1", first.URL)
	assert.Equal(t, "Summary of article one", first.Description)
	assert.Equal(t, models.SourceTypeRSS, first.SourceType)
	assert.Equal(t, "en", first.Language)
	assert.False(t, first.PublishedAt.IsZero(), "PublishedAt should be set")
	assert.False(t, first.FetchedAt.IsZero(), "FetchedAt should be set")

	second := items[1]
	assert.Equal(t, "Article Two With Whitespace", second.Title,
		"title whitespace should be trimmed")
}

func TestFetcher_ParseRSSFeed_SourceDomainExtractedFromFeedURL(t *testing.T) {
	srv := serveFeed(t, validRSSFeed, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	// httptest server URL is http://127.0.0.1:<port>; domain = "127.0.0.1"
	assert.Equal(t, "127.0.0.1", items[0].SourceDomain)
}

func TestFetcher_ParseRSSFeed_PublishedAtParsedCorrectly(t *testing.T) {
	srv := serveFeed(t, validRSSFeed, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, items, 2)

	expected, _ := time.Parse(time.RFC1123Z, "Mon, 01 Jan 2024 10:00:00 +0000")
	assert.Equal(t, expected.UTC(), items[0].PublishedAt.UTC())
}

func TestFetcher_ParseAtomFeed_ValidXML(t *testing.T) {
	srv := serveFeed(t, validAtomFeed, "application/atom+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, items, 1)

	item := items[0]
	assert.Equal(t, "Atom Entry One", item.Title)
	assert.Equal(t, "http://atomfeed.example.com/entry-1", item.URL)
	assert.Equal(t, "zh", item.Language)
}

func TestFetcher_ParseRSSFeed_GUIDUsedWhenLinkEmpty(t *testing.T) {
	srv := serveFeed(t, rssWithGUIDFallback, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "http://guid.example.com/item-guid-1", items[0].URL)
}

func TestFetcher_ParseRSSFeed_ItemsWithNoURLSkipped(t *testing.T) {
	srv := serveFeed(t, rssWithItemsNoURL, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	assert.Empty(t, items, "items with no link or GUID should be skipped")
}

func TestFetcher_ParseRSSFeed_EmptyFeed_ReturnsEmptySlice(t *testing.T) {
	srv := serveFeed(t, emptyRSSFeed, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestFetcher_ParseRSSFeed_ContentTruncatedAt2000Chars(t *testing.T) {
	longContent := make([]byte, 3000)
	for i := range longContent {
		longContent[i] = 'x'
	}
	feed := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel><title>T</title>
  <item>
    <title>Long Item</title>
    <link>http://example.com/long</link>
    <content:encoded><![CDATA[%s]]></content:encoded>
  </item></channel></rss>`, string(longContent))

	srv := serveFeed(t, feed, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.LessOrEqual(t, len(items[0].Content), 2000)
}

// TestFetcher_ParseRSSFeed_ContentTruncationMultibyteSafe feeds 3-byte UTF-8
// runes (Chinese) and asserts the truncation lands on a rune boundary — the
// result must be valid UTF-8 and ≤ 2000 bytes. A naive byte slice would split
// the last rune and produce invalid UTF-8.
func TestFetcher_ParseRSSFeed_ContentTruncationMultibyteSafe(t *testing.T) {
	// 1000 Chinese chars = 3000 bytes; truncation must cut at ≤2000 bytes on a
	// rune boundary (2000 / 3 = 666 runes, 1998 bytes).
	longContent := strings.Repeat("新", 1000)
	feed := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel><title>T</title>
  <item>
    <title>Long Item</title>
    <link>http://example.com/long</link>
    <content:encoded><![CDATA[%s]]></content:encoded>
  </item></channel></rss>`, longContent)

	srv := serveFeed(t, feed, "application/rss+xml")
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	items, err := f.Fetch(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.LessOrEqual(t, len(items[0].Content), 2000)
	assert.True(t, utf8.ValidString(items[0].Content), "truncated content must be valid UTF-8")
}

// ---------------------------------------------------------------------------
// Tests — error cases
// ---------------------------------------------------------------------------

func TestFetcher_EmptyURL_ReturnsError(t *testing.T) {
	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: ""}

	_, err := f.Fetch(context.Background(), cfg)
	require.Error(t, err)

	var fetchErr *fetcher.FetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, "rss", fetchErr.Source)
}

func TestFetcher_InvalidXML_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, "this is not xml at all!!!")
	}))
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	_, err := f.Fetch(context.Background(), cfg)
	require.Error(t, err)

	var fetchErr *fetcher.FetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, "rss", fetchErr.Source)
}

func TestFetcher_ServerReturns404_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{Type: models.SourceTypeRSS, URL: srv.URL}

	_, err := f.Fetch(context.Background(), cfg)
	require.Error(t, err)
}

func TestFetcher_ContextCancelled_ReturnsError(t *testing.T) {
	// Server that hangs long enough for context to cancel.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	f := fetcher.NewRSSFetcher(nil)
	cfg := models.FetchConfig{
		Type:    models.SourceTypeRSS,
		URL:     srv.URL,
		Timeout: 50 * time.Millisecond,
	}

	_, err := f.Fetch(context.Background(), cfg)
	require.Error(t, err)
}

func TestFetcher_Name_ReturnsRSS(t *testing.T) {
	f := fetcher.NewRSSFetcher(nil)
	assert.Equal(t, "rss", f.Name())
}

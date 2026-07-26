package fetcher

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
	"golang.org/x/net/html"
)

// ---------------------------------------------------------------------------
// catTargetDomain
// ---------------------------------------------------------------------------

func TestCatTargetDomain_ExistingDomain(t *testing.T) {
	assert.Equal(t, "reuters.com", catTargetDomain("", "reuters.com"))
}

func TestCatTargetDomain_ByCategory(t *testing.T) {
	assert.Equal(t, "tech-search", catTargetDomain("科技/AI", ""))
	assert.Equal(t, "finance-search", catTargetDomain("金融", ""))
	assert.Equal(t, "politics-search", catTargetDomain("政治", ""))
	assert.Equal(t, "economy-search", catTargetDomain("经济", ""))
	assert.Equal(t, "world-search", catTargetDomain("国际", ""))
	assert.Equal(t, "web-search", catTargetDomain("未知", ""))
}

// ---------------------------------------------------------------------------
// detectLanguage
// ---------------------------------------------------------------------------

func TestDetectLanguage_Chinese(t *testing.T) {
	assert.Equal(t, "zh", detectLanguage("这是一篇中文文章"))
}

func TestDetectLanguage_English(t *testing.T) {
	assert.Equal(t, "en", detectLanguage("This is an English article"))
}

func TestDetectLanguage_Mixed(t *testing.T) {
	assert.Equal(t, "zh", detectLanguage("AI 技术的最新进展"))
}

func TestDetectLanguage_Empty(t *testing.T) {
	assert.Equal(t, "en", detectLanguage(""))
}

// ---------------------------------------------------------------------------
// truncateText
// ---------------------------------------------------------------------------

func TestTruncateText_ShortEnough(t *testing.T) {
	assert.Equal(t, "hello", truncateText("hello", 10))
}

func TestTruncateText_Truncated(t *testing.T) {
	assert.Equal(t, "hello...", truncateText("hello world", 5))
}

func TestTruncateText_Multibyte(t *testing.T) {
	assert.Equal(t, "你好...", truncateText("你好世界", 2))
}

func TestTruncateText_Empty(t *testing.T) {
	assert.Empty(t, truncateText("", 10))
}

// ---------------------------------------------------------------------------
// extractSearchDomain
// ---------------------------------------------------------------------------

func TestExtractSearchDomain_Standard(t *testing.T) {
	assert.Equal(t, "reuters.com", extractSearchDomain("https://www.reuters.com/article"))
}

func TestExtractSearchDomain_NoSubdomain(t *testing.T) {
	assert.Equal(t, "bbc.com", extractSearchDomain("https://bbc.com/news"))
}

func TestExtractSearchDomain_InvalidURL(t *testing.T) {
	assert.Empty(t, extractSearchDomain("://bad"))
}

func TestExtractSearchDomain_Empty(t *testing.T) {
	assert.Empty(t, extractSearchDomain(""))
}

// ---------------------------------------------------------------------------
// hasClassAttr / getAttr
// ---------------------------------------------------------------------------

func TestHasClassAttr_Found(t *testing.T) {
	n := &html.Node{
		Type: html.ElementNode,
		Attr: []html.Attribute{{Key: "class", Val: "result__a snippet"}},
	}
	assert.True(t, hasClassAttr(n, "result__a"))
	assert.True(t, hasClassAttr(n, "snippet"))
}

func TestHasClassAttr_NotFound(t *testing.T) {
	n := &html.Node{
		Type: html.ElementNode,
		Attr: []html.Attribute{{Key: "class", Val: "other-class"}},
	}
	assert.False(t, hasClassAttr(n, "result__a"))
}

func TestHasClassAttr_NoClass(t *testing.T) {
	n := &html.Node{Type: html.ElementNode}
	assert.False(t, hasClassAttr(n, "anything"))
}

func TestGetAttr_Found(t *testing.T) {
	n := &html.Node{
		Attr: []html.Attribute{{Key: "href", Val: "https://example.com"}},
	}
	assert.Equal(t, "https://example.com", getAttr(n, "href"))
}

func TestGetAttr_NotFound(t *testing.T) {
	n := &html.Node{}
	assert.Empty(t, getAttr(n, "nonexistent"))
}

// ---------------------------------------------------------------------------
// extractText
// ---------------------------------------------------------------------------

func TestExtractText_Simple(t *testing.T) {
	n := &html.Node{
		Type: html.ElementNode,
		FirstChild: &html.Node{
			Type: html.TextNode,
			Data: "Hello World",
		},
	}
	assert.Equal(t, "Hello World", extractText(n))
}

func TestExtractText_Nested(t *testing.T) {
	// <div>Hello <span>World</span></div>
	span := &html.Node{Type: html.ElementNode, Data: "span"}
	span.FirstChild = &html.Node{Type: html.TextNode, Data: "World"}

	div := &html.Node{Type: html.ElementNode, Data: "div"}
	div.FirstChild = &html.Node{Type: html.TextNode, Data: "Hello "}
	div.FirstChild.NextSibling = span

	assert.Equal(t, "Hello World", extractText(div))
}

// ---------------------------------------------------------------------------
// findSnippet
// ---------------------------------------------------------------------------

func TestFindSnippet_InAnchor(t *testing.T) {
	// <a class="result__snippet">snippet text</a>
	n := &html.Node{
		Type: html.ElementNode,
		Data: "a",
		Attr: []html.Attribute{{Key: "class", Val: "result__snippet"}},
		FirstChild: &html.Node{Type: html.TextNode, Data: "snippet text"},
	}
	assert.Equal(t, "snippet text", findSnippet(n))
}

func TestFindSnippet_InSpan(t *testing.T) {
	n := &html.Node{
		Type: html.ElementNode,
		Data: "span",
		Attr: []html.Attribute{{Key: "class", Val: "result__snippet"}},
		FirstChild: &html.Node{Type: html.TextNode, Data: "span snippet"},
	}
	assert.Equal(t, "span snippet", findSnippet(n))
}

func TestFindSnippet_NotFound(t *testing.T) {
	n := &html.Node{
		Type: html.ElementNode,
		Data: "div",
		FirstChild: &html.Node{Type: html.TextNode, Data: "no snippet here"},
	}
	assert.Empty(t, findSnippet(n))
}

// ---------------------------------------------------------------------------
// parseSearchResults — with mock HTML
// ---------------------------------------------------------------------------

func TestParseSearchResults_EmptyDoc(t *testing.T) {
	doc := &html.Node{Type: html.DocumentNode}
	results := parseSearchResults(doc, "test query", nil)
	assert.Empty(t, results)
}

func TestParseSearchResults_WithResults(t *testing.T) {
  // Build a minimal DuckDuckGo-like HTML structure
  // <html><body><div><h2 class="result__title">
  //   <a class="result__a" href="...">Test Title</a>
  // </h2><a class="result__snippet">Test snippet</a></div></body></html>
  snippet := &html.Node{
   Type: html.ElementNode, Data: "a",
   Attr: []html.Attribute{{Key: "class", Val: "result__snippet"}},
   FirstChild: &html.Node{Type: html.TextNode, Data: "Test snippet content"},
  }

  link := &html.Node{
   Type: html.ElementNode, Data: "a",
   Attr: []html.Attribute{
    {Key: "class", Val: "result__a"},
    {Key: "href", Val: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Farticle"},
   },
   FirstChild: &html.Node{Type: html.TextNode, Data: "Test Article Title"},
  }

  h2 := &html.Node{Type: html.ElementNode, Data: "h2"}
  h2.Attr = []html.Attribute{{Key: "class", Val: "result__title"}}
  h2.FirstChild = link
  link.Parent = h2
  h2.NextSibling = snippet
  snippet.Parent = h2.Parent

  div := &html.Node{Type: html.ElementNode, Data: "div"}
  div.FirstChild = h2
  h2.Parent = div

  body := &html.Node{Type: html.ElementNode, Data: "body"}
  body.FirstChild = div
  div.Parent = body

  htmlNode := &html.Node{Type: html.ElementNode, Data: "html"}
  htmlNode.FirstChild = body
  body.Parent = htmlNode

  doc := &html.Node{Type: html.DocumentNode}
  doc.FirstChild = htmlNode
  htmlNode.Parent = doc

  results := parseSearchResults(doc, "test query", nil)
  require.Len(t, results, 1)
  assert.Equal(t, "https://example.com/article", results[0].URL)
  assert.Equal(t, "Test Article Title", results[0].Title)
  assert.Contains(t, results[0].Description, "Test snippet content")
 }

// ---------------------------------------------------------------------------
// Fetch with mock HTTP server
// ---------------------------------------------------------------------------

func TestSearchFetcher_Fetch_WithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><div><h2 class="result__title"><a class="result__a" href="//ddg.com/l/?uddg=https%3A%2F%2Fexample.com%2Farticle">Test Title</a></h2><a class="result__snippet">Snippet</a></div></body></html>`))
	}))
	t.Cleanup(srv.Close)

	s := NewSearchFetcher(srv.URL, srv.Client(), slog.Default())
	results, err := s.Fetch(context.Background(), models.FetchConfig{
		URL:        "test query",
		Categories: []models.Category{models.CategoryTechAI},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Test Title", results[0].Title)
	assert.Equal(t, "example.com", results[0].SourceDomain)
}

func TestSearchFetcher_Fetch_EmptyQuery(t *testing.T) {
	s := NewSearchFetcher("http://localhost", &http.Client{}, slog.Default())
	results, err := s.Fetch(context.Background(), models.FetchConfig{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestSearchFetcher_Fetch_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	s := NewSearchFetcher(srv.URL, srv.Client(), slog.Default())
	_, err := s.Fetch(context.Background(), models.FetchConfig{URL: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search request failed")
}

// ---------------------------------------------------------------------------
// findSnippetAround
// ---------------------------------------------------------------------------

func TestFindSnippetAround_Found(t *testing.T) {
  // Create a structure where the snippet is a sibling of the parent
  // <h2><a>link</a></h2><a class="result__snippet">found snippet</a>
  snippet := &html.Node{
   Type: html.ElementNode, Data: "a",
   Attr: []html.Attribute{{Key: "class", Val: "result__snippet"}},
   FirstChild: &html.Node{Type: html.TextNode, Data: "found snippet"},
  }

  link := &html.Node{Type: html.ElementNode, Data: "a"}
  parent := &html.Node{Type: html.ElementNode, Data: "h2"}
  parent.FirstChild = link
  link.Parent = parent
  parent.NextSibling = snippet

  result := findSnippetAround(link)
  assert.Equal(t, "found snippet", result)
 }

func TestFindSnippetAround_NotFound(t *testing.T) {
	link := &html.Node{Type: html.ElementNode, Data: "a"}
	result := findSnippetAround(link)
	assert.Empty(t, result)
}

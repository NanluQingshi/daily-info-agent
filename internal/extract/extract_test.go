package extract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// pageHTML is a minimal readable article longer than DefaultMinLength runes.
func pageHTML(title string) string {
	body := strings.Repeat("这是一段用于测试正文提取的中文内容。", 60) // 1020 runes
	return "<html><head><title>" + title + "</title></head><body>" +
		"<nav>navigation junk</nav>" +
		"<article><h1>" + title + "</h1><p>" + body + "</p></article>" +
		"</body></html>"
}

func TestExtractor_EnrichFillsContentText(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML("测试文章")))
	}))
	defer srv.Close()

	x := New(srv.Client(), 10, 2, nil)
	items := []models.RawItem{{URL: srv.URL + "/a", Title: "A", Description: "desc"}}

	n := x.Enrich(context.Background(), items)

	require.Equal(t, 1, n)
	require.Equal(t, int64(1), hits.Load())
	assert.True(t, utf8Runes(items[0].ContentText) >= DefaultMinLength)
	assert.Contains(t, items[0].ContentText, "测试正文提取")
}

func TestExtractor_SkipsItemsWithFeedContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not fetch when feed content is long enough")
	}))
	defer srv.Close()

	x := New(srv.Client(), 10, 2, nil)
	items := []models.RawItem{{
		URL:     srv.URL,
		Title:   "A",
		Content: strings.Repeat("自带的正文内容。", 100), // > minLen runes
	}}

	n := x.Enrich(context.Background(), items)
	assert.Equal(t, 0, n)
	assert.Empty(t, items[0].ContentText)
}

func TestExtractor_FailureFallsBackSilently(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	x := New(srv.Client(), 10, 2, nil)
	items := []models.RawItem{{URL: srv.URL, Title: "A", Description: "desc"}}

	n := x.Enrich(context.Background(), items)
	assert.Equal(t, 0, n)
	assert.Empty(t, items[0].ContentText)
}

func TestExtractor_ShortExtractionRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body><article><p>太短</p></article></body></html>"))
	}))
	defer srv.Close()

	x := New(srv.Client(), 10, 2, nil)
	items := []models.RawItem{{URL: srv.URL, Title: "A"}}

	n := x.Enrich(context.Background(), items)
	assert.Equal(t, 0, n)
	assert.Empty(t, items[0].ContentText)
}

func TestExtractor_RespectsMaxItems(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(pageHTML("t")))
	}))
	defer srv.Close()

	x := New(srv.Client(), 2, 4, nil) // maxItems = 2
	items := []models.RawItem{
		{URL: srv.URL + "/1"}, {URL: srv.URL + "/2"}, {URL: srv.URL + "/3"},
	}

	n := x.Enrich(context.Background(), items)
	assert.Equal(t, 2, n)
	assert.Equal(t, int64(2), hits.Load())
}

func TestExtractor_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(pageHTML("t")))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	x := New(srv.Client(), 10, 2, nil)
	items := []models.RawItem{{URL: srv.URL, Title: "A"}}

	n := x.Enrich(ctx, items)
	assert.Equal(t, 0, n)
}

func TestExtractor_TruncatesHugePages(t *testing.T) {
	huge := strings.Repeat("字", 200_000) // 200k runes > maxStoreLen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body><article><p>" + huge + "</p></article></body></html>"))
	}))
	defer srv.Close()

	x := New(srv.Client(), 10, 2, nil)
	items := []models.RawItem{{URL: srv.URL, Title: "A"}}

	n := x.Enrich(context.Background(), items)
	require.Equal(t, 1, n)
	assert.LessOrEqual(t, utf8Runes(items[0].ContentText), maxStoreLen)
}

func utf8Runes(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

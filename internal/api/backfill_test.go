package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"

	"github.com/user/daily-info-agent/internal/extract"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/config"
)

// longHTML is >200 runes so the extractor accepts it (DefaultMinLength).
func longHTML() string {
	body := strings.Repeat("这是一段足够长的正文内容，用于通过提取器的最小长度校验。", 10)
	return "<html><body><article><h1>标题</h1><p>" + body + "</p></article></body></html>"
}

func newBackfillExtractor(t *testing.T, failedPath string) (*extract.Extractor, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == failedPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(longHTML()))
	}))
	t.Cleanup(srv.Close)
	return extract.New(srv.Client(), 10, 2, slog.Default()), srv
}

func callBackfill(t *testing.T, h *Handler, rawQuery string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/x?"+rawQuery, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	err := h.BackfillContent(c)
	return rec, err
}

// mustBackfill asserts the handler completed without an echo error.
func mustBackfill(t *testing.T, h *Handler, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	rec, err := callBackfill(t, h, rawQuery)
	require.NoError(t, err)
	return rec
}

func TestBackfillContent_FillsMissingText(t *testing.T) {
	ext, srv := newBackfillExtractor(t, "")
	m := &mockStore{}
	m.backfillResp.refs = []store.ArticleContentRef{
		{ID: 1, SourceURL: srv.URL + "/a"},
		{ID: 2, SourceURL: srv.URL + "/b"},
	}
	m.backfillResp.total = 5
	m.backfillResp.content = map[int64]string{}

	h := New(m, nil, nil, &config.Config{}, ext, slog.Default())
	rec := mustBackfill(t, h, "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"updated":2`)
	assert.Contains(t, rec.Body.String(), `"processed":2`)
	assert.Contains(t, rec.Body.String(), `"failed":0`)
	assert.Contains(t, rec.Body.String(), `"remaining":3`) // 5 total - 2 filled
	assert.Len(t, m.backfillResp.updated, 2)
	for _, id := range []int64{1, 2} {
		assert.NotEmpty(t, m.backfillResp.content[id])
	}
}

func TestBackfillContent_PageFailureDegrades(t *testing.T) {
	ext, srv := newBackfillExtractor(t, "/bad")
	m := &mockStore{}
	m.backfillResp.refs = []store.ArticleContentRef{
		{ID: 1, SourceURL: srv.URL + "/bad"}, // 404 → failure
		{ID: 2, SourceURL: srv.URL + "/ok"},
	}
	m.backfillResp.total = 2
	m.backfillResp.content = map[int64]string{}

	h := New(m, nil, nil, &config.Config{}, ext, slog.Default())
	rec := mustBackfill(t, h, "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"updated":1`)
	assert.Contains(t, rec.Body.String(), `"failed":1`)
	assert.Contains(t, rec.Body.String(), `"remaining":1`)
	assert.NotContains(t, m.backfillResp.updated, int64(1)) // failure untouched
}

func TestBackfillContent_LimitValidation(t *testing.T) {
	h := New(&mockStore{}, nil, nil, &config.Config{}, nil, slog.Default())
	for _, q := range []string{"limit=0", "limit=-3", "limit=501", "limit=abc"} {
		rec := mustBackfill(t, h, q)
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
	}
}

func TestBackfillContent_ExtractorDisabled(t *testing.T) {
	// nil *extract.Extractor must keep the interface nil (no typed-nil trap)
	h := New(&mockStore{}, nil, nil, &config.Config{}, nil, slog.Default())
	rec := mustBackfill(t, h, "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "fulltext_disabled")
}

func TestBackfillContent_NilStoreAndDBError(t *testing.T) {
	ext, _ := newBackfillExtractor(t, "")
	h := New(nil, nil, nil, &config.Config{}, ext, slog.Default())
	rec, err := callBackfill(t, h, "")
	require.Error(t, err) // echo aborts on requireStore error
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	m := &mockStore{}
	m.backfillResp.err = errors.New("db down")
	h = New(m, nil, nil, &config.Config{}, ext, slog.Default())
	rec = mustBackfill(t, h, "")
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestBackfillContent_RouteRegistered(t *testing.T) {
	ext, _ := newBackfillExtractor(t, "")
	h := New(&mockStore{}, nil, nil, &config.Config{}, ext, slog.Default())
	e := newEcho()
	h.Register(e.Group("/api"))

	req := httptest.NewRequest(http.MethodPost, "/api/articles/backfill-content", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

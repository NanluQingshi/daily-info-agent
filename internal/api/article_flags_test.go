package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/user/daily-info-agent/pkg/models"
)

func mkFlagStore() *mockStore {
	m := &mockStore{articles: map[int64]models.ArticleRow{}, nextID: 1}
	m.articles[1] = models.ArticleRow{ID: 1, Title: "Unread article", Status: "published"}
	m.articles[2] = models.ArticleRow{ID: 2, Title: "Read article", Status: "published"}
	return m
}

func callFlags(t *testing.T, m *mockStore, method, path, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)
	require.NoError(t, h.UpdateArticleFlags(c))
	return rec
}

func TestUpdateArticleFlags_BookmarkAndRead(t *testing.T) {
	m := mkFlagStore()
	rec := callFlags(t, m, http.MethodPatch, "/api/articles/1/flags", "1",
		`{"bookmarked": true, "read": true}`)
	require.Equal(t, http.StatusOK, rec.Code)

	var got models.ArticleRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, got.Bookmarked)
	require.NotNil(t, got.ReadAt)
	assert.WithinDuration(t, time.Now().UTC(), *got.ReadAt, time.Minute)
}

func TestUpdateArticleFlags_PartialUpdateAndUndo(t *testing.T) {
	m := mkFlagStore()

	// Only mark read; bookmark untouched.
	rec := callFlags(t, m, http.MethodPatch, "/api/articles/1/flags", "1", `{"read": true}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var got models.ArticleRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.False(t, got.Bookmarked, "bookmark must stay false when omitted")
	require.NotNil(t, got.ReadAt)

	// Undo the read state (idempotent semantics per #59).
	rec = callFlags(t, m, http.MethodPatch, "/api/articles/1/flags", "1", `{"read": false}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Nil(t, got.ReadAt)
	assert.False(t, got.Bookmarked)
}

func TestUpdateArticleFlags_RepeatedCallsIdempotent(t *testing.T) {
	m := mkFlagStore()
	for i := 0; i < 3; i++ {
		rec := callFlags(t, m, http.MethodPatch, "/api/articles/1/flags", "1", `{"bookmarked": true}`)
		require.Equal(t, http.StatusOK, rec.Code, "call %d", i)
	}
	a := m.articles[1]
	assert.True(t, a.Bookmarked)
}

func TestUpdateArticleFlags_NotFound(t *testing.T) {
	m := mkFlagStore()
	rec := callFlags(t, m, http.MethodPatch, "/api/articles/999/flags", "999", `{"read": true}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUpdateArticleFlags_BadID(t *testing.T) {
	m := mkFlagStore()
	rec := callFlags(t, m, http.MethodPatch, "/api/articles/abc/flags", "abc", `{"read": true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateArticleFlags_EmptyBody(t *testing.T) {
	m := mkFlagStore()
	rec := callFlags(t, m, http.MethodPatch, "/api/articles/1/flags", "1", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateArticleFlags_DBError(t *testing.T) {
	m := mkFlagStore()
	m.flagsResp.err = errors.New("db down")
	rec := callFlags(t, m, http.MethodPatch, "/api/articles/1/flags", "1", `{"read": true}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUpdateArticleFlags_NilStore(t *testing.T) {
	// A typed-nil *mockStore would make the interface non-nil; pass an
	// untyped nil so requireStore sees a genuinely absent store.
	h := newHandler(nil, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPatch, "/api/articles/1/flags", strings.NewReader(`{"read": true}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.UpdateArticleFlags(c)
	require.Error(t, err) // requireStore aborts with a sentinel error
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestListArticles_BoolFilters_Parsed(t *testing.T) {
	// Invalid boolean values are rejected before reaching the store.
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	for _, q := range []string{"bookmarked=yes", "unread=1x"} {
		req := httptest.NewRequest(http.MethodGet, "/api/articles?"+q, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		require.NoError(t, h.ListArticles(c), q)
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
	}
}

func TestUpdateArticleFlags_RouteRegistered(t *testing.T) {
	h := newHandler(mkFlagStore(), nil, nil)
	e := newEcho()
	h.Register(e.Group("/api"))

	req := httptest.NewRequest(http.MethodPatch, "/api/articles/1/flags",
		strings.NewReader(`{"read": true}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

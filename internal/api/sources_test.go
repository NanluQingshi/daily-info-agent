package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

func newSourcesHandler(m *mockStore) (*Handler, *echo.Echo) {
	h := newHandler(m, nil, nil)
	e := newEcho()
	h.Register(e.Group("/api"))
	return h, e
}

func TestSources_List_ReturnsRowsIncludingDisabled(t *testing.T) {
	m := &mockStore{}
	m.sourcesResp.list = []models.SourceRow{
		{ID: 1, URL: "https://a.example/rss", Enabled: true},
		{ID: 2, URL: "https://b.example/rss", Enabled: false},
	}
	_, e := newSourcesHandler(m)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sources", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"enabled":false`)
	assert.Contains(t, rec.Body.String(), "b.example")
}

func TestSources_List_EmptyIsNotAnError(t *testing.T) {
	_, e := newSourcesHandler(&mockStore{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sources", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"sources":[]`)
}

func TestSources_Add_ValidatesURL(t *testing.T) {
	_, e := newSourcesHandler(&mockStore{})

	for _, body := range []string{
		`{"url":""}`,
		`{"url":"not a url"}`,
		`{"url":"ftp://example.com/rss"}`,
		`{}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

func TestSources_Add_ConflictOnDuplicate(t *testing.T) {
	m := &mockStore{}
	m.sourcesResp.addErr = store.ErrConflict
	_, e := newSourcesHandler(m)

	req := httptest.NewRequest(http.MethodPost, "/api/sources",
		strings.NewReader(`{"url":"https://dup.example/rss"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestSources_Add_CreatesRow(t *testing.T) {
	m := &mockStore{}
	m.sourcesResp.added = models.SourceRow{URL: "https://new.example/rss", Enabled: true}
	_, e := newSourcesHandler(m)

	req := httptest.NewRequest(http.MethodPost, "/api/sources",
		strings.NewReader(`{"url":"https://new.example/rss"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), "new.example")
}

func TestSources_SetEnabled_RequiresBoolAndValidID(t *testing.T) {
	_, e := newSourcesHandler(&mockStore{})

	// missing enabled
	req := httptest.NewRequest(http.MethodPatch, "/api/sources/3", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// non-numeric id
	req2 := httptest.NewRequest(http.MethodPatch, "/api/sources/abc",
		strings.NewReader(`{"enabled":true}`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestSources_SetEnabled_NotFound(t *testing.T) {
	m := &mockStore{}
	m.sourcesResp.updErr = store.ErrNotFound
	_, e := newSourcesHandler(m)

	req := httptest.NewRequest(http.MethodPatch, "/api/sources/42",
		strings.NewReader(`{"enabled":false}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSources_SetEnabled_UpdatesRow(t *testing.T) {
	m := &mockStore{}
	m.sourcesResp.updated = models.SourceRow{URL: "https://x.example/rss"}
	_, e := newSourcesHandler(m)

	req := httptest.NewRequest(http.MethodPatch, "/api/sources/7",
		strings.NewReader(`{"enabled":false}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"enabled":false`)
}

func TestSources_Remove_NoContentAndNotFound(t *testing.T) {
	m := &mockStore{}
	_, e := newSourcesHandler(m)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/sources/9", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	m.sourcesResp.delErr = store.ErrNotFound
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, httptest.NewRequest(http.MethodDelete, "/api/sources/9", nil))
	assert.Equal(t, http.StatusNotFound, rec2.Code)
}

func TestSources_ManagementRoutesNeedStore(t *testing.T) {
	h := newHandlerWithCfg(&config.Config{})
	e := newEcho()
	h.Register(e.Group("/api"))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sources", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// mockStore compile guard: the interface grew in this feature.
var _ store.ArticleStore = (*mockStore)(nil)

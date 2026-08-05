package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/publisher"
	"github.com/user/daily-info-agent/internal/scheduler"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// mockStore — a minimal implementation of store.ArticleStore for testing
// ---------------------------------------------------------------------------

type mockStore struct {
	articles    map[int64]models.ArticleRow
	runLogs     map[string]models.RunLogRow
	nextID      int64
	listResp    struct {
		articles []models.ArticleRow
		total    int
		err      error
	}
	getResp     struct {
		article models.ArticleRow
		err     error
	}
	statsResp   struct {
		stats models.StatsResult
		err   error
	}
	deleteErr   error
	markPubErr  error
	markFailErr error
	markPenErr  error
	saveRunErr  error
	saveArtErr  error
}

func (m *mockStore) SaveArticles(ctx context.Context, articles []models.ProcessedArticle, runID string) (int, error) {
	return len(articles), m.saveArtErr
}

func (m *mockStore) SaveRunLog(ctx context.Context, log models.RunLogRow) error {
	if m.runLogs == nil {
		m.runLogs = make(map[string]models.RunLogRow)
	}
	m.runLogs[log.RunID] = log
	return m.saveRunErr
}

func (m *mockStore) GetRunLog(ctx context.Context, runID string) (models.RunLogRow, error) {
	if m.runLogs != nil {
		if r, ok := m.runLogs[runID]; ok {
			return r, nil
		}
	}
	return models.RunLogRow{}, store.ErrNotFound
}

func (m *mockStore) ListArticles(ctx context.Context, f models.ArticleFilter) ([]models.ArticleRow, int, error) {
	return m.listResp.articles, m.listResp.total, m.listResp.err
}

func (m *mockStore) GetArticle(ctx context.Context, id int64) (models.ArticleRow, error) {
	return m.getResp.article, m.getResp.err
}

func (m *mockStore) DeleteArticle(ctx context.Context, id int64) error {
	return m.deleteErr
}

func (m *mockStore) MarkPublished(ctx context.Context, id int64, externalID int64) error {
	return m.markPubErr
}

func (m *mockStore) MarkFailed(ctx context.Context, id int64) error {
	return m.markFailErr
}

func (m *mockStore) MarkPending(ctx context.Context, id int64) error {
	return m.markPenErr
}

func (m *mockStore) GetStats(ctx context.Context, since time.Time) (models.StatsResult, error) {
	return m.statsResp.stats, m.statsResp.err
}

func (m *mockStore) Ping(ctx context.Context) error {
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	return e
}

func newHandler(st store.ArticleStore, sched *scheduler.Scheduler, pub *publisher.Client) *Handler {
	return New(st, sched, pub, &config.Config{}, slog.Default())
}

// noopScheduler returns immediately — avoids goroutine panics in async fetch tests.
func noopScheduler() *scheduler.Scheduler {
	// All nil deps: the goroutine will log and return early.
	return nil
}

func jsonBody(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestParseCategories(t *testing.T) {
	t.Run("absent uses defaults", func(t *testing.T) {
		cats, err := parseCategories("")
		require.NoError(t, err)
		assert.Nil(t, cats)
	})

	t.Run("trims and removes duplicates", func(t *testing.T) {
		cats, err := parseCategories(" 金融,科技/AI,金融 ")
		require.NoError(t, err)
		assert.Equal(t, []models.Category{models.CategoryFinance, models.CategoryTechAI}, cats)
	})

	t.Run("rejects unknown category", func(t *testing.T) {
		cats, err := parseCategories("金融,体育")
		require.Error(t, err)
		assert.Nil(t, cats)
		assert.Contains(t, err.Error(), `invalid category "体育"`)
	})

	t.Run("rejects separators without categories", func(t *testing.T) {
		cats, err := parseCategories(" , , ")
		require.Error(t, err)
		assert.Nil(t, cats)
	})
}

// ---------------------------------------------------------------------------
// requireStore
// ---------------------------------------------------------------------------

func TestRequireStore_NilStore_Returns503(t *testing.T) {
	h := newHandler(nil, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ---------------------------------------------------------------------------
// ListArticles
// ---------------------------------------------------------------------------

func TestListArticles_Success(t *testing.T) {
	m := &mockStore{
		listResp: struct {
			articles []models.ArticleRow
			total    int
			err      error
		}{
			articles: []models.ArticleRow{
				{ID: 1, Title: "Article 1", Category: models.CategoryFinance},
				{ID: 2, Title: "Article 2", Category: models.CategoryTechAI},
			},
			total: 2,
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp models.ArticleListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Articles, 2)
	assert.Equal(t, 2, resp.Total)
}

func TestListArticles_WithFilters(t *testing.T) {
	m := &mockStore{
		listResp: struct {
			articles []models.ArticleRow
			total    int
			err      error
		}{
			articles: []models.ArticleRow{{ID: 1, Title: "Finance News"}},
			total:    1,
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles?category=金融&page=1&page_size=10&status=published", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListArticles_InvalidDateFrom(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles?date_from=invalid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListArticles_InvalidPage(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles?page=-1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListArticles_InvalidPageSize(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles?page_size=200", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListArticles_DBError(t *testing.T) {
	m := &mockStore{
		listResp: struct {
			articles []models.ArticleRow
			total    int
			err      error
		}{
			err: errors.New("db down"),
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.ListArticles(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// GetArticle
// ---------------------------------------------------------------------------

func TestGetArticle_Success(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			article: models.ArticleRow{ID: 1, Title: "Test Article"},
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.GetArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetArticle_NotFound(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			err: store.ErrNotFound,
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	err := h.GetArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetArticle_InvalidID(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("invalid")

	err := h.GetArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetArticle_DBError(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			err: errors.New("db error"),
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.GetArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// PublishArticle
// ---------------------------------------------------------------------------

func TestPublishArticle_PublisherDisabled(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			article: models.ArticleRow{ID: 1},
		},
	}
	h := newHandler(m, nil, nil) // no publisher
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.PublishArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestPublishArticle_NotFound(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			err: store.ErrNotFound,
		},
	}
	h := newHandler(m, nil, &publisher.Client{})
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	err := h.PublishArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPublishArticle_Success(t *testing.T) {
	// Mock website API that returns 201
	websiteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/agent/articles", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":42,"source_url":"https://example.com/article","status":"published"}`))
	}))
	t.Cleanup(websiteSrv.Close)

	pub := publisher.New(websiteSrv.URL, "test-token", nil, slog.Default())

	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			article: models.ArticleRow{
				ID: 1, SourceURL: "https://example.com/article",
				Title: "Test Article", SourceDomain: "example.com",
				RunID: "run-1",
			},
		},
		markPubErr: nil,
	}
	h := newHandler(m, nil, pub)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.PublishArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, true, body["published"])
	assert.Equal(t, float64(42), body["external_id"])
}

// ---------------------------------------------------------------------------
// RetryArticle
// ---------------------------------------------------------------------------

func TestRetryArticle_Success(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			article: models.ArticleRow{ID: 1, Status: "failed"},
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.RetryArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRetryArticle_NotFailed_ReturnsConflict(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			article: models.ArticleRow{ID: 1, Status: "published"},
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.RetryArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestRetryArticle_NotFound(t *testing.T) {
	m := &mockStore{
		getResp: struct {
			article models.ArticleRow
			err     error
		}{
			err: store.ErrNotFound,
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	err := h.RetryArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------------------------------------------------------------------------
// DeleteArticle
// ---------------------------------------------------------------------------

func TestDeleteArticle_Success(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.DeleteArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteArticle_NotFound(t *testing.T) {
	m := &mockStore{deleteErr: store.ErrNotFound}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	err := h.DeleteArticle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------------------------------------------------------------------------
// GetStats
// ---------------------------------------------------------------------------

func TestGetStats_Success(t *testing.T) {
	now := time.Now().UTC()
	m := &mockStore{
		statsResp: struct {
			stats models.StatsResult
			err   error
		}{
			stats: models.StatsResult{
				ByDay: []models.DayStat{
					{Date: now.Format("2006-01-02"), Count: 5},
				},
				ByCategory: []models.CategoryStat{
					{Category: models.CategoryFinance, Count: 3},
				},
			},
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.GetStats(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp models.StatsResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.ByDay, 1)
	assert.Len(t, resp.ByCategory, 1)
}

func TestGetStats_InvalidSince(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/stats?since=bad-date", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.GetStats(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetStats_DBError(t *testing.T) {
	m := &mockStore{
		statsResp: struct {
			stats models.StatsResult
			err   error
		}{
			err: errors.New("db error"),
		},
	}
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.GetStats(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// StreamFetch
// ---------------------------------------------------------------------------

func TestStreamFetch_AlreadyRunning_ReturnsErrorEvent(t *testing.T) {
	h := newHandler(nil, nil, nil)
	h.pipelineRunning = true

	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/fetch/stream", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.StreamFetch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Should have SSE event with error message
	body := rec.Body.String()
	assert.Contains(t, body, `"stage":"error"`)
	assert.Contains(t, body, `"status":"error"`)
	assert.Contains(t, body, "pipeline already running")
}

func TestStreamFetch_InvalidCategory_ReturnsBadRequest(t *testing.T) {
	h := newHandler(nil, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/fetch/stream?categories=体育", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.StreamFetch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"invalid_param"`)
	assert.NotContains(t, rec.Header().Get("Content-Type"), "text/event-stream")
	assert.False(t, h.pipelineRunning)
}

func TestStreamFetch_Success_WithRealScheduler(t *testing.T) {
	sched := scheduler.New(nil, nil, nil, nil, nil, &config.Config{}, slog.Default())
	h := newHandler(nil, sched, nil)

	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/fetch/stream", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.StreamFetch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Check SSE headers
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

	// Should have at least one SSE event (fetch stage will fire before failing)
	body := rec.Body.String()
	assert.Contains(t, body, "data: ")
	assert.Contains(t, body, `"stage"`)
}

func TestStreamFetch_ResetsPipelineRunning(t *testing.T) {
	sched := scheduler.New(nil, nil, nil, nil, nil, &config.Config{}, slog.Default())
	h := newHandler(nil, sched, nil)

	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/fetch/stream", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = h.StreamFetch(c)

	// Pipeline should be marked as not running after completion
	time.Sleep(50 * time.Millisecond)
	h.pipelineMu.Lock()
	running := h.pipelineRunning
	activeRunID := h.activeRunID
	h.pipelineMu.Unlock()
	assert.False(t, running, "pipeline should be marked as not running after stream completion")
	assert.Empty(t, activeRunID, "active run ID should be cleared after stream completion")
}
// ---------------------------------------------------------------------------

func TestTriggerFetch_Success(t *testing.T) {
	// Build a real scheduler with nil deps but real logger so the goroutine
	// doesn't panic — it will log once and return early when mgr.FetchAll fails.
	sched := scheduler.New(nil, nil, nil, nil, nil, &config.Config{}, slog.Default())
	h := newHandler(nil, sched, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/api/fetch", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.TriggerFetch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp models.FetchTriggerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Triggered)
	assert.NotEmpty(t, resp.RunID)

	// Give the goroutine time to complete and verify pipelineRunning is reset.
	time.Sleep(50 * time.Millisecond)
	h.pipelineMu.Lock()
	running := h.pipelineRunning
	activeRunID := h.activeRunID
	h.pipelineMu.Unlock()
	assert.False(t, running, "pipeline should be marked as not running after completion")
	assert.Empty(t, activeRunID, "active run ID should be cleared after completion")
}

func TestTriggerFetch_InvalidCategory_ReturnsBadRequest(t *testing.T) {
	h := newHandler(nil, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/api/fetch?categories=体育", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.TriggerFetch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"invalid_param"`)
	assert.False(t, h.pipelineRunning)
}

func TestTriggerFetch_AlreadyRunning(t *testing.T) {
	h := newHandler(nil, nil, nil)
	h.pipelineRunning = true
	e := newEcho()
	req := httptest.NewRequest(http.MethodPost, "/api/fetch", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.TriggerFetch(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// ---------------------------------------------------------------------------
// GetFetchStatus
// ---------------------------------------------------------------------------

func TestGetFetchStatus_MissingRunID(t *testing.T) {
  h := newHandler(nil, nil, nil)
  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusBadRequest, rec.Code)
 }

 func TestGetFetchStatus_InMemoryResult(t *testing.T) {
  h := newHandler(nil, nil, nil)
  h.runs["test-run-1"] = models.RunResult{
   RunID:          "test-run-1",
   TotalFetched:   42,
   TotalPublished: 10,
  }

  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("test-run-1")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusOK, rec.Code)

  var body map[string]any
  require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
  assert.Equal(t, "done", body["status"])
  assert.Equal(t, float64(42), body["fetched"])
 }

 func TestGetFetchStatus_Running_NoResultYet(t *testing.T) {
  h := newHandler(nil, nil, nil)
  h.pipelineRunning = true
  h.activeRunID = "fresh-run"

  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("fresh-run")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusOK, rec.Code)

  var body map[string]any
  require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
  assert.Equal(t, "running", body["status"])
 }

 func TestGetFetchStatus_DifferentRunIsNotReportedAsRunning(t *testing.T) {
  h := newHandler(nil, nil, nil)
  h.pipelineRunning = true
  h.activeRunID = "actual-run"

  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("different-run")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusNotFound, rec.Code)
 }

 func TestGetFetchStatus_DBFallback(t *testing.T) {
  m := &mockStore{
   runLogs: map[string]models.RunLogRow{
    "db-run": {
     RunID:          "db-run",
     TotalFetched:   99,
     TotalPublished: 50,
     StartedAt:      time.Now().UTC().Add(-5 * time.Minute),
     FinishedAt:     time.Now().UTC(),
    },
   },
  }
  h := newHandler(m, nil, nil)

  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("db-run")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusOK, rec.Code)

  var body map[string]any
  require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
  assert.Equal(t, "done", body["status"])
  assert.Equal(t, float64(99), body["fetched"])
 }

 func TestGetFetchStatus_DBNotFound_NoDB(t *testing.T) {
  h := newHandler(nil, nil, nil)

  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("nonexistent")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusNotFound, rec.Code)
 }

 func TestGetFetchStatus_DBNotFound_WithDB(t *testing.T) {
  m := &mockStore{} // no run logs configured
  h := newHandler(m, nil, nil)

  e := newEcho()
  req := httptest.NewRequest(http.MethodGet, "/", nil)
  rec := httptest.NewRecorder()
  c := e.NewContext(req, rec)
  c.SetParamNames("run_id")
  c.SetParamValues("nonexistent")

  err := h.GetFetchStatus(c)
  require.NoError(t, err)
  assert.Equal(t, http.StatusNotFound, rec.Code)
 }

// ---------------------------------------------------------------------------
// fetchStatusFromResult
// ---------------------------------------------------------------------------

func TestFetchStatusFromResult_WithFatalError(t *testing.T) {
	r := models.RunResult{
		RunID:          "test-run",
		TotalFetched:   10,
		TotalProcessed: 8,
		TotalPublished: 5,
		TotalSkipped:   2,
		TotalFailed:    1,
		FatalError:     errors.New("something went wrong"),
		DurationMs:     5000,
	}
	m := fetchStatusFromResult(r)
	assert.Equal(t, "error", m["status"])
	assert.Equal(t, "something went wrong", m["fatal_error"])
}

func TestFetchStatusFromResult_NoError(t *testing.T) {
	r := models.RunResult{
		RunID:          "test-run",
		TotalFetched:   10,
		TotalPublished: 5,
	}
	m := fetchStatusFromResult(r)
	assert.Equal(t, "done", m["status"])
	assert.Equal(t, "", m["fatal_error"])
	assert.EqualValues(t, 10, m["fetched"])
	assert.EqualValues(t, 5, m["published"])
}

// ---------------------------------------------------------------------------
// rowToProcessedArticle
// ---------------------------------------------------------------------------

func TestRowToProcessedArticle_WithPublishedAt(t *testing.T) {
	now := time.Now()
	row := models.ArticleRow{
		ID: 1, SourceURL: "https://example.com", Title: "Test",
		SourceDomain: "example.com", SourceType: "rss",
		Summary: "摘要", Category: models.CategoryFinance,
		CredibilityScore: 0.85, PublishedAt: &now,
	}
	pa := rowToProcessedArticle(row)
	assert.Equal(t, row.SourceURL, pa.Raw.URL)
	assert.Equal(t, row.Summary, pa.Summary)
	assert.Equal(t, row.Category, pa.Category)
	assert.NotNil(t, pa.Raw.PublishedAt)
}

func TestRowToProcessedArticle_NilPublishedAt(t *testing.T) {
	row := models.ArticleRow{
		ID: 1, SourceURL: "https://example.com", Title: "Test",
		SourceDomain: "example.com",
	}
	pa := rowToProcessedArticle(row)
	assert.True(t, pa.Raw.PublishedAt.IsZero())
}

// ---------------------------------------------------------------------------
// parseID
// ---------------------------------------------------------------------------

func TestParseID_Invalid(t *testing.T) {
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	_, err := parseID(c)
	assert.Error(t, err)
}

func TestParseID_Negative(t *testing.T) {
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("-1")

	// ParseInt accepts negative numbers — this is valid.
	id, err := parseID(c)
	assert.NoError(t, err)
	assert.Equal(t, int64(-1), id)
}

func TestParseID_Valid(t *testing.T) {
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("42")

	id, err := parseID(c)
	assert.NoError(t, err)
	assert.Equal(t, int64(42), id)
}

// ---------------------------------------------------------------------------
// errJSON
// ---------------------------------------------------------------------------

func TestErrJSON(t *testing.T) {
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := errJSON(c, http.StatusBadRequest, "test_error", "test message")
	require.NoError(t, err)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "test_error", body["error"])
	assert.Equal(t, "test message", body["message"])
}

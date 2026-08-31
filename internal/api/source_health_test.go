package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// stubHealthProvider injects live snapshots without a real scheduler.
type stubHealthProvider struct {
	snaps []fetcher.HealthSnapshot
}

func (s *stubHealthProvider) SourceHealth() []fetcher.HealthSnapshot { return s.snaps }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestHostOf(t *testing.T) {
	assert.Equal(t, "example.com", hostOf("https://Example.com:443/feed.xml"))
	assert.Equal(t, "rss.example.com", hostOf("http://rss.example.com/rss"))
	assert.Empty(t, hostOf("/wallstreetcn/news/global")) // RSSHub route path
	assert.Empty(t, hostOf("not a url"))
}

// ---------------------------------------------------------------------------
// GetSourceHealth
// ---------------------------------------------------------------------------

func TestGetSourceHealth_MergesLiveAndDB(t *testing.T) {
	okAt := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	m := &mockStore{
		activity: []models.SourceActivity{
			{Domain: "Example.com", Articles: 12, LastFetchedAt: okAt}, // case differs on purpose
			{Domain: "news.ycombinator.com", Articles: 3, LastFetchedAt: okAt},
		},
	}
	h := newHandler(m, nil, nil)
	h.sourceHealth = &stubHealthProvider{snaps: []fetcher.HealthSnapshot{
		{
			Source: "https://example.com/feed.xml", ConsecutiveFailures: 0, Skipped: false,
			TotalAttempts: 10, TotalFailures: 1, LastOutcome: "ok",
			LastAttemptAt: okAt, LastSuccessAt: okAt,
		},
		{
			Source: "https://broken.net/rss", ConsecutiveFailures: 3, Skipped: true,
			TotalAttempts: 3, TotalFailures: 3, LastOutcome: "error", LastError: "dial timeout",
			LastAttemptAt: okAt,
		},
	}}
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/sources/health", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.GetSourceHealth(e.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp models.SourceHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Sources, 3)
	assert.Equal(t, 7, resp.WindowDays)

	// Ordering: disabled first.
	assert.Equal(t, "disabled", resp.Sources[0].Status)
	assert.Equal(t, "https://broken.net/rss", resp.Sources[0].Source)
	assert.Equal(t, 3, resp.Sources[0].ConsecutiveFailures)
	assert.Equal(t, "dial timeout", resp.Sources[0].LastError)
	assert.Nil(t, resp.Sources[0].LastSuccessAt)

	// Live ok source merged with DB activity (domain normalised).
	bySrc := map[string]models.SourceHealthRow{}
	for _, r := range resp.Sources {
		bySrc[r.Source] = r
	}
	live := bySrc["https://example.com/feed.xml"]
	assert.Equal(t, "ok", live.Status)
	assert.Equal(t, "example.com", live.Domain)
	assert.Equal(t, 12, live.RecentArticles)
	require.NotNil(t, live.LastArticleAt)
	assert.Equal(t, "2026-08-20T10:00:00Z", live.LastArticleAt.Format(time.RFC3339))

	// DB-only domain degrades to "unknown".
	dbOnly := bySrc["news.ycombinator.com"]
	assert.Equal(t, "unknown", dbOnly.Status)
	assert.Equal(t, 3, dbOnly.RecentArticles)
}

func TestGetSourceHealth_WarningState(t *testing.T) {
	at := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	h := newHandler(&mockStore{}, nil, nil)
	h.sourceHealth = &stubHealthProvider{snaps: []fetcher.HealthSnapshot{
		{Source: "https://flaky.io/rss", ConsecutiveFailures: 2, Skipped: false, TotalAttempts: 5, LastOutcome: "error", LastAttemptAt: at},
	}}
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/sources/health", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.GetSourceHealth(e.NewContext(req, rec)))

	var resp models.SourceHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Sources, 1)
	assert.Equal(t, "warning", resp.Sources[0].Status)
	assert.Equal(t, 2, resp.Sources[0].ConsecutiveFailures)
}

func TestGetSourceHealth_EmptyEverything(t *testing.T) {
	// No scheduler, no store, no DB rows: graceful empty list, not an error.
	h := newHandler(nil, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/sources/health", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.GetSourceHealth(e.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp models.SourceHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Sources)
}

func TestGetSourceHealth_DBErrorServesLiveOnly(t *testing.T) {
	h := newHandler(&mockStore{activityErr: errors.New("connection refused")}, nil, nil)
	h.sourceHealth = &stubHealthProvider{snaps: []fetcher.HealthSnapshot{
		{Source: "https://example.com/feed.xml", TotalAttempts: 4},
	}}
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/sources/health", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.GetSourceHealth(e.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp models.SourceHealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Sources, 1)
	assert.Equal(t, "ok", resp.Sources[0].Status)
}

func TestGetSourceHealth_RouteRegistered(t *testing.T) {
	e := echo.New()
	g := e.Group("/api")
	h := newHandler(&mockStore{}, nil, nil)
	h.sourceHealth = &stubHealthProvider{}
	h.Register(g)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

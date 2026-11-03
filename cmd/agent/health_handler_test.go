package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/internal/health"
	"github.com/user/daily-info-agent/internal/store"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestHealthHandler_Healthy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	_, err := health.NewStore(path).Record(models.RunResult{
		RunID:        "run-ok",
		TotalFetched: 8,
		StartedAt:    time.Now().Add(-time.Hour),
		FinishedAt:   time.Now().Add(-30 * time.Minute),
	}, 0)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	handler := healthHandler("test", nil, time.Now(), "", true, health.NewStore(path))

	require.NoError(t, handler(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])

	freshness, ok := body["freshness"].(map[string]any)
	require.True(t, ok, "freshness should be an object")
	assert.Equal(t, true, freshness["healthy"])
}

func TestHealthHandler_StaleRunFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	_, err := health.NewStore(path).Record(models.RunResult{
		RunID:        "run-old",
		TotalFetched: 8,
		StartedAt:    time.Now().Add(-72 * time.Hour),
		FinishedAt:   time.Now().Add(-48 * time.Hour),
	}, 0)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	handler := healthHandler("test", nil, time.Now(), "", true, health.NewStore(path))

	require.NoError(t, handler(c))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "degraded", body["status"])

	freshness, ok := body["freshness"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, freshness["healthy"])
}

func TestHealthHandler_DatabaseErrorFails(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	handler := healthHandler("test", &failingStore{}, time.Now(), "", true, health.NewStore(filepath.Join(t.TempDir(), "run-health.json")))

	require.NoError(t, handler(c))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHealthHandler_EmptyCacheStillHealthy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	_, err := health.NewStore(path).Record(models.RunResult{
		RunID:        "run-ok",
		TotalFetched: 8,
		StartedAt:    time.Now().Add(-time.Hour),
		FinishedAt:   time.Now().Add(-30 * time.Minute),
	}, 0)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	handler := healthHandler("test", nil, time.Now(), filepath.Join(t.TempDir(), "missing.json"), true, health.NewStore(path))

	require.NoError(t, handler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
}

type failingStore struct {
	store.ArticleStore
}

func (f *failingStore) Ping(_ context.Context) error {
	return assert.AnError
}

var _ store.ArticleStore = (*failingStore)(nil)

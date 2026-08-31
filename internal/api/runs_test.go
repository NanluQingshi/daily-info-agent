package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/user/daily-info-agent/pkg/models"
)

func mkRunLog(id string, fetched, extracted, failed int, started time.Time) models.RunLogRow {
	return models.RunLogRow{
		RunID:          id,
		TotalFetched:   fetched,
		TotalExtracted: extracted,
		TotalProcessed: fetched,
		TotalFailed:    failed,
		DurationMs:     45_000,
		FatalError:     "",
		StartedAt:      started,
		FinishedAt:     started.Add(45 * time.Second),
	}
}

func TestGetRuns_ReturnsRunLogs(t *testing.T) {
	now := time.Now().UTC()
	m := &mockStore{}
	m.listRunsResp.runs = []models.RunLogRow{
		mkRunLog("run-2", 40, 35, 1, now),
		mkRunLog("run-1", 30, 28, 0, now.Add(-time.Hour)),
	}

	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.GetRuns(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp RunListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Runs, 2)
	assert.Equal(t, "run-2", resp.Runs[0].RunID)
	assert.Equal(t, 35, resp.Runs[0].TotalExtracted)
	assert.Equal(t, 1, resp.Runs[0].TotalFailed)
	assert.Equal(t, "run-1", resp.Runs[1].RunID)
	assert.Equal(t, 0, resp.Runs[1].TotalFailed)
}

func TestGetRuns_Empty_ReturnsEmptyArray(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.GetRuns(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp RunListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Runs)
	assert.NotNil(t, resp.Runs) // [] not null, so the panel can map over it
}

func TestGetRuns_LimitValidation(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()

	for _, q := range []string{"?limit=0", "?limit=101", "?limit=abc", "?limit=-1"} {
		req := httptest.NewRequest(http.MethodGet, "/api/runs"+q, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		require.NoError(t, h.GetRuns(c), q)
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
	}
}

func TestGetRuns_DBError_500(t *testing.T) {
	m := &mockStore{}
	m.listRunsResp.err = errors.New("db down")
	h := newHandler(m, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.GetRuns(c))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetRuns_NilStore_503(t *testing.T) {
	h := newHandler(nil, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = h.GetRuns(c) //nolint:errcheck // echo pattern
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestGetRuns_RouteRegistered(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	h.Register(e.Group("/api"))

	req := httptest.NewRequest(http.MethodGet, "/api/runs?limit=5", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/user/daily-info-agent/pkg/metrics"
	"github.com/user/daily-info-agent/pkg/models"
)

func callFeedback(t *testing.T, m *mockStore, method, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := newHandler(m, nil, nil)
	e := newEcho()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/x", nil)
	} else {
		req = httptest.NewRequest(method, "/x", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)

	var err error
	switch method {
	case http.MethodPost:
		err = h.SubmitFeedback(c)
	case http.MethodGet:
		if id == "stats" {
			c.SetParamNames()
			c.SetParamValues()
			err = h.GetFeedbackStats(c)
		} else {
			err = h.GetFeedback(c)
		}
	default:
		t.Fatalf("unsupported method %s", method)
	}
	require.NoError(t, err)
	return rec
}

func TestSubmitFeedback_Valid(t *testing.T) {
	metrics.App.FeedbackUp.Store(0)
	rec := callFeedback(t, &mockStore{}, http.MethodPost, "5", `{"kind":"summary","rating":1}`)
	require.Equal(t, http.StatusOK, rec.Code)

	var got models.ArticleFeedbackRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, int64(5), got.ArticleID)
	assert.Equal(t, "summary", got.Kind)
	assert.Equal(t, int16(1), got.Rating)
	assert.Equal(t, int64(1), metrics.App.FeedbackUp.Load())
}

func TestSubmitFeedback_Down(t *testing.T) {
	metrics.App.FeedbackDown.Store(0)
	rec := callFeedback(t, &mockStore{}, http.MethodPost, "5", `{"kind":"category","rating":-1}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(1), metrics.App.FeedbackDown.Load())
}

func TestSubmitFeedback_RejectsBadKindAndRating(t *testing.T) {
	m := &mockStore{}
	for _, body := range []string{
		`{"kind":"title","rating":1}`,
		`{"kind":"summary","rating":0}`,
		`{"kind":"summary","rating":2}`,
	} {
		rec := callFeedback(t, m, http.MethodPost, "5", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

func TestSubmitFeedback_BadIDAndDBError(t *testing.T) {
	rec := callFeedback(t, &mockStore{}, http.MethodPost, "abc", `{"kind":"summary","rating":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	m := &mockStore{}
	m.feedbackResp.err = errors.New("db down")
	rec = callFeedback(t, m, http.MethodPost, "5", `{"kind":"summary","rating":1}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetFeedback_ReturnsRows(t *testing.T) {
	m := &mockStore{}
	m.feedbackResp.list = []models.ArticleFeedbackRow{
		{ID: 1, ArticleID: 5, Kind: "category", Rating: -1},
		{ID: 2, ArticleID: 5, Kind: "summary", Rating: 1},
	}
	rec := callFeedback(t, m, http.MethodGet, "5", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Feedback []models.ArticleFeedbackRow `json:"feedback"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Len(t, got.Feedback, 2)
}

func TestGetFeedbackStats_Aggregates(t *testing.T) {
	m := &mockStore{}
	m.feedbackResp.stats = []models.FeedbackStat{
		{Kind: "category", Up: 2, Down: 1},
		{Kind: "summary", Up: 0, Down: 3},
	}
	rec := callFeedback(t, m, http.MethodGet, "stats", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got models.FeedbackStatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Stats, 2)
	assert.Equal(t, 3, got.Stats[1].Down)
}

func TestFeedback_RoutesRegistered(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()
	h.Register(e.Group("/api"))

	req := httptest.NewRequest(http.MethodPost, "/api/articles/5/feedback",
		strings.NewReader(`{"kind":"summary","rating":1}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/api/feedback/stats", nil)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

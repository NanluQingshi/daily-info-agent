package scheduler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"

	"github.com/user/daily-info-agent/internal/extract"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestExtractStage_NilExtractorSkipped(t *testing.T) {
	s := &Scheduler{logger: slog.Default()}
	var events []models.ProgressEvent
	fire := func(e models.ProgressEvent) { events = append(events, e) }

	items := []models.RawItem{{URL: "https://example.com/a"}}
	s.extractStage(context.Background(), items, "run-1", fire)

	assert.Empty(t, events, "no events should fire when extractor is nil")
	assert.Empty(t, items[0].ContentText)
}

func TestExtractStage_EnrichesAndEmitsEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><article><p>" +
			strings.Repeat("正文内容测试。", 100) +
			"</p></article></body></html>"))
	}))
	defer srv.Close()

	x := extract.New(srv.Client(), 5, 2, nil)
	s := &Scheduler{logger: slog.Default(), ext: x}

	var events []models.ProgressEvent
	fire := func(e models.ProgressEvent) { events = append(events, e) }

	items := []models.RawItem{{URL: srv.URL, Title: "A", Description: "d"}}
	s.extractStage(context.Background(), items, "run-1", fire)

	require.Len(t, events, 2)
	assert.Equal(t, "extract", events[0].Stage)
	assert.Equal(t, "running", events[0].Status)
	assert.Equal(t, "extract", events[1].Stage)
	assert.Equal(t, "done", events[1].Status)
	assert.Equal(t, 1, events[1].Count)

	assert.NotEmpty(t, items[0].ContentText, "item should be enriched in place")
}

func TestExtractStage_FailuresDegradeGracefully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	x := extract.New(srv.Client(), 5, 2, nil)
	s := &Scheduler{logger: slog.Default(), ext: x}

	var events []models.ProgressEvent
	fire := func(e models.ProgressEvent) { events = append(events, e) }

	items := []models.RawItem{{URL: srv.URL, Title: "A", Description: "d"}}
	s.extractStage(context.Background(), items, "run-1", fire)

	require.Len(t, events, 2)
	assert.Equal(t, "done", events[1].Status)
	assert.Equal(t, 0, events[1].Count, "failed extraction must not count")
	assert.Empty(t, items[0].ContentText)
}

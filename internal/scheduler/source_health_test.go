package scheduler_test

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/user/daily-info-agent/pkg/models"
)

// TestScheduler_SourceHealth_ExposesManagerState: after a successful run the
// accessor surfaces the manager's per-source snapshot (source ok, no failures).
func TestScheduler_SourceHealth_ExposesManagerState(t *testing.T) {
	publishCalls := &atomic.Int32{}
	publishSrv := httptest.NewServer(publishSuccessHandler(publishCalls))
	defer publishSrv.Close()

	items := []models.RawItem{
		makeRawItem("http://reuters.com/article/health-1", "reuters.com"),
	}
	sched := buildTestScheduler(
		t,
		`[{"url":"http://reuters.com/article/health-1","category":"金融","summary":"摘要","credibility_score":0.95,"tags":["x"],"language":"en"}]`,
		items,
		[]string{"reuters.com"},
		publishSrv,
	)

	// Before any run the snapshot may be empty; after a run it must contain
	// the fetched source with a clean bill of health.
	result := sched.RunForCategories(t.Context(), []models.Category{models.CategoryFinance})
	if result.FatalError != nil {
		t.Fatalf("unexpected fatal error: %v", result.FatalError)
	}

	snaps := sched.SourceHealth()
	if len(snaps) == 0 {
		t.Fatal("SourceHealth() returned no sources after a successful run")
	}
	for _, s := range snaps {
		if s.Skipped {
			t.Errorf("source %q unexpectedly disabled", s.Source)
		}
		if s.ConsecutiveFailures != 0 {
			t.Errorf("source %q has %d consecutive failures after success", s.Source, s.ConsecutiveFailures)
		}
		if s.LastOutcome != "ok" {
			t.Errorf("source %q last outcome = %q, want ok", s.Source, s.LastOutcome)
		}
	}
}

package health

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestRecord_FirstSuccess(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	snap, err := store.Record(models.RunResult{
		RunID:        "run-1",
		TotalFetched: 12,
		StartedAt:    time.Now().Add(-time.Minute),
		FinishedAt:   time.Now(),
	}, 0)
	require.NoError(t, err)

	assert.Equal(t, "success", snap.LastOutcome)
	assert.Equal(t, 1, snap.ConsecutiveSuccess)
	assert.Equal(t, 0, snap.ConsecutiveFailure)
	assert.Equal(t, "run-1", snap.LastRunID)
}

func TestRecord_EmptyRunCountsAsFailure(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	snap, err := store.Record(models.RunResult{RunID: "run-empty", TotalFetched: 0}, 0)
	require.NoError(t, err)

	assert.Equal(t, "empty", snap.LastOutcome)
	assert.Equal(t, 1, snap.ConsecutiveFailure)
	assert.Equal(t, 0, snap.ConsecutiveSuccess)
}

func TestRecord_FatalRunCountsAsFailure(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	snap, err := store.Record(models.RunResult{
		RunID:      "run-fatal",
		FatalError: errors.New("all sources failed"),
	}, 0)
	require.NoError(t, err)

	assert.Equal(t, "failure", snap.LastOutcome)
	assert.Equal(t, 1, snap.ConsecutiveFailure)
	assert.Equal(t, "all sources failed", snap.LastFatalError)
}

func TestRecord_ConsecutiveFailuresSurviveProcessBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")

	for i := 0; i < 3; i++ {
		_, err := NewStore(path).Record(models.RunResult{
			RunID:        "run-" + string(rune('a'+i)),
			TotalFetched: 0,
		}, 0)
		require.NoError(t, err)
	}

	snap, err := NewStore(path).Read()
	require.NoError(t, err)
	assert.Equal(t, 3, snap.ConsecutiveFailure)
}

func TestRecord_SuccessResetsFailureStreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	store := NewStore(path)

	_, err := store.Record(models.RunResult{TotalFetched: 0}, 0)
	require.NoError(t, err)
	snap, err := store.Record(models.RunResult{RunID: "run-ok", TotalFetched: 5}, 0)
	require.NoError(t, err)

	assert.Equal(t, 0, snap.ConsecutiveFailure)
	assert.Equal(t, 1, snap.ConsecutiveSuccess)
}

func TestCheck_NoPreviousRun(t *testing.T) {
	report, err := NewStore(filepath.Join(t.TempDir(), "run-health.json")).Check(24 * time.Hour)
	require.NoError(t, err)

	assert.False(t, report.Healthy)
	assert.Equal(t, "no previous run recorded", report.Reason)
}

func TestCheck_StaleSuccessFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	finished := time.Now().Add(-48 * time.Hour)
	store := NewStore(path)
	_, err := store.Record(models.RunResult{
		RunID:        "run-old",
		TotalFetched: 10,
		StartedAt:    finished.Add(-time.Minute),
		FinishedAt:   finished,
	}, 0)
	require.NoError(t, err)

	report, err := store.Check(24 * time.Hour)
	require.NoError(t, err)

	assert.False(t, report.Healthy)
	assert.Contains(t, report.Reason, "last successful run is")
}

func TestCheck_RecentSuccessPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	store := NewStore(path)
	_, err := store.Record(models.RunResult{
		RunID:        "run-recent",
		TotalFetched: 10,
		StartedAt:    time.Now().Add(-time.Hour),
		FinishedAt:   time.Now().Add(-30 * time.Minute),
	}, 0)
	require.NoError(t, err)

	report, err := store.Check(24 * time.Hour)
	require.NoError(t, err)

	assert.True(t, report.Healthy)
	assert.Equal(t, "success", report.LastOutcome)
}

func TestCheck_EmptyOutcomeFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	_, err := NewStore(path).Record(models.RunResult{RunID: "run-empty", TotalFetched: 0}, 0)
	require.NoError(t, err)

	report, err := NewStore(path).Check(24 * time.Hour)
	require.NoError(t, err)

	assert.False(t, report.Healthy)
	assert.Equal(t, `last run outcome was "empty"`, report.Reason)
}

func TestRead_CorruptStoreIsUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	require.NoError(t, os.WriteFile(path, []byte("{not-json"), 0o644))

	_, err := NewStore(path).Read()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnreadable)
}

func TestStoreFileIsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-health.json")
	_, err := NewStore(path).Record(models.RunResult{
		RunID:        "run-json",
		TotalFetched: 3,
		StartedAt:    time.Now(),
		FinishedAt:   time.Now(),
	}, 0)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "last_outcome")
	assert.Equal(t, "success", raw["last_outcome"])
}

func TestRecord_ThinRunIsEmptyWhenThresholdSet(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	// Two fetched items is a real run, but not enough to trust when the
	// operator expects at least five.
	snap, err := store.Record(models.RunResult{RunID: "run-thin", TotalFetched: 2}, 5)
	require.NoError(t, err)

	assert.Equal(t, "empty", snap.LastOutcome)
	assert.Equal(t, 1, snap.ConsecutiveFailure)
	assert.Equal(t, 0, snap.ConsecutiveSuccess)
}

func TestRecord_ThresholdDoesNotAffectFatal(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	snap, err := store.Record(models.RunResult{
		RunID:        "run-fatal",
		TotalFetched: 100,
		FatalError:   errors.New("publish failed"),
	}, 5)
	require.NoError(t, err)

	assert.Equal(t, "failure", snap.LastOutcome)
	assert.Equal(t, 1, snap.ConsecutiveFailure)
}

func TestRecord_AllLLMSkippedIsFailure(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	// The pipeline fetched items but the AI never returned anything for them.
	// That is a provider outage, not a successful run.
	snap, err := store.Record(models.RunResult{
		RunID:           "run-llm-down",
		TotalFetched:    200,
		TotalLLMSkipped: 200,
	}, 0)
	require.NoError(t, err)

	assert.Equal(t, "failure", snap.LastOutcome)
	assert.Equal(t, 1, snap.ConsecutiveFailure)
}

func TestRecord_SnapshotCarriesPipelineCounters(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	snap, err := store.Record(models.RunResult{
		RunID:           "run-counters",
		TotalFetched:    200,
		TotalLLMSkipped: 5,
		TotalFailed:     4,
	}, 0)
	require.NoError(t, err)

	assert.Equal(t, 200, snap.TotalFetched)
	assert.Equal(t, 5, snap.TotalLLMSkipped)
	assert.Equal(t, 4, snap.SourceFailed)
}

// A failed run must not destroy the freshness baseline. If it did, a single
// empty run would mark every later run unhealthy and the pipeline could never
// recover, because the next run would fail against its own failed record.
func TestRecord_FailurePreservesSuccessBaseline(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "run-health.json"))

	first, err := store.Record(models.RunResult{
		RunID: "run-ok", TotalFetched: 50,
		StartedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now().Add(-time.Hour),
	}, 10)
	require.NoError(t, err)
	require.False(t, first.LastSuccessFinishedAt.IsZero())

	second, err := store.Record(models.RunResult{
		RunID: "run-empty", TotalFetched: 0,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}, 10)
	require.NoError(t, err)
	assert.Equal(t, "empty", second.LastOutcome)
	assert.Equal(t, 1, second.ConsecutiveFailure)
	assert.Equal(t, first.LastSuccessFinishedAt, second.LastSuccessFinishedAt,
		"the freshness baseline must survive a failed run")

	// A later success must reset the streak normally.
	third, err := store.Record(models.RunResult{
		RunID: "run-ok-2", TotalFetched: 60,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	}, 10)
	require.NoError(t, err)
	assert.Equal(t, "success", third.LastOutcome)
	assert.Equal(t, 0, third.ConsecutiveFailure)
	assert.Equal(t, 1, third.ConsecutiveSuccess)
}

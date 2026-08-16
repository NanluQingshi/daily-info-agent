package scheduler

import (
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestTrackFailure_ResetsOnSuccess(t *testing.T) {
	s := &Scheduler{logger: slog.Default()}

	s.trackFailure(&models.RunResult{TotalFetched: 5}) // success
	assert.Equal(t, 0, s.consecutiveFail)

	s.trackFailure(&models.RunResult{}) // zero items = failure
	assert.Equal(t, 1, s.consecutiveFail)

	s.trackFailure(&models.RunResult{TotalFetched: 3}) // success resets
	assert.Equal(t, 0, s.consecutiveFail)
}

func TestTrackFailure_AlertFiresAtThreshold(t *testing.T) {
	var calls atomic.Int32
	s := &Scheduler{logger: slog.Default()}
	s.WithFailureAlert(2, func(n int) { calls.Add(1) })

	// First failure — below threshold, no alert.
	s.trackFailure(&models.RunResult{})
	assert.Equal(t, int32(0), calls.Load())

	// Second consecutive failure — threshold reached, alert fires.
	s.trackFailure(&models.RunResult{})
	// Alert callback runs in a goroutine; give it a moment.
	assert.Eventually(t, func() bool { return calls.Load() == 1 }, 2e9, 1e7)
}

func TestTrackFailure_FatalErrorCountsAsFailure(t *testing.T) {
	var calls atomic.Int32
	s := &Scheduler{logger: slog.Default()}
	s.WithFailureAlert(1, func(n int) { calls.Add(1) })

	s.trackFailure(&models.RunResult{FatalError: assert.AnError})
	assert.Eventually(t, func() bool { return calls.Load() == 1 }, 2e9, 1e7)
}

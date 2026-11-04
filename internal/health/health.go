// Package health persists the result of scheduled pipeline runs so failure
// detection can survive across GitHub Actions process boundaries.
package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
)

// StoreFile is the JSON file shared by scheduled runs through GitHub Actions
// cache. It deliberately stays tiny: it is a status signal, not an audit log.
const StoreFile = "cache/run-health.json"

var (
	// ErrNotFound means no previous run has been recorded.
	ErrNotFound = errors.New("run health not found")

	// ErrUnreadable means the store exists but cannot be decoded safely.
	ErrUnreadable = errors.New("run health is unreadable")
)

// Store is the persisted run-health signal consumed by the scheduler.
type Store struct {
	Path string
}

// NewStore returns a Store rooted at path. An empty path uses StoreFile.
func NewStore(path string) *Store {
	if path == "" {
		path = StoreFile
	}
	return &Store{Path: path}
}

// Snapshot is the durable part of one scheduled run.
type Snapshot struct {
	LastRunID          string    `json:"last_run_id"`
	LastStartedAt      time.Time `json:"last_started_at"`
	LastFinishedAt     time.Time `json:"last_finished_at"`
	LastOutcome        string    `json:"last_outcome"` // success | failure | empty
	LastFatalError     string    `json:"last_fatal_error,omitempty"`
	ConsecutiveSuccess int       `json:"consecutive_success"`
	ConsecutiveFailure int       `json:"consecutive_failure"`
	// LastSuccessFinishedAt is when the most recent successful non-empty run
	// finished. It is the freshness baseline and must survive failures:
	// otherwise a single empty run permanently marks the system unhealthy and
	// the pipeline can never recover.
	LastSuccessFinishedAt time.Time `json:"last_success_finished_at,omitempty"`
	TotalFetched          int       `json:"total_fetched"`
	TotalLLMSkipped       int       `json:"total_llm_skipped"`
	SourceFailed          int       `json:"source_failed"`
}

// Read loads the persisted snapshot. It returns ErrNotFound when the file is
// absent and a wrapped ErrUnreadable when the file exists but is invalid.
func (s *Store) Read() (Snapshot, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return snap, nil
}

// Record updates the snapshot for result. Empty runs and fatal runs are both
// failures; a successful non-empty run resets the failure streak. minExpected
// lets a run that fetched fewer than minExpected items be classified as "empty"
// instead of "success" (0 disables that check).
func (s *Store) Record(result models.RunResult, minExpected int) (Snapshot, error) {
	previous, err := s.Read()
	if err != nil && !errors.Is(err, ErrNotFound) {
		// A corrupt signal must not be silently overwritten. Return it so the
		// caller can surface an observability failure instead of pretending
		// the health state is known.
		return Snapshot{}, err
	}
	if errors.Is(err, ErrNotFound) {
		previous = Snapshot{}
	}

	outcome := classify(result, minExpected)
	snap := Snapshot{
		LastRunID:      result.RunID,
		LastOutcome:    outcome,
		LastFatalError: fatalMessage(result),
	}
	snap.TotalFetched = result.TotalFetched
	snap.TotalLLMSkipped = result.TotalLLMSkipped
	snap.SourceFailed = result.TotalFailed

	switch outcome {
	case "success":
		snap.ConsecutiveSuccess = previous.ConsecutiveSuccess + 1
		snap.ConsecutiveFailure = 0
	case "empty":
		snap.ConsecutiveFailure = previous.ConsecutiveFailure + 1
	case "failure":
		snap.ConsecutiveFailure = previous.ConsecutiveFailure + 1
	}

	// Preserve timing when the caller has not populated it. This keeps tests
	// and short-lived processes from clobbering the previous run's timestamp.
	if !result.StartedAt.IsZero() {
		snap.LastStartedAt = result.StartedAt.UTC()
	} else {
		snap.LastStartedAt = previous.LastStartedAt
	}
	if !result.FinishedAt.IsZero() {
		snap.LastFinishedAt = result.FinishedAt.UTC()
	} else {
		snap.LastFinishedAt = previous.LastFinishedAt
	}

	// The freshness baseline must survive failed runs. Without this, the gate
	// reads the failing run's own timestamp and a single empty run locks the
	// pipeline out permanently — every later run fails against its own record.
	if outcome == "success" {
		snap.LastSuccessFinishedAt = snap.LastFinishedAt
	} else {
		snap.LastSuccessFinishedAt = previous.LastSuccessFinishedAt
	}

	if err := s.write(snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// Check evaluates the previous run against freshness and outcome policy.
// maxAge is the maximum allowed gap since the last successful non-empty run.
// A previous missing/empty/unreadable store is treated as unknown, not healthy.
func (s *Store) Check(maxAge time.Duration) (Report, error) {
	previous, err := s.Read()
	if errors.Is(err, ErrNotFound) {
		return Report{
			Healthy: false,
			Reason:  "no previous run recorded",
		}, nil
	}
	if err != nil {
		return Report{Healthy: false, Reason: err.Error()}, err
	}

	if previous.LastOutcome != "success" {
		return Report{
			Healthy:            false,
			Reason:             fmt.Sprintf("last run outcome was %q", previous.LastOutcome),
			LastOutcome:        previous.LastOutcome,
			ConsecutiveFailure: previous.ConsecutiveFailure,
		}, nil
	}

	if previous.LastSuccessFinishedAt.IsZero() {
		return Report{
			Healthy:     false,
			Reason:      "last successful run has no finish timestamp",
			LastOutcome: previous.LastOutcome,
		}, nil
	}

	age := time.Since(previous.LastSuccessFinishedAt)
	if age > maxAge {
		return Report{
			Healthy:            false,
			Reason:             fmt.Sprintf("last successful run is %s old (limit %s)", age.Round(time.Minute), maxAge),
			LastOutcome:        previous.LastOutcome,
			ConsecutiveFailure: previous.ConsecutiveFailure,
			LastFinishedAt:     previous.LastSuccessFinishedAt,
		}, nil
	}

	return Report{
		Healthy:            true,
		LastOutcome:        previous.LastOutcome,
		ConsecutiveFailure: previous.ConsecutiveFailure,
		LastFinishedAt:     previous.LastSuccessFinishedAt,
	}, nil
}

// Report is the result of a freshness/outcome check.
type Report struct {
	Healthy            bool      `json:"healthy"`
	Reason             string    `json:"reason"`
	LastOutcome        string    `json:"last_outcome,omitempty"`
	ConsecutiveFailure int       `json:"consecutive_failure"`
	LastFinishedAt     time.Time `json:"last_finished_at,omitempty"`
}

func (s *Store) write(snap Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create run-health directory: %w", err)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run-health: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.Path, data, 0o644); err != nil {
		return fmt.Errorf("write run-health: %w", err)
	}
	return nil
}

func classify(result models.RunResult, minExpected int) string {
	if result.FatalError != nil {
		return "failure"
	}
	if result.TotalFetched == 0 || (minExpected > 0 && result.TotalFetched < minExpected) {
		return "empty"
	}
	// Every item reached the LLM and came back empty: the pipeline moved
	// bytes but produced nothing. That is an outage, not a success.
	if result.TotalLLMSkipped > 0 && result.TotalLLMSkipped >= result.TotalFetched {
		return "failure"
	}
	return "success"
}

func fatalMessage(result models.RunResult) string {
	if result.FatalError == nil {
		return ""
	}
	return result.FatalError.Error()
}

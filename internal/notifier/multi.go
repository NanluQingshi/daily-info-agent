package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/user/daily-info-agent/pkg/models"
)

// Multi fans a notification out to every configured channel. A failure in one
// channel never blocks the others; SendDailySummary returns an error only when
// every channel failed (joined), so the scheduler log still surfaces total
// outage while partial delivery stays non-fatal.
type Multi struct {
	senders []Sender
	logger  *slog.Logger
}

// NewMulti wraps the given senders. Senders are called in order. A nil sender
// entry is skipped (callers often build the slice from optional configs).
func NewMulti(logger *slog.Logger, senders ...Sender) *Multi {
	if logger == nil {
		logger = slog.Default()
	}
	return &Multi{senders: senders, logger: logger}
}

// Len reports the number of wrapped channels.
func (m *Multi) Len() int { return len(m.senders) }

// SendDailySummary delivers the digest to all channels.
func (m *Multi) SendDailySummary(ctx context.Context, articles []models.ProcessedArticle, result models.RunResult) error {
	var errs []error
	succeeded := 0
	for _, s := range m.senders {
		if s == nil {
			continue
		}
		if err := s.SendDailySummary(ctx, articles, result); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			m.logger.Error("digest delivery failed",
				slog.String("channel", s.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}
		succeeded++
	}
	if len(errs) > 0 && succeeded == 0 {
		return fmt.Errorf("all notification channels failed: %w", errors.Join(errs...))
	}
	return nil
}

// SendAlert delivers an alert to all channels. Same error semantics as
// SendDailySummary: only total failure is an error.
func (m *Multi) SendAlert(ctx context.Context, message string) error {
	var errs []error
	succeeded := 0
	for _, s := range m.senders {
		if s == nil {
			continue
		}
		if err := s.SendAlert(ctx, message); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			m.logger.Error("alert delivery failed",
				slog.String("channel", s.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}
		succeeded++
	}
	if len(errs) > 0 && succeeded == 0 {
		return fmt.Errorf("all notification channels failed: %w", errors.Join(errs...))
	}
	return nil
}

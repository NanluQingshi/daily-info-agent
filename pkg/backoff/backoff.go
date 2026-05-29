// Package backoff provides a generic exponential-backoff retry helper.
package backoff

import (
	"context"
	"errors"
	"time"
)

// RetryableError wraps an error to signal that the operation should be retried.
type RetryableError struct {
	Cause error
}

func (e *RetryableError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "retryable error"
}

func (e *RetryableError) Unwrap() error { return e.Cause }

// IsRetryable reports whether err (or any error in its chain) is a RetryableError.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}

// Retry calls fn up to maxAttempts times.
//
// Between attempts it waits baseDelay * 2^(attempt-1) (exponential backoff).
// It only retries when fn returns a *RetryableError (checked via errors.As).
// On success (nil) or a non-retryable error, it returns immediately.
// If ctx is cancelled between retries, Retry returns ctx.Err().
func Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Wait before retrying: baseDelay * 2^(attempt-1)
			delay := baseDelay
			for i := 1; i < attempt; i++ {
				delay *= 2
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !IsRetryable(lastErr) {
			// Unwrap the cause so callers see the real error.
			var re *RetryableError
			if errors.As(lastErr, &re) {
				return re.Cause
			}
			return lastErr
		}
	}

	// Exhausted all attempts — unwrap to the underlying cause.
	var re *RetryableError
	if errors.As(lastErr, &re) && re.Cause != nil {
		return re.Cause
	}
	return lastErr
}

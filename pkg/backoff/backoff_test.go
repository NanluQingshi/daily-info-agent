package backoff

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryableError(t *testing.T) {
	cause := errors.New("underlying error")
	re := &RetryableError{Cause: cause}

	if re.Error() != "underlying error" {
		t.Errorf("expected 'underlying error', got %q", re.Error())
	}

	if !errors.Is(re, cause) {
		t.Error("RetryableError should wrap its cause")
	}

	nilCause := &RetryableError{Cause: nil}
	if nilCause.Error() != "retryable error" {
		t.Errorf("expected 'retryable error', got %q", nilCause.Error())
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(&RetryableError{Cause: errors.New("x")}) != true {
		t.Error("RetryableError should be detected as retryable")
	}
	if IsRetryable(errors.New("plain error")) != false {
		t.Error("plain error should not be retryable")
	}
	if IsRetryable(nil) != false {
		t.Error("nil should not be retryable")
	}
}

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), 3, 10*time.Millisecond, func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestRetry_SuccessAfterRetries(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), 3, 10*time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return &RetryableError{Cause: errors.New("not yet")}
		}
		return nil
	})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_ExhaustedRetries(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		attempts++
		return &RetryableError{Cause: errors.New("always fail")}
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "always fail" {
		t.Errorf("expected 'always fail', got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	attempts := 0
	permErr := errors.New("permanent error")
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		attempts++
		return permErr
	})
	if !errors.Is(err, permErr) {
		t.Errorf("expected permanent error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (non-retryable), got %d", attempts)
	}
}

func TestRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Retry(ctx, 3, time.Second, func() error {
		return &RetryableError{Cause: errors.New("fail")}
	})
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRetry_ZeroMaxAttemptsReturnsNil(t *testing.T) {
	err := Retry(context.Background(), 0, time.Millisecond, func() error {
		return &RetryableError{Cause: errors.New("fail")}
	})
	// 0 attempts means the loop never executes, fn is never called, so err is nil
	if err != nil {
		t.Errorf("expected nil with 0 attempts, got %v", err)
	}
}

func TestRetry_RetryableErrorWithNilCause(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), 2, time.Millisecond, func() error {
		attempts++
		return &RetryableError{Cause: nil}
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "retryable error" {
		t.Errorf("expected 'retryable error', got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetry_BackoffDelayIncreases(t *testing.T) {
	start := time.Now()
	attempts := 0
	_ = Retry(context.Background(), 4, 50*time.Millisecond, func() error {
		attempts++
		return &RetryableError{Cause: errors.New("retry")}
	})
	elapsed := time.Since(start)
	if attempts != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts)
	}
	// minimum expected delay: 0 + 50ms + 100ms + 200ms = 350ms
	if elapsed < 300*time.Millisecond {
		t.Errorf("backoff too fast: elapsed=%v", elapsed)
	}
}

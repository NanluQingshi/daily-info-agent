package backoff

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// RetryableError
// ---------------------------------------------------------------------------

func TestRetryableError_WithCause(t *testing.T) {
	cause := errors.New("underlying error")
	e := &RetryableError{Cause: cause}
	assert.Equal(t, "underlying error", e.Error())
	assert.Equal(t, cause, e.Unwrap())
}

func TestRetryableError_WithoutCause(t *testing.T) {
	e := &RetryableError{}
	assert.Equal(t, "retryable error", e.Error())
}

// ---------------------------------------------------------------------------
// IsRetryable
// ---------------------------------------------------------------------------

func TestIsRetryable_True(t *testing.T) {
	err := &RetryableError{Cause: errors.New("oops")}
	assert.True(t, IsRetryable(err))
}

func TestIsRetryable_False_PlainError(t *testing.T) {
	assert.False(t, IsRetryable(errors.New("plain error")))
}

func TestIsRetryable_False_Nil(t *testing.T) {
	assert.False(t, IsRetryable(nil))
}

func TestIsRetryable_False_WrappedNonRetryable(t *testing.T) {
	inner := errors.New("inner")
	wrapped := fmt.Errorf("wrapped: %w", inner)
	assert.False(t, IsRetryable(wrapped))
}

func TestIsRetryable_True_Wrapped(t *testing.T) {
	re := &RetryableError{Cause: errors.New("cause")}
	wrapped := fmt.Errorf("wrapped: %w", re)
	assert.True(t, IsRetryable(wrapped))
}

// ---------------------------------------------------------------------------
// Retry — success paths
// ---------------------------------------------------------------------------

func TestRetry_SucceedsOnFirstAttempt(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls.Add(1)
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRetry_SucceedsOnSecondAttempt(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls.Add(1)
		if calls.Load() < 2 {
			return &RetryableError{Cause: errors.New("not yet")}
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestRetry_SucceedsOnLastAttempt(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls.Add(1)
		if calls.Load() < 3 {
			return &RetryableError{Cause: errors.New("not yet")}
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load())
}

// ---------------------------------------------------------------------------
// Retry — failure paths
// ---------------------------------------------------------------------------

func TestRetry_MaxAttemptsExhausted(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls.Add(1)
		return &RetryableError{Cause: errors.New("persistent error")}
	})
	require.Error(t, err)
	assert.Equal(t, "persistent error", err.Error())
	assert.Equal(t, int32(3), calls.Load())
}

func TestRetry_NonRetryableError_StopsImmediately(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 5, time.Millisecond, func() error {
		calls.Add(1)
		return errors.New("fatal error")
	})
	require.Error(t, err)
	assert.Equal(t, "fatal error", err.Error())
	assert.Equal(t, int32(1), calls.Load())
}

func TestRetry_RetryableWithNilCause_Exhausted(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 2, time.Millisecond, func() error {
		calls.Add(1)
		return &RetryableError{}
	})
	require.Error(t, err)
	// When the RetryableError has a nil cause, Retry returns the RetryableError's Error().
	assert.Equal(t, "retryable error", err.Error())
	assert.Equal(t, int32(2), calls.Load())
}

// ---------------------------------------------------------------------------
// Retry — context cancellation
// ---------------------------------------------------------------------------

func TestRetry_ContextCancelled_BetweenRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32

	// Cancel the context after the first attempt completes.
	err := Retry(ctx, 5, 50*time.Millisecond, func() error {
		calls.Add(1)
		if calls.Load() == 1 {
			// Cancel after a short delay so the first attempt returns, then
			// the retry loop hits ctx.Done() during the wait.
			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()
		}
		return &RetryableError{Cause: errors.New("retry me")}
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRetry_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Retry(ctx, 3, time.Millisecond, func() error {
		return &RetryableError{Cause: errors.New("should not retry")}
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Retry — zero attempts edge case
// ---------------------------------------------------------------------------

func TestRetry_ZeroAttempts_DoesNotCallFn(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 0, time.Millisecond, func() error {
		calls.Add(1)
		return nil
	})
	// With 0 attempts, the loop never executes; lastErr is nil → returns nil.
	assert.NoError(t, err)
	assert.Equal(t, int32(0), calls.Load())
}

func TestRetry_SingleAttempt(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 1, time.Millisecond, func() error {
		calls.Add(1)
		return &RetryableError{Cause: errors.New("fail once")}
	})
	require.Error(t, err)
	assert.Equal(t, "fail once", err.Error())
	assert.Equal(t, int32(1), calls.Load())
}

// ---------------------------------------------------------------------------
// Retry — exponential backoff delay verification
// ---------------------------------------------------------------------------

func TestRetry_DelaysAreExponential(t *testing.T) {
	// Use a very short base delay and verify that each retry takes at least
	// the expected minimum time. baseDelay=5ms → delays: wait1=5ms, wait2=10ms
	const base = 10 * time.Millisecond
	var calls atomic.Int32
	start := time.Now()

	_ = Retry(context.Background(), 4, base, func() error {
		calls.Add(1)
		return &RetryableError{Cause: errors.New("keep failing")}
	})

	elapsed := time.Since(start)
	assert.Equal(t, int32(4), calls.Load())
	// Minimum expected: wait0=0, wait1=10ms, wait2=20ms, wait3=40ms = 70ms total
	// Add a generous fudge factor for CI slowness.
	assert.GreaterOrEqual(t, elapsed, 65*time.Millisecond,
		"total backoff should be at least ~70ms for 4 attempts with 10ms base")
}

// ---------------------------------------------------------------------------
// Retry — fn returns nil RetryableError (weird but handled)
// ---------------------------------------------------------------------------

func TestRetry_FnReturnsRetryableErrorWrappingNil(t *testing.T) {
	var calls atomic.Int32
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls.Add(1)
		return &RetryableError{Cause: nil}
	})
	// Nil cause means it's still a retryable error, so it will be retried.
	// After exhausting attempts, it returns the RetryableError's Error().
	require.Error(t, err)
	assert.Equal(t, "retryable error", err.Error())
	assert.Equal(t, int32(3), calls.Load())
}

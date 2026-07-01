package chat

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToCapacityThenBlocks(t *testing.T) {
	// 3 tokens, refilling 1 per 100ms.
	rl := newRateLimiter(3, 100*time.Millisecond)

	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Fatal("4th request should be rate-limited")
	}
}

func TestRateLimiter_IsolatesByIP(t *testing.T) {
	rl := newRateLimiter(1, 100*time.Millisecond)
	if !rl.Allow("a") {
		t.Fatal("first request for a should pass")
	}
	if rl.Allow("a") {
		t.Fatal("second request for a should be limited")
	}
	// Different key has its own bucket.
	if !rl.Allow("b") {
		t.Fatal("first request for b should pass")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	rl := newRateLimiter(1, 10*time.Millisecond)
	if !rl.Allow("k") {
		t.Fatal("first request should pass")
	}
	// Wait long enough for one token to refill.
	time.Sleep(30 * time.Millisecond)
	if !rl.Allow("k") {
		t.Fatal("request after refill window should pass")
	}
}

package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLimiter_AllowsUpToCapacityThenBlocks(t *testing.T) {
	rl := New(3, 100*time.Millisecond)
	for i := 0; i < 3; i++ {
		assert.True(t, rl.Allow("1.2.3.4"), "request %d should be allowed", i+1)
	}
	assert.False(t, rl.Allow("1.2.3.4"), "4th request should be limited")
}

func TestLimiter_IsolatesByKey(t *testing.T) {
	rl := New(1, 100*time.Millisecond)
	assert.True(t, rl.Allow("a"))
	assert.False(t, rl.Allow("a"))
	assert.True(t, rl.Allow("b"), "different key has its own bucket")
}

func TestLimiter_RefillsOverTime(t *testing.T) {
	rl := New(1, 10*time.Millisecond)
	assert.True(t, rl.Allow("k"))
	assert.False(t, rl.Allow("k"))
	time.Sleep(30 * time.Millisecond)
	assert.True(t, rl.Allow("k"), "request after refill window should pass")
}

func TestLimiter_CapacityOne_ImmediateSecondDenied(t *testing.T) {
	rl := New(1, time.Second)
	assert.True(t, rl.Allow("x"))
	assert.False(t, rl.Allow("x"))
}

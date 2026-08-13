// Package ratelimit provides a per-key token bucket limiter suitable for
// single-process deployments. Keys are typically client IPs.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a per-key token bucket with bounded memory and opportunistic GC.
type Limiter struct {
	mu sync.Mutex

	capacity  int           // max tokens in the bucket
	refill    time.Duration // time per token refilled
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// New builds a limiter that allows up to capacity requests per
// refill*capacity window, refilling continuously at 1 token / refill.
func New(capacity int, refill time.Duration) *Limiter {
	return &Limiter{
		capacity:  capacity,
		refill:    refill,
		buckets:   make(map[string]*bucket),
		lastSweep: time.Now(),
	}
}

// Allow reports whether key may proceed. It mutates the bucket, consuming one
// token when allowed.
func (r *Limiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Opportunistic GC: drop buckets idle for > 10 minutes every 10 minutes.
	now := time.Now()
	if now.Sub(r.lastSweep) > 10*time.Minute {
		cutoff := now.Add(-10 * time.Minute)
		for k, b := range r.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(r.buckets, k)
			}
		}
		r.lastSweep = now
	}

	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(r.capacity), lastSeen: now}
		r.buckets[key] = b
	}

	// Refill proportional to elapsed time.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * (1 / r.refill.Seconds())
	if b.tokens > float64(r.capacity) {
		b.tokens = float64(r.capacity)
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

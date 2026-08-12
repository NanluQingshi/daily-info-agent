package chat

import (
	"time"

	"github.com/user/daily-info-agent/pkg/ratelimit"
)

// rateLimiter is a thin wrapper over pkg/ratelimit.Limiter, retained as an
// alias so the chat handler's field type stays stable.
type rateLimiter = ratelimit.Limiter

// newRateLimiter builds a limiter that allows up to capacity requests per
// refill*capacity window, refilling continuously at 1 token / refill.
func newRateLimiter(capacity int, refill time.Duration) *rateLimiter {
	return ratelimit.New(capacity, refill)
}

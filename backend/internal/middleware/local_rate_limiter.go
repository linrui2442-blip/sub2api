package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// LocalRateLimiter is the single-process fixed-window limiter used by the
// Personal Edition HTTP boundary. Unlike the legacy Redis limiter it needs no
// network listener and is sufficient because a Personal runtime is one local
// executable. It intentionally retains the same Allow/LimitWithOptions API so
// auth and panel limits keep their existing fail-open/fail-close semantics.
type LocalRateLimiter struct {
	mu      sync.Mutex
	entries map[string]localRateLimitEntry
}

type localRateLimitEntry struct {
	count     int64
	expiresAt time.Time
}

func NewLocalRateLimiter() *LocalRateLimiter {
	return &LocalRateLimiter{entries: make(map[string]localRateLimitEntry)}
}

func (r *LocalRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (AllowResult, error) {
	if err := ctx.Err(); err != nil {
		return AllowResult{}, err
	}
	if r == nil || limit <= 0 {
		return AllowResult{Allowed: true}, nil
	}
	if window <= 0 {
		window = time.Millisecond
	}
	now := time.Now()
	r.mu.Lock()
	entry := r.entries[key]
	if entry.expiresAt.IsZero() || !now.Before(entry.expiresAt) {
		entry = localRateLimitEntry{expiresAt: now.Add(window)}
	}
	entry.count++
	r.entries[key] = entry
	r.mu.Unlock()

	result := AllowResult{Allowed: entry.count <= int64(limit), Count: entry.count}
	if !result.Allowed {
		result.RetryAfter = time.Until(entry.expiresAt)
		if result.RetryAfter < 0 {
			result.RetryAfter = 0
		}
	}
	return result, nil
}

func (r *LocalRateLimiter) Limit(key string, limit int, window time.Duration) gin.HandlerFunc {
	return r.LimitWithOptions(key, limit, window, RateLimitOptions{})
}

func (r *LocalRateLimiter) LimitWithOptions(key string, limit int, window time.Duration, opts RateLimitOptions) gin.HandlerFunc {
	failureMode := opts.FailureMode
	if failureMode != RateLimitFailClose {
		failureMode = RateLimitFailOpen
	}
	return func(c *gin.Context) {
		result, err := r.Allow(c.Request.Context(), key+":"+clientIPForRateLimit(c), limit, window)
		if err != nil {
			if failureMode == RateLimitFailClose {
				abortRateLimit(c, window)
				return
			}
			c.Next()
			return
		}
		if !result.Allowed {
			abortRateLimit(c, result.RetryAfter)
			return
		}
		c.Next()
	}
}

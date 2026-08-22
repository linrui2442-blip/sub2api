package middleware

import (
	"context"
	"testing"
	"time"
)

func TestLocalRateLimiterEnforcesAndResetsWindow(t *testing.T) {
	limiter := NewLocalRateLimiter()
	window := 20 * time.Millisecond
	for i := 0; i < 2; i++ {
		result, err := limiter.Allow(context.Background(), "login:127.0.0.1", 2, window)
		if err != nil || !result.Allowed {
			t.Fatalf("attempt %d = (%+v, %v), want allowed", i+1, result, err)
		}
	}
	blocked, err := limiter.Allow(context.Background(), "login:127.0.0.1", 2, window)
	if err != nil || blocked.Allowed || blocked.RetryAfter <= 0 {
		t.Fatalf("blocked attempt = (%+v, %v), want retryable rejection", blocked, err)
	}
	time.Sleep(30 * time.Millisecond)
	reset, err := limiter.Allow(context.Background(), "login:127.0.0.1", 2, window)
	if err != nil || !reset.Allowed || reset.Count != 1 {
		t.Fatalf("post-window attempt = (%+v, %v), want first allowed attempt", reset, err)
	}
}

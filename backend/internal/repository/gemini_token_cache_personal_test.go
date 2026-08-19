package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestPersonalGeminiTokenCacheTTLAndDelete(t *testing.T) {
	cache := newPersonalGeminiTokenCache()
	ctx := context.Background()

	if _, err := cache.GetAccessToken(ctx, "p1"); !errors.Is(err, redis.Nil) {
		t.Fatalf("missing token must match redis.Nil, got %v", err)
	}
	if err := cache.SetAccessToken(ctx, "p1", "token", 20*time.Millisecond); err != nil {
		t.Fatalf("set token: %v", err)
	}
	if got, err := cache.GetAccessToken(ctx, "p1"); err != nil || got != "token" {
		t.Fatalf("get token = %q, %v", got, err)
	}

	time.Sleep(30 * time.Millisecond)
	if _, err := cache.GetAccessToken(ctx, "p1"); !errors.Is(err, redis.Nil) {
		t.Fatalf("expired token must match redis.Nil, got %v", err)
	}

	if err := cache.SetAccessToken(ctx, "p1", "token2", 0); err != nil {
		t.Fatalf("set non-expiring token: %v", err)
	}
	if err := cache.DeleteAccessToken(ctx, "p1"); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if _, err := cache.GetAccessToken(ctx, "p1"); !errors.Is(err, redis.Nil) {
		t.Fatalf("deleted token must match redis.Nil, got %v", err)
	}
}

func TestPersonalGeminiTokenCacheRefreshLock(t *testing.T) {
	cache := newPersonalGeminiTokenCache()
	ctx := context.Background()

	acquired, err := cache.AcquireRefreshLock(ctx, "p1", 20*time.Millisecond)
	if err != nil || !acquired {
		t.Fatalf("first lock acquisition = %v, %v", acquired, err)
	}
	acquired, err = cache.AcquireRefreshLock(ctx, "p1", 20*time.Millisecond)
	if err != nil || acquired {
		t.Fatalf("second lock acquisition = %v, %v", acquired, err)
	}

	time.Sleep(30 * time.Millisecond)
	acquired, err = cache.AcquireRefreshLock(ctx, "p1", time.Second)
	if err != nil || !acquired {
		t.Fatalf("expired lock acquisition = %v, %v", acquired, err)
	}
	if err := cache.ReleaseRefreshLock(ctx, "p1"); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	acquired, err = cache.AcquireRefreshLock(ctx, "p1", time.Second)
	if err != nil || !acquired {
		t.Fatalf("lock after release = %v, %v", acquired, err)
	}
}

package repository

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type personalTokenEntry struct {
	value     string
	expiresAt time.Time
}

type personalGeminiTokenCache struct {
	mu     sync.Mutex
	tokens map[string]personalTokenEntry
	locks  map[string]time.Time
}

var _ service.GeminiTokenCache = (*personalGeminiTokenCache)(nil)

func newPersonalGeminiTokenCache() service.GeminiTokenCache {
	return &personalGeminiTokenCache{
		tokens: make(map[string]personalTokenEntry),
		locks:  make(map[string]time.Time),
	}
}

func (c *personalGeminiTokenCache) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.tokens[cacheKey]
	if !ok {
		return "", redis.Nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.tokens, cacheKey)
		return "", redis.Nil
	}
	return entry.value, nil
}

func (c *personalGeminiTokenCache) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry := personalTokenEntry{value: token}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}

	c.mu.Lock()
	c.tokens[cacheKey] = entry
	c.mu.Unlock()
	return nil
}

func (c *personalGeminiTokenCache) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.tokens, cacheKey)
	c.mu.Unlock()
	return nil
}

func (c *personalGeminiTokenCache) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if expiresAt, exists := c.locks[cacheKey]; exists {
		if expiresAt.IsZero() || now.Before(expiresAt) {
			return false, nil
		}
		delete(c.locks, cacheKey)
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	c.locks[cacheKey] = expiresAt
	return true, nil
}

func (c *personalGeminiTokenCache) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.locks, cacheKey)
	c.mu.Unlock()
	return nil
}

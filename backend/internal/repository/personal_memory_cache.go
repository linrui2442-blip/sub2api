package repository

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var errPersonalCacheMiss = errors.New("personal local cache miss")

// personalTTLStore is deliberately process-local. Durable state remains in
// SQLite/Ent; these entries only accelerate routing and recover safely after a
// restart by reloading from the account pool or issuing a new session.
type personalTTLStore struct {
	mu      sync.Mutex
	entries map[string]personalTTLValue
}

type personalTTLValue struct {
	value     any
	expiresAt time.Time
}

func newPersonalTTLStore() *personalTTLStore {
	return &personalTTLStore{entries: make(map[string]personalTTLValue)}
}

func (s *personalTTLStore) get(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.IsZero() && !time.Now().Before(entry.expiresAt) {
		delete(s.entries, key)
		return nil, false
	}
	return entry.value, true
}

func (s *personalTTLStore) set(key string, value any, ttl time.Duration) {
	entry := personalTTLValue{value: value}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.entries[key] = entry
	s.mu.Unlock()
}

func (s *personalTTLStore) delete(key string) {
	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()
}

type personalGatewayCache struct{ store *personalTTLStore }

func newPersonalGatewayCache() service.GatewayCache {
	return &personalGatewayCache{store: newPersonalTTLStore()}
}

func (c *personalGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	v, ok := c.store.get(buildSessionKey(groupID, sessionHash))
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	id, ok := v.(int64)
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	return id, nil
}
func (c *personalGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.set(buildSessionKey(groupID, sessionHash), accountID, ttl)
	return nil
}
func (c *personalGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	v, ok := c.store.get(buildSessionKey(groupID, sessionHash))
	if ok {
		c.store.set(buildSessionKey(groupID, sessionHash), v, ttl)
	}
	return nil
}
func (c *personalGatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.delete(buildSessionKey(groupID, sessionHash))
	return nil
}
func (c *personalGatewayCache) SetReasoningContent(ctx context.Context, itemID, content string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if itemID == "" || content == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = reasoningContentDefaultTTL
	}
	c.store.set(reasoningContentPrefix+itemID, content, ttl)
	return nil
}
func (c *personalGatewayCache) GetReasoningContent(ctx context.Context, itemID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, ok := c.store.get(reasoningContentPrefix + itemID)
	if !ok {
		return "", service.ErrReasoningContentNotFound
	}
	text, ok := v.(string)
	if !ok {
		return "", service.ErrReasoningContentNotFound
	}
	return text, nil
}
func (c *personalGatewayCache) SetCyberSessionBlocked(ctx context.Context, key string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.set(cyberSessionBlockPrefix+key, true, ttl)
	return nil
}
func (c *personalGatewayCache) IsCyberSessionBlocked(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, ok := c.store.get(cyberSessionBlockPrefix + key)
	return ok, nil
}

type personalDashboardCache struct{ store *personalTTLStore }

func newPersonalDashboardCache() service.DashboardStatsCache {
	return &personalDashboardCache{store: newPersonalTTLStore()}
}
func (c *personalDashboardCache) GetDashboardStats(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, ok := c.store.get(dashboardStatsCacheKey)
	if !ok {
		return "", service.ErrDashboardStatsCacheMiss
	}
	text, ok := v.(string)
	if !ok {
		return "", service.ErrDashboardStatsCacheMiss
	}
	return text, nil
}
func (c *personalDashboardCache) SetDashboardStats(ctx context.Context, data string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.set(dashboardStatsCacheKey, data, ttl)
	return nil
}
func (c *personalDashboardCache) DeleteDashboardStats(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.delete(dashboardStatsCacheKey)
	return nil
}

type personalUpdateCache struct{ store *personalTTLStore }

func newPersonalUpdateCache() service.UpdateCache {
	return &personalUpdateCache{store: newPersonalTTLStore()}
}
func (c *personalUpdateCache) GetUpdateInfo(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, ok := c.store.get(updateCacheKey)
	if !ok {
		return "", errPersonalCacheMiss
	}
	text, ok := v.(string)
	if !ok {
		return "", errPersonalCacheMiss
	}
	return text, nil
}
func (c *personalUpdateCache) SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.set(updateCacheKey, data, ttl)
	return nil
}

type personalTempUnschedCache struct{ store *personalTTLStore }

func newPersonalTempUnschedCache() service.TempUnschedCache {
	return &personalTempUnschedCache{store: newPersonalTTLStore()}
}
func (c *personalTempUnschedCache) SetTempUnsched(ctx context.Context, accountID int64, state *service.TempUnschedState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state == nil || state.UntilUnix <= time.Now().Unix() {
		return nil
	}
	key := tempUnschedPrefix + fmtInt64(accountID)
	if prior, ok := c.store.get(key); ok {
		if old, ok := prior.(*service.TempUnschedState); ok && old.UntilUnix >= state.UntilUnix {
			return nil
		}
	}
	copy := *state
	c.store.set(key, &copy, time.Until(time.Unix(state.UntilUnix, 0)))
	return nil
}
func (c *personalTempUnschedCache) GetTempUnsched(ctx context.Context, accountID int64) (*service.TempUnschedState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, ok := c.store.get(tempUnschedPrefix + fmtInt64(accountID))
	if !ok {
		return nil, nil
	}
	state, ok := v.(*service.TempUnschedState)
	if !ok {
		return nil, nil
	}
	copy := *state
	return &copy, nil
}
func (c *personalTempUnschedCache) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.delete(tempUnschedPrefix + fmtInt64(accountID))
	return nil
}

func fmtInt64(id int64) string { return strconv.FormatInt(id, 10) }

// personalAccountCounterCache keeps short-lived account penalties local to one
// Personal process. All operations are mutex-protected so failure thresholds
// remain atomic under concurrent gateway requests.
type personalAccountCounterCache struct {
	mu      sync.Mutex
	entries map[string]personalCounterEntry
}

type personalCounterEntry struct {
	count     int64
	expiresAt time.Time
}

func newPersonalAccountCounterCache() *personalAccountCounterCache {
	return &personalAccountCounterCache{entries: make(map[string]personalCounterEntry)}
}

func (c *personalAccountCounterCache) increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	now := time.Now()
	c.mu.Lock()
	entry := c.entries[key]
	if entry.expiresAt.IsZero() || !now.Before(entry.expiresAt) {
		entry = personalCounterEntry{expiresAt: now.Add(ttl)}
	}
	entry.count++
	c.entries[key] = entry
	c.mu.Unlock()
	return entry.count, nil
}

func (c *personalAccountCounterCache) get(ctx context.Context, key string) (int64, time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	now := time.Now()
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok && !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
		delete(c.entries, key)
		ok = false
	}
	c.mu.Unlock()
	if !ok {
		return 0, 0, nil
	}
	return entry.count, time.Until(entry.expiresAt), nil
}

func (c *personalAccountCounterCache) reset(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
	return nil
}

func (c *personalAccountCounterCache) IncrementTimeoutCount(ctx context.Context, accountID int64, windowMinutes int) (int64, error) {
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	return c.increment(ctx, timeoutCounterPrefix+fmtInt64(accountID), time.Duration(windowMinutes)*time.Minute)
}
func (c *personalAccountCounterCache) GetTimeoutCount(ctx context.Context, accountID int64) (int64, error) {
	count, _, err := c.get(ctx, timeoutCounterPrefix+fmtInt64(accountID))
	return count, err
}
func (c *personalAccountCounterCache) ResetTimeoutCount(ctx context.Context, accountID int64) error {
	return c.reset(ctx, timeoutCounterPrefix+fmtInt64(accountID))
}
func (c *personalAccountCounterCache) GetTimeoutCountTTL(ctx context.Context, accountID int64) (time.Duration, error) {
	_, ttl, err := c.get(ctx, timeoutCounterPrefix+fmtInt64(accountID))
	return ttl, err
}
func (c *personalAccountCounterCache) IncrementOpenAI403Count(ctx context.Context, accountID int64, windowMinutes int) (int64, error) {
	if windowMinutes < 1 {
		windowMinutes = 1
	}
	return c.increment(ctx, openAI403CounterPrefix+fmtInt64(accountID), time.Duration(windowMinutes)*time.Minute)
}
func (c *personalAccountCounterCache) ResetOpenAI403Count(ctx context.Context, accountID int64) error {
	return c.reset(ctx, openAI403CounterPrefix+fmtInt64(accountID))
}
func (c *personalAccountCounterCache) IncrementInternal500Count(ctx context.Context, accountID int64) (int64, error) {
	return c.increment(ctx, internal500CounterPrefix+fmtInt64(accountID), time.Duration(internal500CounterTTLSeconds)*time.Second)
}
func (c *personalAccountCounterCache) ResetInternal500Count(ctx context.Context, accountID int64) error {
	return c.reset(ctx, internal500CounterPrefix+fmtInt64(accountID))
}

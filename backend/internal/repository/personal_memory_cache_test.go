package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPersonalGatewayCacheExpiresAndReportsDomainMiss(t *testing.T) {
	cache := newPersonalGatewayCache()
	ctx := context.Background()
	if _, err := cache.GetSessionAccountID(ctx, 1, "session"); !errors.Is(err, service.ErrStickySessionNotFound) {
		t.Fatalf("empty cache error = %v", err)
	}
	if err := cache.SetSessionAccountID(ctx, 1, "session", 42, 15*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.GetSessionAccountID(ctx, 1, "session"); err != nil || got != 42 {
		t.Fatalf("GetSessionAccountID = (%d, %v)", got, err)
	}
	time.Sleep(25 * time.Millisecond)
	if _, err := cache.GetSessionAccountID(ctx, 1, "session"); !errors.Is(err, service.ErrStickySessionNotFound) {
		t.Fatalf("expired cache error = %v", err)
	}
}

func TestPersonalTempUnschedOnlyExtendsBlock(t *testing.T) {
	cache := newPersonalTempUnschedCache()
	ctx := context.Background()
	later := &service.TempUnschedState{UntilUnix: time.Now().Add(time.Minute).Unix(), ErrorMessage: "later"}
	earlier := &service.TempUnschedState{UntilUnix: time.Now().Add(30 * time.Second).Unix(), ErrorMessage: "earlier"}
	if err := cache.SetTempUnsched(ctx, 7, later); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetTempUnsched(ctx, 7, earlier); err != nil {
		t.Fatal(err)
	}
	got, err := cache.GetTempUnsched(ctx, 7)
	if err != nil || got == nil || got.ErrorMessage != "later" {
		t.Fatalf("state = (%+v, %v)", got, err)
	}
}

func TestPersonalAccountCounterKeepsPenaltyWindowsIndependent(t *testing.T) {
	cache := newPersonalAccountCounterCache()
	ctx := context.Background()
	if got, err := cache.IncrementTimeoutCount(ctx, 7, 1); err != nil || got != 1 {
		t.Fatalf("first timeout = (%d, %v)", got, err)
	}
	if got, err := cache.IncrementTimeoutCount(ctx, 7, 1); err != nil || got != 2 {
		t.Fatalf("second timeout = (%d, %v)", got, err)
	}
	if ttl, err := cache.GetTimeoutCountTTL(ctx, 7); err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("timeout ttl = (%s, %v)", ttl, err)
	}
	if got, err := cache.IncrementOpenAI403Count(ctx, 7, 1); err != nil || got != 1 {
		t.Fatalf("403 count = (%d, %v)", got, err)
	}
	if err := cache.ResetTimeoutCount(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.GetTimeoutCount(ctx, 7); err != nil || got != 0 {
		t.Fatalf("reset timeout = (%d, %v)", got, err)
	}
}

func TestPersonalCacheConstructorsDoNotRequireRedisClient(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	if _, ok := NewGatewayCache(nil).(*personalGatewayCache); !ok {
		t.Fatal("gateway cache did not select local implementation")
	}
	if _, ok := NewTempUnschedCache(nil).(*personalTempUnschedCache); !ok {
		t.Fatal("temporary scheduling cache did not select local implementation")
	}
	if _, ok := NewTimeoutCounterCache(nil).(*personalAccountCounterCache); !ok {
		t.Fatal("timeout counter did not select local implementation")
	}
	if _, ok := NewOpenAI403CounterCache(nil).(*personalAccountCounterCache); !ok {
		t.Fatal("403 counter did not select local implementation")
	}
	if _, ok := NewInternal500CounterCache(nil).(*personalAccountCounterCache); !ok {
		t.Fatal("500 counter did not select local implementation")
	}
}

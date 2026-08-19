package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

func TestPersonalEmbeddedRedisWallClockTTL(t *testing.T) {
	runtime, err := newPersonalEmbeddedRedis()
	if err != nil {
		t.Fatalf("start embedded redis: %v", err)
	}
	defer runtime.Close()

	client := runtime.Client()
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	if err := pingPersonalEmbeddedRedis(ctx, client); err != nil {
		t.Fatalf("ping embedded redis: %v", err)
	}
	if err := client.Set(ctx, "personal:ttl", "ok", 150*time.Millisecond).Err(); err != nil {
		t.Fatalf("set ttl key: %v", err)
	}

	time.Sleep(350 * time.Millisecond)
	if err := client.Get(ctx, "personal:ttl").Err(); !errors.Is(err, redis.Nil) {
		t.Fatalf("wall-clock TTL must expire, got %v", err)
	}
}

func TestPersonalEmbeddedRedisRunsSchedulerLuaContracts(t *testing.T) {
	runtime, err := newPersonalEmbeddedRedis()
	if err != nil {
		t.Fatalf("start embedded redis: %v", err)
	}
	defer runtime.Close()

	client := runtime.Client()
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	cache, ok := newSchedulerCacheWithChunkSizes(client, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize).(*schedulerCache)
	if !ok {
		t.Fatal("expected scheduler cache implementation")
	}

	bucket := service.SchedulerBucket{GroupID: 1, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	if err != nil {
		t.Fatalf("capture scheduler token: %v", err)
	}
	account := service.Account{ID: 101, Name: "personal-openai", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	if err := cache.SetSnapshot(ctx, bucket, token, []service.Account{account}); err != nil {
		t.Fatalf("set scheduler snapshot: %v", err)
	}

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	if err != nil {
		t.Fatalf("get scheduler snapshot: %v", err)
	}
	if !hit || len(snapshot) != 1 || snapshot[0].ID != account.ID {
		t.Fatalf("unexpected scheduler snapshot: hit=%v snapshot=%+v", hit, snapshot)
	}

	if err := cache.RetireBucket(ctx, bucket); err != nil {
		t.Fatalf("retire scheduler bucket: %v", err)
	}
	if _, err := cache.ReopenBucket(ctx, bucket); err != nil {
		t.Fatalf("reopen scheduler bucket: %v", err)
	}
}

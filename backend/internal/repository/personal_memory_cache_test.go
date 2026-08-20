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

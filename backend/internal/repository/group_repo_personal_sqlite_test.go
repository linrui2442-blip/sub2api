package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestGroupAccountCountsUseSQLiteNativeTimePredicates(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")

	drv, db, err := openPersonalSQLite(filepath.Join(t.TempDir(), "group-counts.db"))
	if err != nil {
		t.Fatalf("open Personal SQLite: %v", err)
	}
	client := ent.NewClient(ent.Driver(drv))
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	group, err := client.Group.Create().
		SetName("antigravity-default").
		SetPlatform(service.PlatformAntigravity).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	ready, err := client.Account.Create().
		SetName("ready").
		SetPlatform(service.PlatformAntigravity).
		SetType(service.AccountTypeOAuth).
		Save(ctx)
	if err != nil {
		t.Fatalf("create ready account: %v", err)
	}
	cooldown, err := client.Account.Create().
		SetName("cooldown").
		SetPlatform(service.PlatformAntigravity).
		SetType(service.AccountTypeOAuth).
		SetRateLimitResetAt(time.Now().Add(time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create cooldown account: %v", err)
	}
	if _, err := client.AccountGroup.Create().SetAccountID(ready.ID).SetGroupID(group.ID).Save(ctx); err != nil {
		t.Fatalf("bind ready account: %v", err)
	}
	if _, err := client.AccountGroup.Create().SetAccountID(cooldown.ID).SetGroupID(group.ID).Save(ctx); err != nil {
		t.Fatalf("bind cooldown account: %v", err)
	}

	repo := newGroupRepositoryWithSQL(client, db)
	groups, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "", nil)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
	if groups[0].AccountCount != 2 || groups[0].ActiveAccountCount != 1 || groups[0].RateLimitedAccountCount != 1 {
		t.Fatalf("unexpected counts: total=%d active=%d limited=%d", groups[0].AccountCount, groups[0].ActiveAccountCount, groups[0].RateLimitedAccountCount)
	}
}

package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	entgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPersonalRestartPreservesOAuthAccountAndRefreshEligibility(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	dbPath := filepath.Join(t.TempDir(), "personal-restart.db")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// First process lifetime: create and refresh a GPT OAuth account through the
	// same admin repository used by the Personal account-management UI.
	drv1, db1, err := openPersonalSQLite(dbPath)
	if err != nil {
		t.Fatalf("open first Personal SQLite: %v", err)
	}
	client1 := ent.NewClient(ent.Driver(drv1))
	if err := client1.Schema.Create(ctx); err != nil {
		t.Fatalf("create first Personal schema: %v", err)
	}
	if err := ensurePersonalSQLiteInfrastructure(ctx, db1); err != nil {
		t.Fatalf("create first Personal infrastructure: %v", err)
	}
	if err := ensureSimpleModeDefaultGroups(ctx, client1); err != nil {
		t.Fatalf("seed first Personal groups: %v", err)
	}

	redis1, err := newPersonalEmbeddedRedis()
	if err != nil {
		t.Fatalf("start first embedded Redis: %v", err)
	}
	rdb1 := redis1.Client()
	scheduler1 := newSchedulerCacheWithChunkSizes(rdb1, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize)
	adminRepo1 := NewPersonalAwareAdminAccountRepository(client1, db1, scheduler1)

	openAIGroup, err := client1.Group.Query().Where(entgroup.NameEQ(service.PlatformOpenAI + "-default")).Only(ctx)
	if err != nil {
		t.Fatalf("load OpenAI default group: %v", err)
	}
	account := &service.Account{
		Name:        "restart-openai",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "access-before", "refresh_token": "refresh-before"},
		Extra:       map[string]any{},
		Concurrency: 2,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
	}
	if err := adminRepo1.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: openAIGroup.ID, Priority: 1}}); err != nil {
		t.Fatalf("create restart account through Personal admin repository: %v", err)
	}

	updater, ok := adminRepo1.(interface {
		UpdateCredentials(context.Context, int64, map[string]any) error
	})
	if !ok {
		t.Fatalf("Personal admin repository must expose SQLite-safe credential persistence, got %T", adminRepo1)
	}
	if err := updater.UpdateCredentials(ctx, account.ID, map[string]any{
		"access_token":  "access-after-refresh",
		"refresh_token": "refresh-after-refresh",
		"expires_at":    time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("persist refreshed OAuth credentials: %v", err)
	}

	// Simulate a full process shutdown. Redis is intentionally discarded; only
	// durable SQLite state is allowed to survive.
	if err := rdb1.Close(); err != nil {
		t.Fatalf("close first Redis client: %v", err)
	}
	redis1.Close()
	if err := client1.Close(); err != nil {
		t.Fatalf("close first Ent client: %v", err)
	}

	// Second process lifetime: reopen the same SQLite file with a fresh embedded
	// Redis instance and prove the scheduler/refresh inputs can be reconstructed.
	drv2, db2, err := openPersonalSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen Personal SQLite: %v", err)
	}
	client2 := ent.NewClient(ent.Driver(drv2))
	defer func() { _ = client2.Close() }()
	if err := ensurePersonalSQLiteInfrastructure(ctx, db2); err != nil {
		t.Fatalf("reopen Personal infrastructure: %v", err)
	}

	redis2, err := newPersonalEmbeddedRedis()
	if err != nil {
		t.Fatalf("start second embedded Redis: %v", err)
	}
	defer redis2.Close()
	rdb2 := redis2.Client()
	defer func() { _ = rdb2.Close() }()
	scheduler2 := newSchedulerCacheWithChunkSizes(rdb2, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize)
	repo2 := NewPersonalAwareAccountRepository(client2, db2, scheduler2)

	loaded, err := repo2.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("reload OAuth account after restart: %v", err)
	}
	if loaded.GetCredential("access_token") != "access-after-refresh" || loaded.GetCredential("refresh_token") != "refresh-after-refresh" {
		t.Fatalf("refreshed OAuth credentials were not durable: %+v", loaded.Credentials)
	}
	if len(loaded.GroupIDs) != 1 || loaded.GroupIDs[0] != openAIGroup.ID {
		t.Fatalf("group binding was not durable across restart: %+v", loaded.GroupIDs)
	}

	schedulable, err := repo2.ListSchedulable(ctx)
	if err != nil {
		t.Fatalf("rebuild schedulable account view after restart: %v", err)
	}
	if len(schedulable) != 1 || schedulable[0].ID != account.ID {
		t.Fatalf("unexpected schedulable accounts after restart: %+v", schedulable)
	}

	pager, ok := repo2.(service.OAuthRefreshCandidatePager)
	if !ok {
		t.Fatalf("Personal repository must expose OAuth refresh pager after restart, got %T", repo2)
	}
	page, err := pager.ListOAuthRefreshCandidatePage(ctx, service.OAuthRefreshPageOptions{
		Platforms:            []string{service.PlatformOpenAI, service.PlatformGemini},
		Limit:                10,
		ActiveOnly:           true,
		IncludeSetupToken:    true,
		RequireRefreshToken:  true,
		ExcludeRetryCooldown: true,
	})
	if err != nil {
		t.Fatalf("list OAuth refresh candidates after restart: %v", err)
	}
	if len(page.Accounts) != 1 || page.Accounts[0].ID != account.ID {
		t.Fatalf("OAuth refresh eligibility was not reconstructed after restart: %+v", page.Accounts)
	}
}

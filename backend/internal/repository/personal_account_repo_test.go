package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPersonalAccountRepositorySchedulerAndOAuthCandidates(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")

	drv, db, err := openPersonalSQLite(filepath.Join(t.TempDir(), "personal-account-adapter.db"))
	if err != nil {
		t.Fatalf("open Personal SQLite: %v", err)
	}
	client := ent.NewClient(ent.Driver(drv))
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("create Ent schema: %v", err)
	}
	if err := ensurePersonalSQLiteInfrastructure(ctx, db); err != nil {
		t.Fatalf("create Personal infrastructure: %v", err)
	}

	runtime, err := newPersonalEmbeddedRedis()
	if err != nil {
		t.Fatalf("start embedded Redis: %v", err)
	}
	defer runtime.Close()
	rdb := runtime.Client()
	defer func() { _ = rdb.Close() }()
	scheduler := newSchedulerCacheWithChunkSizes(rdb, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize)

	repo := NewPersonalAwareAccountRepository(client, db, scheduler)
	personalRepo, ok := repo.(*personalAccountRepository)
	if !ok {
		t.Fatalf("Personal mode must select SQLite-safe account adapter, got %T", repo)
	}

	ready := &service.Account{
		Name:        "openai-ready",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "access-1", "refresh_token": "refresh-1"},
		Extra:       map[string]any{},
		Concurrency: 2,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
	}
	if err := repo.Create(ctx, ready); err != nil {
		t.Fatalf("create ready OpenAI account: %v", err)
	}

	cooldown := &service.Account{
		Name:        "gemini-refresh-cooldown",
		Platform:    service.PlatformGemini,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "access-2", "refresh_token": "refresh-2"},
		Extra:       map[string]any{},
		Concurrency: 1,
		Priority:    2,
		Status:      service.StatusActive,
		Schedulable: true,
	}
	if err := repo.Create(ctx, cooldown); err != nil {
		t.Fatalf("create cooldown Gemini account: %v", err)
	}
	future := time.Now().Add(30 * time.Minute)
	if _, err := client.Account.UpdateOneID(cooldown.ID).
		SetTempUnschedulableUntil(future).
		SetTempUnschedulableReason("token refresh retry exhausted: test").
		Save(ctx); err != nil {
		t.Fatalf("mark Gemini refresh cooldown: %v", err)
	}

	schedulable, err := personalRepo.ListSchedulable(ctx)
	if err != nil {
		t.Fatalf("list Personal schedulable accounts: %v", err)
	}
	if len(schedulable) != 1 || schedulable[0].ID != ready.ID {
		t.Fatalf("unexpected Personal schedulable accounts: %+v", schedulable)
	}

	loads, err := personalRepo.ListSchedulableAccountLoads(ctx)
	if err != nil {
		t.Fatalf("list Personal scheduler loads: %v", err)
	}
	if len(loads) != 1 || loads[0].ID != ready.ID || loads[0].MaxConcurrency != ready.Concurrency {
		t.Fatalf("unexpected Personal scheduler loads: %+v", loads)
	}

	page, err := personalRepo.ListOAuthRefreshCandidatePage(ctx, service.OAuthRefreshPageOptions{
		Platforms:            []string{service.PlatformOpenAI, service.PlatformGemini},
		AfterID:              0,
		Limit:                10,
		ActiveOnly:           true,
		IncludeSetupToken:    true,
		RequireRefreshToken:  true,
		ExcludeRetryCooldown: true,
	})
	if err != nil {
		t.Fatalf("list Personal OAuth refresh candidates: %v", err)
	}
	if len(page.Accounts) != 1 || page.Accounts[0].ID != ready.ID {
		t.Fatalf("unexpected Personal OAuth refresh candidates: %+v", page.Accounts)
	}
	if page.HasMore {
		t.Fatal("single Personal OAuth candidate must not report another page")
	}
}

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

func TestPersonalAccountCredentialRefreshPersistsOnSQLite(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")

	drv, db, err := openPersonalSQLite(filepath.Join(t.TempDir(), "personal-credential-write.db"))
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

	repo, ok := NewPersonalAwareAccountRepository(client, db, scheduler).(*personalAccountRepository)
	if !ok {
		t.Fatal("expected Personal account adapter")
	}
	account := &service.Account{
		Name:        "gemini-oauth-write",
		Platform:    service.PlatformGemini,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "old-access", "refresh_token": "refresh"},
		Extra:       map[string]any{},
		Concurrency: 1,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("create OAuth account: %v", err)
	}

	newCredentials := map[string]any{
		"access_token":  "new-access",
		"refresh_token": "refresh-rotated",
		"expires_at":    time.Now().Add(time.Hour).Unix(),
	}
	if err := repo.UpdateCredentials(ctx, account.ID, newCredentials); err != nil {
		t.Fatalf("persist refreshed OAuth credentials on SQLite: %v", err)
	}
	loaded, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("reload refreshed OAuth account: %v", err)
	}
	if got, _ := loaded.Credentials["access_token"].(string); got != "new-access" {
		t.Fatalf("access token = %q, want new-access", got)
	}
	if got, _ := loaded.Credentials["refresh_token"].(string); got != "refresh-rotated" {
		t.Fatalf("refresh token = %q, want refresh-rotated", got)
	}

	loaded.Name = "gemini-oauth-edited"
	loaded.Priority = 7
	if err := repo.Update(ctx, loaded); err != nil {
		t.Fatalf("edit Personal OAuth account on SQLite: %v", err)
	}
	edited, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("reload edited OAuth account: %v", err)
	}
	if edited.Name != "gemini-oauth-edited" || edited.Priority != 7 {
		t.Fatalf("Personal account edit not persisted: %+v", edited)
	}
}

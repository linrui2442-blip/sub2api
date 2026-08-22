package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	entgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPersonalSQLiteCoreAccountRepositorySmoke(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "personal-core.db")
	drv, db, err := openPersonalSQLite(dbPath)
	if err != nil {
		t.Fatalf("open personal sqlite: %v", err)
	}
	client := ent.NewClient(ent.Driver(drv))
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if err := ensurePersonalSQLiteInfrastructure(ctx, db); err != nil {
		t.Fatalf("create personal infrastructure schema: %v", err)
	}
	if err := ensureSimpleModeDefaultGroups(ctx, client); err != nil {
		t.Fatalf("seed simple-mode groups: %v", err)
	}

	openAIGroup, err := client.Group.Query().Where(entgroup.NameEQ(service.PlatformOpenAI + "-default")).Only(ctx)
	if err != nil {
		t.Fatalf("load OpenAI default group: %v", err)
	}

	embeddedRedis, err := newPersonalEmbeddedRedis()
	if err != nil {
		t.Fatalf("start embedded redis: %v", err)
	}
	defer embeddedRedis.Close()
	rdb := embeddedRedis.Client()
	defer func() { _ = rdb.Close() }()
	scheduler := newSchedulerCacheWithChunkSizes(rdb, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize)

	repo := newAccountRepositoryWithSQL(client, db, scheduler)
	account := &service.Account{
		Name:        "personal-openai-smoke",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-access", "refresh_token": "test-refresh"},
		Extra:       map[string]any{},
		Concurrency: 1,
		Priority:    10,
		Status:      service.StatusActive,
		Schedulable: true,
	}
	bindings := []service.AccountGroup{{GroupID: openAIGroup.ID, Priority: 10}}
	if err := repo.CreateWithAccountGroups(ctx, account, bindings); err != nil {
		t.Fatalf("create OpenAI account with group: %v", err)
	}
	if account.ID <= 0 {
		t.Fatalf("created account must receive ID, got %d", account.ID)
	}

	loaded, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("read OpenAI account through repository: %v", err)
	}
	if loaded.ID != account.ID || loaded.Platform != service.PlatformOpenAI || loaded.Type != service.AccountTypeOAuth {
		t.Fatalf("unexpected loaded account: %+v", loaded)
	}
	if len(loaded.GroupIDs) != 1 || loaded.GroupIDs[0] != openAIGroup.ID {
		t.Fatalf("account group binding not preserved: %+v", loaded.GroupIDs)
	}

	var outboxCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", account.ID).Scan(&outboxCount); err != nil {
		t.Fatalf("query scheduler outbox: %v", err)
	}
	if outboxCount < 1 {
		t.Fatalf("expected scheduler outbox event for created account, got %d", outboxCount)
	}
}

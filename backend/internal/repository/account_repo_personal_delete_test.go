package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
)

func TestAccountRepositoryDeleteOnPersonalSQLiteWithoutScheduledTestPlans(t *testing.T) {
	drv, db, err := openPersonalSQLite(filepath.Join(t.TempDir(), "personal.db"))
	if err != nil {
		t.Fatalf("open personal sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	client := ent.NewClient(ent.Driver(drv))
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("create personal schema: %v", err)
	}
	account, err := client.Account.Create().
		SetName("delete-me").
		SetPlatform("antigravity").
		SetType("oauth").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	repo := NewAccountRepository(client, db, nil)
	if err := repo.Delete(ctx, account.ID); err != nil {
		t.Fatalf("delete account without legacy scheduled_test_plans table: %v", err)
	}
	exists, err := repo.ExistsByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("check deleted account: %v", err)
	}
	if exists {
		t.Fatal("deleted account remains visible")
	}
}

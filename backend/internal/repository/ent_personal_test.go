package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
)

func TestPersonalSQLiteCreatesFullEntSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "personal.db")
	drv, db, err := openPersonalSQLite(dbPath)
	if err != nil {
		t.Fatalf("open personal sqlite: %v", err)
	}

	client := ent.NewClient(ent.Driver(drv))
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Schema.Create(ctx); err != nil {
		t.Fatalf("create full Ent schema on SQLite: %v", err)
	}

	var foreignKeys int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys must be enabled, got %d", foreignKeys)
	}

	var accountsTable string
	if err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='accounts'").Scan(&accountsTable); err != nil {
		t.Fatalf("accounts table missing after schema create: %v", err)
	}
	if accountsTable != "accounts" {
		t.Fatalf("unexpected accounts table name %q", accountsTable)
	}
}

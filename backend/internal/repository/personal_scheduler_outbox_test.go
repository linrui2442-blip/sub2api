package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestPersonalSQLiteSchedulerOutboxContracts(t *testing.T) {
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	drv, db, err := openPersonalSQLite(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatalf("open personal sqlite: %v", err)
	}
	defer func() { _ = drv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ensurePersonalSQLiteInfrastructure(ctx, db); err != nil {
		t.Fatalf("create personal infrastructure: %v", err)
	}

	accountID := int64(7)
	payload := map[string]any{"group_ids": []int64{1}}
	if err := enqueueSchedulerOutbox(ctx, db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, payload); err != nil {
		t.Fatalf("enqueue first event: %v", err)
	}
	if err := enqueueSchedulerOutbox(ctx, db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, payload); err != nil {
		t.Fatalf("enqueue duplicate event: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox").Scan(&count); err != nil {
		t.Fatalf("count deduped events: %v", err)
	}
	if count != 1 {
		t.Fatalf("pending dedup must keep one event, got %d", count)
	}

	repo := NewSchedulerOutboxRepository(db)
	events, err := repo.ListAfterAndReleaseDedup(ctx, 0, 100)
	if err != nil {
		t.Fatalf("list personal outbox: %v", err)
	}
	if len(events) != 1 || events[0].AccountID == nil || *events[0].AccountID != accountID {
		t.Fatalf("unexpected outbox events: %+v", events)
	}
	if events[0].CreatedAt.IsZero() {
		t.Fatal("created_at must scan into time.Time")
	}

	if err := enqueueSchedulerOutbox(ctx, db, service.SchedulerOutboxEventAccountChanged, &accountID, nil, payload); err != nil {
		t.Fatalf("enqueue after dedup release: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox").Scan(&count); err != nil {
		t.Fatalf("count released events: %v", err)
	}
	if count != 2 {
		t.Fatalf("released dedup key must allow a new event, got %d rows", count)
	}

	firstCreated, ok, err := repo.FirstCreatedAtAfter(ctx, 0)
	if err != nil || !ok || firstCreated.IsZero() {
		t.Fatalf("first created_at = %v, %v, %v", firstCreated, ok, err)
	}
	maxID, err := repo.MaxID(ctx)
	if err != nil || maxID < 2 {
		t.Fatalf("max outbox id = %d, %v", maxID, err)
	}

	lease1, acquired, err := repo.TryAcquireCleanupLock(ctx)
	if err != nil || !acquired || lease1 == nil {
		t.Fatalf("first cleanup lock = %v, %v, %v", lease1, acquired, err)
	}
	if lease2, acquired2, err2 := repo.TryAcquireCleanupLock(ctx); err2 != nil || acquired2 || lease2 != nil {
		t.Fatalf("second cleanup lock must be unavailable: %v, %v, %v", lease2, acquired2, err2)
	}
	lease1.Release()
	lease3, acquired3, err3 := repo.TryAcquireCleanupLock(ctx)
	if err3 != nil || !acquired3 || lease3 == nil {
		t.Fatalf("cleanup lock after release = %v, %v, %v", lease3, acquired3, err3)
	}
	lease3.Release()

	if _, err := db.ExecContext(ctx, "UPDATE scheduler_outbox SET created_at = datetime('now', '-20 seconds')"); err != nil {
		t.Fatalf("age outbox rows: %v", err)
	}
	deleted, err := repo.DeleteConsumedUpTo(ctx, maxID, 100)
	if err != nil {
		t.Fatalf("cleanup consumed outbox: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 cleaned rows, got %d", deleted)
	}
}

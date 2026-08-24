package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestAuthCacheInvalidationOutboxRepository_ClaimUsesSQLiteLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	created := time.Now().UTC()
	mock.ExpectQuery("(?s)UPDATE auth_cache_invalidation_outbox.*claimed_at = \\?.*RETURNING").
		WithArgs(sqlmock.AnyArg(), "worker-a", sqlmock.AnyArg(), sqlmock.AnyArg(), 100, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cache_key", "attempts", "delivery_stage", "created_at"}).
			AddRow(int64(4), strings.Repeat("a", 64), 2, 1, created))

	repo := NewAuthCacheInvalidationOutboxRepository(db)
	events, err := repo.Claim(context.Background(), "worker-a", 100, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(4), events[0].ID)
	require.Equal(t, 1, events[0].Stage)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthCacheInvalidationOutboxRepository_ClaimIsBoundedByDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("(?s)UPDATE auth_cache_invalidation_outbox.*LIMIT \\?.*RETURNING").
		WithArgs(sqlmock.AnyArg(), "worker", sqlmock.AnyArg(), sqlmock.AnyArg(), 100, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cache_key", "attempts", "delivery_stage", "created_at"}))
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	_, err = repo.Claim(context.Background(), "worker", 0, 0)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func openAuthInvalidationSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, deleted_at DATETIME NULL);
		CREATE TABLE groups (id INTEGER PRIMARY KEY, name TEXT NOT NULL, deleted_at DATETIME NULL);
	`)
	require.NoError(t, err)
	require.NoError(t, ensurePersonalSQLiteInfrastructure(context.Background(), db))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestAuthCacheInvalidationOutboxRepository_SQLiteRuntimeLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openAuthInvalidationSQLite(t)
	repo := NewAuthCacheInvalidationOutboxRepository(db)

	// An empty queue is a normal poll, not an error.
	events, err := repo.Claim(ctx, "worker-a", 10, 30*time.Second)
	require.NoError(t, err)
	require.Empty(t, events)

	now := time.Now().UTC()
	result, err := db.ExecContext(ctx, `
		INSERT INTO auth_cache_invalidation_outbox (cache_key, available_at, created_at)
		VALUES (?, ?, ?)
	`, strings.Repeat("a", 64), now.Add(-time.Second), now)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)

	events, err = repo.Claim(ctx, "worker-a", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, id, events[0].ID)

	// An active lease cannot be stolen by another worker.
	events, err = repo.Claim(ctx, "worker-b", 10, 30*time.Second)
	require.NoError(t, err)
	require.Empty(t, events)

	// Retry clears ownership, increments attempts and honors backoff.
	retryAt := time.Now().UTC().Add(time.Minute)
	require.NoError(t, repo.RetryClaimed(ctx, id, "worker-a", retryAt, "temporary failure"))
	events, err = repo.Claim(ctx, "worker-b", 10, 30*time.Second)
	require.NoError(t, err)
	require.Empty(t, events)

	_, err = db.ExecContext(ctx, `UPDATE auth_cache_invalidation_outbox SET available_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), id)
	require.NoError(t, err)
	events, err = repo.Claim(ctx, "worker-b", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, 1, events[0].Attempts)

	// Successful first and second passes retain then remove the event.
	require.NoError(t, repo.ScheduleSecondPass(ctx, id, "worker-b", time.Now().UTC().Add(-time.Second)))
	events, err = repo.Claim(ctx, "worker-b", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, 1, events[0].Stage)
	require.NoError(t, repo.DeleteClaimed(ctx, id, "worker-b"))

	events, err = repo.Claim(ctx, "worker-b", 10, 30*time.Second)
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestAuthCacheInvalidationOutboxRepository_ClaimOwnershipTransitions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewAuthCacheInvalidationOutboxRepository(db)

	next := time.Now().UTC().Add(time.Minute)
	mock.ExpectExec("UPDATE auth_cache_invalidation_outbox").
		WithArgs(int64(1), "worker", next).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.ScheduleSecondPass(context.Background(), 1, "worker", next))

	retryAt := next.Add(time.Minute)
	mock.ExpectExec("UPDATE auth_cache_invalidation_outbox").
		WithArgs(int64(2), "worker", retryAt, "publish failed").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.RetryClaimed(context.Background(), 2, "worker", retryAt, "publish failed"))

	mock.ExpectExec("DELETE FROM auth_cache_invalidation_outbox").
		WithArgs(int64(3), "worker").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.DeleteClaimed(context.Background(), 3, "worker"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthCacheInvalidationOutboxRepository_RejectsLostClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectExec("DELETE FROM auth_cache_invalidation_outbox").
		WithArgs(int64(3), "old-worker").
		WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	err = repo.DeleteClaimed(context.Background(), 3, "old-worker")
	require.ErrorContains(t, err, "no longer owned")
}

func TestAuthCacheInvalidationOutboxRepository_StatsExposeDurableLagAndFailures(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	oldest := time.Now().UTC().Add(-time.Minute)
	mock.ExpectQuery("(?s)SELECT COUNT\\(\\*\\), MIN\\(created_at\\), COALESCE\\(MAX\\(attempts\\), 0\\)").
		WillReturnRows(sqlmock.NewRows([]string{"count", "min", "max", "last_error"}).AddRow(5, oldest, 7, "redis down"))
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	stats, err := repo.Stats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(5), stats.Pending)
	require.Equal(t, 7, stats.MaxAttempts)
	require.Equal(t, "redis down", stats.LastError)
	require.NotNil(t, stats.OldestCreatedAt)
}

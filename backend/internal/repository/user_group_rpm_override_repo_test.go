package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestUserGroupRPMOverrideRepositorySQLiteLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:user_group_rpm_override?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			username TEXT NOT NULL,
			email TEXT NOT NULL,
			notes TEXT,
			status TEXT NOT NULL,
			deleted_at DATETIME NULL
		)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE groups (id INTEGER PRIMARY KEY, name TEXT NOT NULL, deleted_at DATETIME NULL)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO users (id, username, email, notes, status) VALUES (1, 'member', 'member@example.test', '', 'active')`)
	require.NoError(t, err)
	require.NoError(t, ensurePersonalSQLiteInfrastructure(ctx, db))

	repo := NewUserGroupRPMOverrideRepository(db)
	limit := 24
	require.NoError(t, repo.SyncGroupRPMOverrides(ctx, 7, []service.GroupRPMOverrideInput{{UserID: 1, RPMOverride: &limit}}))

	got, err := repo.GetRPMOverrideByUserAndGroup(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, limit, *got)

	entries, err := repo.ListByGroupID(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, []service.UserGroupRPMOverrideEntry{{
		UserID: 1, UserName: "member", UserEmail: "member@example.test", UserStatus: "active", RPMOverride: limit,
	}}, entries)

	require.NoError(t, repo.SyncGroupRPMOverrides(ctx, 7, []service.GroupRPMOverrideInput{{UserID: 1, RPMOverride: nil}}))
	got, err = repo.GetRPMOverrideByUserAndGroup(ctx, 1, 7)
	require.NoError(t, err)
	require.Nil(t, got)
}

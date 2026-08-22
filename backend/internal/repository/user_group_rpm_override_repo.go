package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userGroupRPMOverrideRepository struct {
	sql *sql.DB
}

// NewUserGroupRPMOverrideRepository creates the SQLite-compatible repository
// for member-specific RPM limits.
func NewUserGroupRPMOverrideRepository(sqlDB *sql.DB) service.UserGroupRPMOverrideRepository {
	return &userGroupRPMOverrideRepository{sql: sqlDB}
}

func (r *userGroupRPMOverrideRepository) GetRPMOverrideByUserAndGroup(ctx context.Context, userID, groupID int64) (*int, error) {
	var rpm int
	err := scanSingleRow(ctx, r.sql,
		`SELECT rpm_override FROM user_group_rpm_overrides WHERE user_id = ? AND group_id = ?`,
		[]any{userID, groupID}, &rpm)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rpm, nil
}

func (r *userGroupRPMOverrideRepository) ListByGroupID(ctx context.Context, groupID int64) ([]service.UserGroupRPMOverrideEntry, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT policy.user_id, users.username, users.email, COALESCE(users.notes, ''), users.status, policy.rpm_override
		FROM user_group_rpm_overrides AS policy
		JOIN users ON users.id = policy.user_id AND users.deleted_at IS NULL
		WHERE policy.group_id = ?
		ORDER BY policy.user_id
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.UserGroupRPMOverrideEntry, 0)
	for rows.Next() {
		var entry service.UserGroupRPMOverrideEntry
		if err := rows.Scan(&entry.UserID, &entry.UserName, &entry.UserEmail, &entry.UserNotes, &entry.UserStatus, &entry.RPMOverride); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

// SyncGroupRPMOverrides replaces a group's policies. Nil values are ignored,
// which gives the API a simple way to clear an individual entry.
func (r *userGroupRPMOverrideRepository) SyncGroupRPMOverrides(ctx context.Context, groupID int64, entries []service.GroupRPMOverrideInput) error {
	tx, err := r.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_group_rpm_overrides WHERE group_id = ?`, groupID); err != nil {
		return err
	}
	if len(entries) == 0 {
		return tx.Commit()
	}

	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.UserID <= 0 || entry.RPMOverride == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_group_rpm_overrides (user_id, group_id, rpm_override, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user_id, group_id) DO UPDATE SET
				rpm_override = excluded.rpm_override,
				updated_at = excluded.updated_at
		`, entry.UserID, groupID, *entry.RPMOverride, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *userGroupRPMOverrideRepository) ClearGroupRPMOverrides(ctx context.Context, groupID int64) error {
	_, err := r.sql.ExecContext(ctx, `DELETE FROM user_group_rpm_overrides WHERE group_id = ?`, groupID)
	return err
}

func (r *userGroupRPMOverrideRepository) DeleteByGroupID(ctx context.Context, groupID int64) error {
	_, err := r.sql.ExecContext(ctx, `DELETE FROM user_group_rpm_overrides WHERE group_id = ?`, groupID)
	return err
}

func (r *userGroupRPMOverrideRepository) DeleteByUserID(ctx context.Context, userID int64) error {
	_, err := r.sql.ExecContext(ctx, `DELETE FROM user_group_rpm_overrides WHERE user_id = ?`, userID)
	return err
}

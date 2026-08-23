package repository

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const simpleModeDefaultGroupDescription = "Auto-created default group"

func ensureSimpleModeDefaultGroups(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	requiredPlatforms := []string{
		service.PlatformAnthropic,
		service.PlatformOpenAI,
		service.PlatformGemini,
		service.PlatformAntigravity,
		service.PlatformGrok,
	}

	for _, platform := range requiredPlatforms {
		name := platform + "-default"
		if err := createGroupIfNotExists(ctx, client, name, platform); err != nil {
			return err
		}
	}
	if err := convergeLegacyAntigravityDefaultGroups(ctx, client); err != nil {
		return err
	}

	return nil
}

// convergeLegacyAntigravityDefaultGroups folds the two historical Personal
// bootstrap groups into the single canonical antigravity-default group. Only
// groups created by our bootstrapper are eligible; similarly named custom
// groups are deliberately left untouched. Usage rows keep their historical
// group_id and remain readable because legacy groups are soft-deleted.
func convergeLegacyAntigravityDefaultGroups(ctx context.Context, client *dbent.Client) error {
	canonical, err := client.Group.Query().Where(
		group.NameEQ(service.PlatformAntigravity+"-default"),
		group.PlatformEQ(service.PlatformAntigravity),
		group.DeletedAtIsNil(),
	).Only(ctx)
	if err != nil {
		return fmt.Errorf("load canonical antigravity default group: %w", err)
	}

	legacy, err := client.Group.Query().Where(
		group.NameIn(service.PlatformAntigravity+"-default-1", service.PlatformAntigravity+"-default-2"),
		group.PlatformEQ(service.PlatformAntigravity),
		group.DescriptionEQ(simpleModeDefaultGroupDescription),
		group.DeletedAtIsNil(),
	).All(ctx)
	if err != nil {
		return fmt.Errorf("load legacy antigravity default groups: %w", err)
	}
	if len(legacy) == 0 {
		return nil
	}

	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin antigravity default group convergence: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, old := range legacy {
		// Preserve account membership and its priority. INSERT OR IGNORE also
		// makes repeated or interrupted startup convergence harmless on SQLite.
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO account_groups (account_id, group_id, priority, created_at)
			SELECT account_id, ?, priority, created_at FROM account_groups WHERE group_id = ?`, canonical.ID, old.ID); err != nil {
			return fmt.Errorf("migrate account memberships from group %d: %w", old.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM account_groups WHERE group_id = ?`, old.ID); err != nil {
			return fmt.Errorf("remove legacy account memberships for group %d: %w", old.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_allowed_groups (user_id, group_id, created_at)
			SELECT user_id, ?, created_at FROM user_allowed_groups WHERE group_id = ?`, canonical.ID, old.ID); err != nil {
			return fmt.Errorf("migrate member permissions from group %d: %w", old.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_allowed_groups WHERE group_id = ?`, old.ID); err != nil {
			return fmt.Errorf("remove legacy member permissions for group %d: %w", old.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET group_id = ?, updated_at = ? WHERE group_id = ? AND deleted_at IS NULL`, canonical.ID, time.Now(), old.ID); err != nil {
			return fmt.Errorf("migrate api keys from group %d: %w", old.ID, err)
		}
		if err := tx.Group.UpdateOneID(old.ID).SetDeletedAt(time.Now()).Exec(ctx); err != nil {
			return fmt.Errorf("soft-delete legacy antigravity default group %d: %w", old.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit antigravity default group convergence: %w", err)
	}
	return nil
}

func createGroupIfNotExists(ctx context.Context, client *dbent.Client, name, platform string) error {
	exists, err := client.Group.Query().
		Where(group.NameEQ(name), group.DeletedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check group exists %s: %w", name, err)
	}
	if exists {
		return nil
	}

	_, err = client.Group.Create().
		SetName(name).
		SetDescription(simpleModeDefaultGroupDescription).
		SetPlatform(platform).
		SetStatus(service.StatusActive).
		SetIsExclusive(false).
		Save(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			// Concurrent server startups may race on creation; treat as success.
			return nil
		}
		return fmt.Errorf("create default group %s: %w", name, err)
	}
	return nil
}

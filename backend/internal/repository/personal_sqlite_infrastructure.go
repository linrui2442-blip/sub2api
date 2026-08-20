package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// ensurePersonalSQLiteInfrastructure creates the small set of runtime objects
// that upstream owns through hand-written PostgreSQL migrations rather than Ent
// schemas. Keep this list intentionally narrow: Personal Edition should only
// carry infrastructure required by its private GPT/Gemini gateway paths.
func ensurePersonalSQLiteInfrastructure(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("nil personal sqlite db")
	}

	statements := []string{
		// Soft-delete aware uniqueness normally comes from upstream migration 016.
		`CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_active
			ON users(email) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS groups_name_unique_active
			ON groups(name) WHERE deleted_at IS NULL`,

		// Scheduler outbox is migration-owned upstream and is required by account
		// mutation/snapshot propagation even in a single-process Personal runtime.
		`CREATE TABLE IF NOT EXISTS scheduler_outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			account_id INTEGER NULL,
			group_id INTEGER NULL,
			payload BLOB NULL,
			dedup_key TEXT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_outbox_created_at
			ON scheduler_outbox (created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduler_outbox_pending_dedup_key
			ON scheduler_outbox (dedup_key)
			WHERE dedup_key IS NOT NULL`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create personal sqlite infrastructure: %w", err)
		}
	}
	return nil
}

package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// ensurePersonalSQLiteInfrastructure creates the small set of runtime objects
// that upstream owns through hand-written migrations rather than Ent
// schemas. Keep this list intentionally narrow: Personal Edition should only
// carry infrastructure required by its private GPT/Gemini gateway paths.
func ensurePersonalSQLiteInfrastructure(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("nil personal sqlite db")
	}

	statements := []string{
		// Remove unreachable external-user identity adoption tables from upgrades.
		`DROP TABLE IF EXISTS identity_adoption_decisions`,
		`DROP TABLE IF EXISTS auth_identity_channels`,
		`DROP TABLE IF EXISTS pending_auth_sessions`,

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

		// Browser refresh sessions must survive a local restart. The token itself
		// is never stored: token_hash is the SHA-256 digest used by the existing
		// rotation/replay-defense flow.
		`CREATE TABLE IF NOT EXISTS personal_refresh_tokens (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			family_id TEXT NOT NULL,
			payload TEXT NOT NULL,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_personal_refresh_tokens_user
			ON personal_refresh_tokens (user_id, expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_personal_refresh_tokens_family
			ON personal_refresh_tokens (family_id, expires_at)`,

		// Member-specific RPM policies are an access-control and gateway
		// protection feature. They replace the upstream commercial user-group
		// pricing table and are intentionally SQLite-native.
		`CREATE TABLE IF NOT EXISTS user_group_rpm_overrides (
			user_id INTEGER NOT NULL,
			group_id INTEGER NOT NULL,
			rpm_override INTEGER NOT NULL CHECK (rpm_override >= 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, group_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_group_rpm_overrides_group
			ON user_group_rpm_overrides (group_id, user_id)`,

		// Audit and operational logs are core Personal observability. They are
		// intentionally local and are not part of Ent's generated schema.
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			actor_user_id INTEGER NULL, actor_email TEXT NOT NULL DEFAULT '',
			actor_role TEXT NOT NULL DEFAULT '', auth_method TEXT NOT NULL DEFAULT '',
			credential_masked TEXT NOT NULL DEFAULT '', action TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', request_id TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', request_body TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 0, latency_ms INTEGER NOT NULL DEFAULT 0,
			extra TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_user_id ON audit_logs(actor_user_id)`,
		`CREATE TABLE IF NOT EXISTS ops_system_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			host TEXT NULL, level TEXT NOT NULL, component TEXT NOT NULL, message TEXT NOT NULL,
			request_id TEXT NULL, client_request_id TEXT NULL, user_id INTEGER NULL,
			api_key_id INTEGER NULL, account_id INTEGER NULL, platform TEXT NULL, model TEXT NULL,
			extra TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_system_logs_created_at ON ops_system_logs(created_at)`,
		`CREATE TABLE IF NOT EXISTS ops_error_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, request_id TEXT NULL, client_request_id TEXT NULL,
			user_id INTEGER NULL, api_key_id INTEGER NULL, account_id INTEGER NULL, group_id INTEGER NULL,
			client_ip TEXT NULL, platform TEXT NULL, model TEXT NULL, request_path TEXT NULL,
			stream INTEGER NOT NULL DEFAULT 0, inbound_endpoint TEXT NULL, upstream_endpoint TEXT NULL,
			requested_model TEXT NULL, upstream_model TEXT NULL, request_type INTEGER NULL, user_agent TEXT NULL,
			error_phase TEXT NOT NULL, error_type TEXT NOT NULL, severity TEXT NULL, status_code INTEGER NULL,
			is_business_limited INTEGER NOT NULL DEFAULT 0, is_count_tokens INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NULL, error_body TEXT NULL, error_source TEXT NULL, error_owner TEXT NULL,
			upstream_status_code INTEGER NULL, upstream_error_message TEXT NULL, upstream_error_detail TEXT NULL,
			upstream_errors TEXT NULL, auth_latency_ms INTEGER NULL, routing_latency_ms INTEGER NULL,
			upstream_latency_ms INTEGER NULL, response_latency_ms INTEGER NULL, time_to_first_token_ms INTEGER NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, api_key_prefix TEXT NULL,
			resolved INTEGER NOT NULL DEFAULT 0, resolved_at DATETIME NULL, resolved_by_user_id INTEGER NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_error_logs_created_at ON ops_error_logs(created_at)`,
		`CREATE TABLE IF NOT EXISTS ops_ingress_reject_aggregates (
			id INTEGER PRIMARY KEY AUTOINCREMENT, bucket_start DATETIME NOT NULL,
			reject_reason TEXT NOT NULL, route_family TEXT NOT NULL, protocol TEXT NOT NULL,
			client_ip TEXT NOT NULL DEFAULT '', user_id INTEGER NOT NULL DEFAULT 0, api_key_id INTEGER NOT NULL DEFAULT 0,
			request_count INTEGER NOT NULL, first_seen DATETIME NOT NULL, last_seen DATETIME NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(bucket_start, reject_reason, route_family, protocol, client_ip, user_id, api_key_id)
		)`,

		// Local profile avatar metadata is stored beside the SQLite user record.
		`CREATE TABLE IF NOT EXISTS user_avatars (
			user_id INTEGER PRIMARY KEY,
			storage_provider TEXT NOT NULL DEFAULT 'local',
			storage_key TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			content_type TEXT NOT NULL DEFAULT '',
			byte_size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create personal sqlite infrastructure: %w", err)
		}
	}
	return nil
}

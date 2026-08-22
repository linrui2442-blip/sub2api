package securityaudit

import (
	"context"
	"database/sql"
	"fmt"
)

// ensurePromptAuditSQLiteSchema owns the local prompt-audit persistence. The
// tables intentionally use SQLite-native types and have no PostgreSQL runtime
// dependency.
func ensurePromptAuditSQLiteSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS prompt_audit_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, request_id TEXT NOT NULL DEFAULT '', user_id INTEGER,
			username_snapshot TEXT NOT NULL DEFAULT '', user_email_snapshot TEXT NOT NULL DEFAULT '', api_key_id INTEGER,
			api_key_name_snapshot TEXT NOT NULL DEFAULT '', group_id INTEGER, group_name TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '', endpoint TEXT NOT NULL DEFAULT '', protocol TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '', prompt_hash TEXT NOT NULL DEFAULT '', redacted_preview TEXT NOT NULL DEFAULT '',
			prompt_length INTEGER NOT NULL DEFAULT 0, message_count INTEGER NOT NULL DEFAULT 0, stage TEXT NOT NULL DEFAULT 'http',
			execution_mode TEXT NOT NULL DEFAULT 'async_audit', config_version INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL DEFAULT 'staging', attempts INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL DEFAULT 3,
			claim_version INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processing_started_at DATETIME, processed_at DATETIME, last_error_code TEXT NOT NULL DEFAULT '',
			last_error_message TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL,
			FOREIGN KEY(api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL,
			FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE SET NULL)`,
		`CREATE TABLE IF NOT EXISTS prompt_audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, job_id INTEGER NOT NULL, request_id TEXT NOT NULL DEFAULT '', user_id INTEGER,
			username_snapshot TEXT NOT NULL DEFAULT '', user_email_snapshot TEXT NOT NULL DEFAULT '', api_key_id INTEGER,
			api_key_name_snapshot TEXT NOT NULL DEFAULT '', group_id INTEGER, group_name TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '', endpoint TEXT NOT NULL DEFAULT '', protocol TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '', prompt_hash TEXT NOT NULL DEFAULT '', redacted_preview TEXT NOT NULL DEFAULT '',
			stage TEXT NOT NULL DEFAULT 'http', decision TEXT NOT NULL DEFAULT 'pass', risk_level TEXT NOT NULL DEFAULT 'low',
			action TEXT NOT NULL DEFAULT 'Allow', categories TEXT NOT NULL DEFAULT '[]', matched_scanners TEXT NOT NULL DEFAULT '[]',
			scanner_scores TEXT NOT NULL DEFAULT '{}', scanner_evidence TEXT NOT NULL DEFAULT '{}', scanner_backend TEXT NOT NULL DEFAULT '',
			scanner_version TEXT NOT NULL DEFAULT '', guard_endpoint_id TEXT NOT NULL DEFAULT '', policy_id TEXT NOT NULL DEFAULT '',
			policy_version INTEGER NOT NULL DEFAULT 0, config_version INTEGER NOT NULL DEFAULT 1, chunk_total INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0, full_prompt TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(job_id) REFERENCES prompt_audit_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL,
			FOREIGN KEY(api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL,
			FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE SET NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_schedule ON prompt_audit_jobs(status,next_attempt_at,id)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_created ON prompt_audit_events(created_at DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_job ON prompt_audit_events(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_request ON prompt_audit_events(request_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create prompt audit SQLite object: %w", err)
		}
	}
	return nil
}

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"entgo.io/ent/dialect"
)

// accountCredentialRevision is deliberately private. Its raw field is the
// exact database value captured by LoadCredentialSnapshot and must never be
// formatted or logged.
type accountCredentialRevision struct {
	dialect string
	raw     []byte
}

func cloneRepositoryCredentialMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneRepositoryCredentialMap(nested)
		} else {
			out[key] = value
		}
	}
	return out
}

func scanRawCredentialValue(rows *sql.Rows) ([]byte, error) {
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrAccountNotFound
	}
	var raw any
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case []byte:
		return append([]byte(nil), value...), nil
	case string:
		return []byte(value), nil
	default:
		return nil, fmt.Errorf("unsupported credential database representation %T", raw)
	}
}

func (r *accountRepository) LoadCredentialSnapshot(ctx context.Context, accountID int64) (*service.AccountCredentialSnapshot, error) {
	if r == nil || r.sql == nil || r.client == nil {
		return nil, errors.New("account credential snapshot repository is unavailable")
	}
	dialectName := r.client.Driver().Dialect()
	query := "SELECT credentials FROM accounts WHERE id = ? AND deleted_at IS NULL"
	if dialectName == dialect.Postgres {
		query = "SELECT credentials FROM accounts WHERE id = $1 AND deleted_at IS NULL"
	}
	rows, err := r.sql.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	raw, err := scanRawCredentialValue(rows)
	if err != nil {
		return nil, err
	}
	credentials := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &credentials); err != nil {
			return nil, fmt.Errorf("decode stored account credentials: %w", err)
		}
	}
	return &service.AccountCredentialSnapshot{
		Credentials: credentials,
		Revision:    accountCredentialRevision{dialect: dialectName, raw: append([]byte(nil), raw...)},
	}, nil
}

func (r *accountRepository) CompareAndSwapCredentials(ctx context.Context, accountID int64, expectedRevision any, credentials map[string]any) (*service.AccountCredentialSnapshot, error) {
	revision, ok := expectedRevision.(accountCredentialRevision)
	if !ok || r == nil || r.sql == nil || r.client == nil || revision.dialect != r.client.Driver().Dialect() {
		return nil, service.ErrCredentialCASConflict
	}
	payload, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return nil, err
	}
	var result sql.Result
	switch revision.dialect {
	case dialect.SQLite:
		result, err = r.sql.ExecContext(ctx,
			"UPDATE accounts SET credentials = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL AND json(credentials) = json(?)",
			string(payload), time.Now(), accountID, nullableRawCredential(revision.raw))
	case dialect.Postgres:
		result, err = r.sql.ExecContext(ctx,
			"UPDATE accounts SET credentials = $1::jsonb, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL AND credentials IS NOT DISTINCT FROM $3::jsonb",
			string(payload), accountID, nullableRawCredential(revision.raw))
	default:
		return nil, fmt.Errorf("unsupported credential CAS dialect %q", revision.dialect)
	}
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		exists, existsErr := r.ExistsByID(ctx, accountID)
		if existsErr != nil {
			return nil, existsErr
		}
		if !exists {
			return nil, service.ErrAccountNotFound
		}
		return nil, service.ErrCredentialCASConflict
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return nil, err
	}
	r.syncSchedulerAccountSnapshot(ctx, accountID)
	return r.LoadCredentialSnapshot(ctx, accountID)
}

func nullableRawCredential(raw []byte) any {
	if raw == nil {
		return nil
	}
	return string(raw)
}

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// SQLite deliberately uses a straightforward transactional prepared INSERT.
// The PostgreSQL path keeps its CTE/json batch implementation; Personal favors
// correctness and upgrade safety over maximizing local write throughput.
const usageLogSQLiteInsert = `
	INSERT INTO usage_logs (
		user_id, api_key_id, account_id, request_id, model, requested_model,
		upstream_model, upstream_response_model, upstream_model_mismatch, group_id,
		input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
		cache_creation_5m_tokens, cache_creation_1h_tokens,
		image_output_tokens, image_input_tokens, request_type, stream, openai_ws_mode,
		duration_ms, first_token_ms, user_agent, ip_address, image_count, image_size,
		image_input_size, image_output_size, image_size_source, image_size_breakdown,
		video_count, video_resolution, video_duration_seconds, service_tier,
		reasoning_effort, inbound_endpoint, upstream_endpoint, channel_id,
		model_mapping_chain, session_id, created_at
	) SELECT
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
	WHERE NOT EXISTS (
		SELECT 1 FROM usage_logs WHERE request_id = ? AND api_key_id = ?
	)`

func sqliteUsageInsertArgs(prepared usageLogInsertPrepared) []any {
	args := make([]any, 0, len(prepared.args)+2)
	args = append(args, prepared.args...)
	args = append(args, prepared.requestID, prepared.args[1])
	return args
}

func createSingleUsageLogSQLite(ctx context.Context, sqlq sqlExecutor, log *service.UsageLog, prepared usageLogInsertPrepared) (bool, error) {
	result, err := sqlq.ExecContext(ctx, usageLogSQLiteInsert, sqliteUsageInsertArgs(prepared)...)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := scanSingleRow(ctx, sqlq,
		"SELECT id, created_at FROM usage_logs WHERE request_id = ? AND api_key_id = ?",
		[]any{prepared.requestID, prepared.args[1]}, &log.ID, &log.CreatedAt,
	); err != nil {
		return false, err
	}
	return rows > 0, nil
}

func batchInsertUsageLogsSQLite(db *sql.DB, keys []string, preparedByKey map[string]usageLogInsertPrepared) (map[string]bool, map[string]usageLogBatchState, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, true, err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, usageLogSQLiteInsert)
	if err != nil {
		return nil, nil, true, err
	}
	defer func() { _ = stmt.Close() }()

	insertedMap := make(map[string]bool, len(keys))
	stateMap := make(map[string]usageLogBatchState, len(keys))
	for _, key := range keys {
		prepared := preparedByKey[key]
		result, execErr := stmt.ExecContext(ctx, sqliteUsageInsertArgs(prepared)...)
		if execErr != nil {
			return insertedMap, stateMap, true, execErr
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return insertedMap, stateMap, false, rowsErr
		}
		insertedMap[key] = rows > 0
		var state usageLogBatchState
		if scanErr := tx.QueryRowContext(ctx,
			"SELECT id, created_at FROM usage_logs WHERE request_id = ? AND api_key_id = ?",
			prepared.requestID, prepared.args[1],
		).Scan(&state.ID, &state.CreatedAt); scanErr != nil {
			return insertedMap, stateMap, false, scanErr
		}
		stateMap[key] = state
	}
	if err := stmt.Close(); err != nil {
		return insertedMap, stateMap, false, err
	}
	if err := tx.Commit(); err != nil {
		return insertedMap, stateMap, false, err
	}
	return insertedMap, stateMap, false, nil
}

func (r *usageLogRepository) flushBestEffortBatchSQLite(db *sql.DB, batch []usageLogBestEffortRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err == nil {
		stmt, prepareErr := tx.PrepareContext(ctx, usageLogSQLiteInsert)
		if prepareErr != nil {
			err = prepareErr
		} else {
			for _, req := range batch {
				if _, execErr := stmt.ExecContext(ctx, sqliteUsageInsertArgs(req.prepared)...); execErr != nil {
					err = execErr
					break
				}
			}
			_ = stmt.Close()
		}
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	if err != nil {
		err = fmt.Errorf("sqlite usage batch insert: %w", err)
	}
	for _, req := range batch {
		if err == nil && req.prepared.requestID != "" && r.bestEffortRecent != nil {
			r.bestEffortRecent.SetDefault(usageLogBatchKey(req.prepared.requestID, req.apiKeyID), struct{}{})
		}
		sendUsageLogBestEffortResult(req.resultCh, err)
	}
}

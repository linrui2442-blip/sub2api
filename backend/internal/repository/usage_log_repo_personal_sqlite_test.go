package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func openUsageSQLiteTest(t *testing.T, name string) (*usageLogRepository, func()) {
	t.Helper()
	drv, db, err := openPersonalSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", name))
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	require.NoError(t, ensurePersonalSQLiteInfrastructure(context.Background(), db))
	// This repository test isolates usage persistence; parent repositories have
	// their own FK coverage and are not needed to validate the writer.
	_, err = db.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	repo := newUsageLogRepositoryWithSQL(client, db)
	require.True(t, repo.sqlite)
	return repo, func() { require.NoError(t, client.Close()) }
}

func personalUsageLog(requestID string, stream bool) *service.UsageLog {
	groupID := int64(4)
	upstream := "gemini-pro-agent"
	inbound := "/v1/chat/completions"
	upstreamEndpoint := "/v1internal:streamGenerateContent"
	return &service.UsageLog{
		UserID: 1, APIKeyID: 2, AccountID: 3, GroupID: &groupID,
		RequestID: requestID, Model: "gemini-3.1-pro-high",
		RequestedModel: "gemini-3.1-pro-high", UpstreamModel: &upstream,
		InputTokens: 2, OutputTokens: 3, Stream: stream,
		RequestType:     service.RequestTypeFromLegacy(stream, false),
		InboundEndpoint: &inbound, UpstreamEndpoint: &upstreamEndpoint,
		CreatedAt: time.Now().UTC(),
	}
}

func TestPersonalSQLiteUsageSingleAndDedup(t *testing.T) {
	repo, closeRepo := openUsageSQLiteTest(t, "usage-single")
	defer closeRepo()
	ctx := context.Background()
	first := personalUsageLog("req-single", false)
	inserted, err := repo.createSingle(ctx, repo.db, first)
	require.NoError(t, err)
	require.True(t, inserted)
	duplicate := personalUsageLog("req-single", false)
	inserted, err = repo.createSingle(ctx, repo.db, duplicate)
	require.NoError(t, err)
	require.False(t, inserted)
	require.Equal(t, first.ID, duplicate.ID)
	var count int
	require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE request_id=? AND api_key_id=?", "req-single", 2).Scan(&count))
	require.Equal(t, 1, count)
}

func TestPersonalSQLiteUsageBatchNonStreamAndStream(t *testing.T) {
	repo, closeRepo := openUsageSQLiteTest(t, "usage-batch")
	defer closeRepo()
	logs := []*service.UsageLog{personalUsageLog("req-sync", false), personalUsageLog("req-stream", true)}
	var wg sync.WaitGroup
	errs := make(chan error, len(logs))
	for _, log := range logs {
		wg.Add(1)
		go func(item *service.UsageLog) {
			defer wg.Done()
			inserted, err := repo.Create(context.Background(), item)
			if err == nil && !inserted {
				err = fmt.Errorf("not inserted")
			}
			errs <- err
		}(log)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	rows, err := repo.db.Query("SELECT request_id, stream, requested_model, upstream_model, input_tokens, output_tokens FROM usage_logs ORDER BY request_id")
	require.NoError(t, err)
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var requestID, requested, upstream string
		var stream bool
		var input, output int
		require.NoError(t, rows.Scan(&requestID, &stream, &requested, &upstream, &input, &output))
		seen[requestID] = stream
		require.Equal(t, "gemini-3.1-pro-high", requested)
		require.Equal(t, "gemini-pro-agent", upstream)
		require.Equal(t, 2, input)
		require.Equal(t, 3, output)
	}
	require.Equal(t, map[string]bool{"req-stream": true, "req-sync": false}, seen)
}

func TestPersonalSQLiteUsageBestEffortInsert(t *testing.T) {
	repo, closeRepo := openUsageSQLiteTest(t, "usage-best-effort")
	defer closeRepo()
	log := personalUsageLog("req-best-effort", false)
	require.NoError(t, repo.CreateBestEffort(context.Background(), log))
	var requested, upstream string
	require.NoError(t, repo.db.QueryRow(
		"SELECT requested_model, upstream_model FROM usage_logs WHERE request_id=? AND api_key_id=?",
		log.RequestID, log.APIKeyID,
	).Scan(&requested, &upstream))
	require.Equal(t, log.RequestedModel, requested)
	require.Equal(t, *log.UpstreamModel, upstream)
}

func TestPersonalSQLiteUsageSchemaUpgradeIsIdempotentAndPreservesRows(t *testing.T) {
	drv, db, err := openPersonalSQLite("file:usage-upgrade?mode=memory&cache=shared")
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	defer func() { require.NoError(t, client.Close()) }()
	_, err = db.Exec(`CREATE TABLE usage_logs (id INTEGER PRIMARY KEY, request_id TEXT NOT NULL, api_key_id INTEGER NOT NULL, model TEXT NOT NULL, created_at DATETIME NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO usage_logs(id,request_id,api_key_id,model,created_at) VALUES(1,'historical',2,'old-model',CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	require.NoError(t, ensurePersonalSQLiteUsageLogColumns(context.Background(), db))
	require.NoError(t, ensurePersonalSQLiteUsageLogColumns(context.Background(), db))
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE request_id='historical'").Scan(&count))
	require.Equal(t, 1, count)
	for _, column := range []string{"image_output_tokens", "image_input_tokens", "request_type", "openai_ws_mode", "service_tier", "reasoning_effort", "inbound_endpoint", "upstream_endpoint", "session_id"} {
		var found int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('usage_logs') WHERE name=?", column).Scan(&found))
		require.Equal(t, 1, found, column)
	}
}

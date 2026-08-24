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
	defer func() { require.NoError(t, rows.Close()) }()
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

func TestPersonalSQLiteUsageDashboardAggregatesAndExactRange(t *testing.T) {
	repo, closeRepo := openUsageSQLiteTest(t, "usage-dashboard")
	defer closeRepo()
	ctx := context.Background()
	start := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	logs := []*service.UsageLog{
		personalUsageLog("before-range", false),
		personalUsageLog("at-start", false),
		personalUsageLog("before-end", true),
		personalUsageLog("at-end", false),
	}
	logs[0].CreatedAt = start.Add(-time.Nanosecond)
	logs[1].CreatedAt = start
	logs[2].CreatedAt = end.Add(-time.Nanosecond)
	logs[3].CreatedAt = end
	duration1, duration2 := 100, 300
	logs[1].DurationMs = &duration1
	logs[2].DurationMs = &duration2
	for _, log := range logs {
		inserted, err := repo.Create(ctx, log)
		require.NoError(t, err)
		require.True(t, inserted)
	}

	stats, err := repo.GetStatsWithFilters(ctx, UsageLogFilters{StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.TotalRequests)
	require.Equal(t, int64(4), stats.TotalInputTokens)
	require.Equal(t, int64(6), stats.TotalOutputTokens)
	require.Equal(t, int64(10), stats.TotalTokens)
	require.Equal(t, float64(200), stats.AverageDurationMs)
	require.Equal(t, []EndpointStat{{Endpoint: "/v1/chat/completions", Requests: 2, TotalTokens: 10}}, stats.Endpoints)
	require.Equal(t, []EndpointStat{{Endpoint: "/v1internal:streamGenerateContent", Requests: 2, TotalTokens: 10}}, stats.UpstreamEndpoints)

	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "hour", UsageLogFilters{})
	require.NoError(t, err)
	require.Len(t, trend, 2)
	require.Equal(t, int64(2), trend[0].Requests+trend[1].Requests)
	require.Equal(t, int64(10), trend[0].TotalTokens+trend[1].TotalTokens)
	dayTrend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", UsageLogFilters{})
	require.NoError(t, err)
	require.Equal(t, stats.TotalRequests, dayTrend[0].Requests+dayTrend[1].Requests)

	models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, UsageLogFilters{}, "requested")
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, stats.TotalRequests, models[0].Requests)
}

func TestSQLiteTrendBucketUsesDashboardTimezone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	tests := []struct {
		name        string
		value       string
		granularity string
		location    *time.Location
		want        string
	}{
		{name: "hour in Shanghai", value: "2026-08-23T01:30:00Z", granularity: "hour", location: shanghai, want: "2026-08-23 09:00"},
		{name: "hour crosses local day", value: "2026-08-23T17:30:00Z", granularity: "hour", location: shanghai, want: "2026-08-24 01:00"},
		{name: "day crosses local day", value: "2026-08-23T17:30:00Z", granularity: "day", location: shanghai, want: "2026-08-24"},
		{name: "UTC remains UTC", value: "2026-08-23T01:30:00Z", granularity: "hour", location: time.UTC, want: "2026-08-23 01:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, parseErr := time.Parse(time.RFC3339, tt.value)
			require.NoError(t, parseErr)
			require.Equal(t, tt.want, sqliteTrendBucket(value, tt.granularity, tt.location))
		})
	}
}

func TestPersonalSQLiteGroupStatsExcludeUnifiedKeyUsage(t *testing.T) {
	repo, closeRepo := openUsageSQLiteTest(t, "usage-unified-group")
	defer closeRepo()
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	end := start.Add(2 * time.Hour)
	for index := 1; index <= 7; index++ {
		_, err := repo.client.Group.Create().
			SetName(fmt.Sprintf("group-%d", index)).
			SetPlatform(service.PlatformAntigravity).
			Save(ctx)
		require.NoError(t, err)
	}
	unified := personalUsageLog("unified-key", false)
	unified.GroupID = nil
	unified.CreatedAt = start.Add(time.Minute)
	groupID := int64(7)
	grouped1 := personalUsageLog("grouped-1", false)
	grouped1.GroupID = &groupID
	grouped1.CreatedAt = start.Add(2 * time.Minute)
	grouped2 := personalUsageLog("grouped-2", false)
	grouped2.GroupID = &groupID
	grouped2.CreatedAt = start.Add(3 * time.Minute)
	for _, log := range []*service.UsageLog{unified, grouped1, grouped2} {
		inserted, err := repo.Create(ctx, log)
		require.NoError(t, err)
		require.True(t, inserted)
	}

	groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, UsageLogFilters{})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, int64(7), groups[0].GroupID)
	require.Equal(t, "group-7", groups[0].GroupName)
	require.Equal(t, int64(2), groups[0].Requests)
	require.Equal(t, int64(10), groups[0].TotalTokens)
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

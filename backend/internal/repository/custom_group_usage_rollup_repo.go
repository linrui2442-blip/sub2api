package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// getAllGroupUsageSummaryFromRollups reads the authoritative Personal SQLite
// usage log directly. Commercial monetary rollup tables are not used.
func (r *usageLogRepository) getAllGroupUsageSummaryFromRollups(ctx context.Context, todayStart time.Time) (results []usagestats.GroupUsageSummary, err error) {
	todayStart = service.GroupUsageTodayStart(todayStart)
	yesterdayStart := service.GroupUsageYesterdayStart(todayStart)
	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			g.id,
			COALESCE(SUM(CASE WHEN ul.created_at >= $1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ul.created_at >= $1 THEN ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ul.created_at >= $2 AND ul.created_at < $1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN ul.created_at >= $2 AND ul.created_at < $1 THEN ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens ELSE 0 END), 0),
			COUNT(ul.id),
			COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0)
		FROM groups g
		LEFT JOIN usage_logs ul ON ul.group_id = g.id
		GROUP BY g.id
		ORDER BY g.id
	`, todayStart, yesterdayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row usagestats.GroupUsageSummary
		if err := rows.Scan(&row.GroupID, &row.TodayRequests, &row.TodayTokens, &row.YesterdayRequests, &row.YesterdayTokens, &row.TotalRequests, &row.TotalTokens); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

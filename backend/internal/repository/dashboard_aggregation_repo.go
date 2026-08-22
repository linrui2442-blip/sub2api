package repository

import (
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// NewDashboardAggregationRepository returns no pre-aggregation backend for the
// Personal SQLite runtime. Dashboard queries read the authoritative usage log
// directly, so PostgreSQL rollup tables and advisory locks are unnecessary.
func NewDashboardAggregationRepository(*sql.DB) service.DashboardAggregationRepository {
	return nil
}

package securityaudit

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSQLRepositoryRunsPromptAuditLifecycleOnSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", "file:prompt-audit-test?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE api_keys (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE groups (id INTEGER PRIMARY KEY)`,
	} {
		_, err = db.Exec(statement)
		require.NoError(t, err)
	}

	repository := NewSQLRepository(db)
	require.NoError(t, repository.initErr)
	result := &NormalizedResult{Decision: EventFlag, RiskLevel: RiskHigh, Action: ActionWarn}
	event, err := repository.RecordBlocking(context.Background(), PromptSnapshot{RequestID: "req-1", PromptHash: "hash-1"}, 1, result, false)
	require.NoError(t, err)
	require.NotNil(t, event)

	page, err := repository.ListEvents(context.Background(), EventFilter{RequestID: "req-1"}, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)

	deleted, err := repository.DeleteEvent(context.Background(), event.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted.DeletedEvents)
	require.Equal(t, int64(1), deleted.DeletedJobs)
}

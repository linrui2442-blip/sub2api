package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSQLiteAuditAndSystemLogBatchInsert(t *testing.T) {
	drv, db, err := openPersonalSQLite("file:sqlite_batch_insert?mode=memory&cache=shared")
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NoError(t, client.Schema.Create(context.Background()))
	require.NoError(t, ensurePersonalSQLiteInfrastructure(context.Background(), db))

	auditRepo := NewAuditLogRepository(db)
	inserted, err := auditRepo.BatchInsert(context.Background(), []*service.AuditLog{{
		ActorEmail: "owner@local", ActorRole: "owner", Action: "account.update",
		Method: "POST", Path: "/api/v1/admin/accounts/1", StatusCode: 200,
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	opsRepo := NewOpsRepository(db)
	inserted, err = opsRepo.BatchInsertSystemLogs(context.Background(), []*service.OpsInsertSystemLogInput{{
		Level: "info", Component: "gateway", Message: "request completed", Platform: "openai",
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	var auditCount, opsCount int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&auditCount))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM ops_system_logs").Scan(&opsCount))
	require.Equal(t, 1, auditCount)
	require.Equal(t, 1, opsCount)
}

package securityaudit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const promptAuditRedisTestEnv = "PROMPT_AUDIT_TEST_REDIS_ADDR"

func openPromptAuditIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prompt-audit.db"))
	require.NoError(t, err)
	_, err = db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT);
		CREATE TABLE groups (id INTEGER PRIMARY KEY AUTOINCREMENT);
		CREATE TABLE api_keys (id INTEGER PRIMARY KEY AUTOINCREMENT);
		CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
	`)
	require.NoError(t, err)
	require.NoError(t, ensurePromptAuditSQLiteSchema(context.Background(), db))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func promptAuditUpdateRequest(version int64, workerCount int, token string) UpdateConfigRequest {
	return UpdateConfigRequest{
		ExpectedConfigVersion: version, Enabled: true, BlockingEnabled: false,
		StorePassEvents: false, Strategy: "priority", WorkerCount: workerCount,
		QueueCapacity: 64, Scanners: []string{"pii", "jailbreak"}, AllGroups: true,
		Endpoints: []UpdateEndpoint{{
			ID: "guard-one", Name: "Guard One", Protocol: "openai_compatible",
			BaseURL: "http://127.0.0.1:18080", Token: token, TimeoutMS: 1000,
			InputLimit: 1024, Enabled: true,
		}},
	}
}

func integrationResult(decision EventDecision) *NormalizedResult {
	result := &NormalizedResult{
		Decision: decision, RiskLevel: RiskLow, Action: ActionAllow, Safety: "Safe",
		Categories: []string{}, MatchedScanners: []string{}, ScannerScores: map[string]float64{},
		ScannerEvidence: map[string]string{}, ScannerBackend: "qwen3guard-openai",
		ScannerVersion: "test", GuardEndpointID: "guard-1", PolicyID: "priority",
		PolicyVersion: 1, ChunkTotal: 1, LatencyMS: 2,
	}
	if decision != EventPass {
		result.RiskLevel = RiskCritical
		result.Action = ActionBlock
		result.Safety = "Unsafe"
		result.Categories = []string{"pii"}
		result.MatchedScanners = []string{"pii"}
		result.ScannerScores["pii"] = 1
		result.ScannerEvidence["pii"] = "redacted evidence"
	}
	return result
}

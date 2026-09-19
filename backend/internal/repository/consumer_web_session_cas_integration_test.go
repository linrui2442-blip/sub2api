package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type lifecycleCASFakeTransport struct {
	mu             sync.Mutex
	boots, rotates int
	started        chan struct{}
	release        chan struct{}
	session        struct{}
}

func (f *lifecycleCASFakeTransport) Bootstrap(context.Context, *service.Account, map[string]any, any) (service.ConsumerWebBootstrapResult, error) {
	f.mu.Lock()
	f.boots++
	f.mu.Unlock()
	return service.ConsumerWebBootstrapResult{Authenticated: true, Session: &f.session, Registry: []service.ConsumerWebModel{{ID: "old-registry"}}}, nil
}
func (f *lifecycleCASFakeTransport) RotatePSIDTS(ctx context.Context, _ *service.Account, _ service.ConsumerWebSessionHandle) (service.ConsumerWebRotationResult, error) {
	f.mu.Lock()
	f.rotates++
	f.mu.Unlock()
	close(f.started)
	select {
	case <-f.release:
	case <-ctx.Done():
		return service.ConsumerWebRotationResult{}, ctx.Err()
	}
	return service.ConsumerWebRotationResult{Authenticated: true, PSIDTS: "stale-rotated-psidts", Session: &f.session}, nil
}
func (f *lifecycleCASFakeTransport) Close(context.Context, service.ConsumerWebSessionHandle) error {
	return nil
}

type lifecycleCASFakeEncryptor struct{}

func (lifecycleCASFakeEncryptor) Encrypt(v string) (string, error) { return "cipher:" + v, nil }
func (lifecycleCASFakeEncryptor) Decrypt(v string) (string, error) { return v, nil }

func TestConsumerWebSessionRealSQLiteCASStaleRotation(t *testing.T) {
	path := t.TempDir() + "/lifecycle.db"
	drv, sqlDB, err := openPersonalSQLite(path)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	_, err = sqlDB.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox (id INTEGER PRIMARY KEY AUTOINCREMENT, event_type TEXT NOT NULL, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = sqlDB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_test_scheduler_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close(); _ = sqlDB.Close() })
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := newAccountRepositoryWithSQL(client, sqlDB, nil)
	account := &service.Account{ID: 701, Name: "synthetic", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "synthetic", "unknown_top": "keep", "consumer_web": map[string]any{service.ConsumerWebPSIDCiphertextKey: "cipher:psid-a", service.ConsumerWebPSIDTSCiphertextKey: "cipher:psidts-a", "unknown_nested": "keep"}}}
	require.NoError(t, repo.Create(context.Background(), account))
	tr := &lifecycleCASFakeTransport{started: make(chan struct{}), release: make(chan struct{})}
	m := service.NewConsumerWebSessionManager(service.ConsumerWebSessionManagerOptions{Transport: tr, Credentials: repo, Persister: service.NewConditionalConsumerWebPSIDTSPersister(repo, lifecycleCASFakeEncryptor{}), RefreshInterval: time.Millisecond})
	t.Cleanup(func() { _ = m.Close() })
	_, err = m.AcquireSession(context.Background(), account)
	require.NoError(t, err)
	select {
	case <-tr.started:
	case <-time.After(2 * time.Second):
		t.Fatal("rotation did not start")
	}
	_, err = service.MutateAccountCredentialsReplaySafe(context.Background(), repo, account.ID, func(current map[string]any) (map[string]any, error) {
		return service.UpdateAccountCredentialNamespace(current, service.ConsumerWebCredentialNamespace, map[string]any{service.ConsumerWebPSIDCiphertextKey: "cipher:psid-b"}), nil
	})
	require.NoError(t, err)
	close(tr.release)
	time.Sleep(30 * time.Millisecond)
	snapshot, err := repo.LoadCredentialSnapshot(context.Background(), account.ID)
	require.NoError(t, err)
	ns := snapshot.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "cipher:psid-b", ns[service.ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "cipher:psidts-a", ns[service.ConsumerWebPSIDTSCiphertextKey])
	require.Equal(t, "keep", snapshot.Credentials["unknown_top"])
	require.Equal(t, "keep", ns["unknown_nested"])
	require.Equal(t, service.ConsumerWebSessionEmpty, m.State(account.ID))
	tr.mu.Lock()
	require.Equal(t, 1, tr.boots)
	require.Equal(t, 1, tr.rotates)
	tr.mu.Unlock()
}

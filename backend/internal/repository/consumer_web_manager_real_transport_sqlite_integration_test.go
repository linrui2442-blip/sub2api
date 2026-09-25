package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type localConsumerWebUpstream struct{}

func (localConsumerWebUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return http.DefaultClient.Do(r)
}
func (localConsumerWebUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return localConsumerWebUpstream{}.Do(r, p, id, n)
}

type localConsumerWebClock struct{}

func (localConsumerWebClock) Now() time.Time { return time.Now() }
func (localConsumerWebClock) AfterFunc(d time.Duration, f func()) service.ConsumerWebTimer {
	return localTimer{time.AfterFunc(d, f)}
}

type localTimer struct{ t *time.Timer }

func (t localTimer) Stop() bool { return t.t.Stop() }

func TestConsumerWebRealTransportManagerTempSQLiteCASAndInvalidation(t *testing.T) {
	var appHits, otaqHits, rotateHits atomic.Int32
	var marker atomic.Value
	marker.Store("A")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app":
			appHits.Add(1)
			w.Header().Add("Set-Cookie", "runtime="+marker.Load().(string)+"; Path=/")
			_, _ = w.Write([]byte(`"SNlM0e":"synthetic-token" "qKIAYe":"push" "cfb2h":"build" "FdrFJe":"sid" "TuX5cc":"en"`))
		case "/otaq":
			otaqHits.Add(1)
			inner := make([]any, 16)
			inner[14] = nil
			inner[15] = []any{map[string]any{"id": "synthetic-model", "display": "Synthetic", "tier": "pro"}}
			b, _ := json.Marshal(inner)
			frame, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
			_, _ = w.Write(append([]byte(")]}'\n"), append(frame, '\n')...))
		case "/rotate":
			rotateHits.Add(1)
			w.Header().Add("Set-Cookie", "__Secure-1PSIDTS=PSIDTS-B; Path=/")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	path := t.TempDir() + "/integration.db"
	drv, sqlDB, err := openPersonalSQLite(path)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	_, err = sqlDB.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox (id INTEGER PRIMARY KEY AUTOINCREMENT, event_type TEXT NOT NULL, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = sqlDB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_test_scheduler_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close(); _ = sqlDB.Close() })
	repo := newAccountRepositoryWithSQL(client, sqlDB, nil)
	enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	psid, _ := enc.Encrypt("PSID-A")
	psidts, _ := enc.Encrypt("PSIDTS-A")
	account := &service.Account{Name: "synthetic", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"unknown_top": "keep", service.ConsumerWebCredentialNamespace: map[string]any{service.ConsumerWebPSIDCiphertextKey: psid, service.ConsumerWebPSIDTSCiphertextKey: psidts, "unknown_nested": "keep"}}}
	require.NoError(t, repo.Create(context.Background(), account))
	require.NotZero(t, account.ID)
	tr := service.NewConsumerWebHTTPTransport(service.ConsumerWebHTTPTransportOptions{HTTPUpstream: localConsumerWebUpstream{}, Encryptor: enc, TestEndpoints: &service.ConsumerWebEndpointSet{App: server.URL + "/app", OTAQ: server.URL + "/otaq", Rotate: server.URL + "/rotate"}})
	m := service.NewConsumerWebSessionManager(service.ConsumerWebSessionManagerOptions{Transport: tr, Credentials: repo, Persister: service.NewConditionalConsumerWebPSIDTSPersister(repo, enc), Clock: localConsumerWebClock{}, RefreshInterval: 500 * time.Millisecond})
	t.Cleanup(func() { _ = m.Close() })
	_, err = m.AcquireSession(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, service.ConsumerWebSessionAuthenticated, m.State(account.ID))
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		snap, e := repo.LoadCredentialSnapshot(context.Background(), account.ID)
		require.NoError(t, e)
		ns := snap.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
		value, _ := enc.Decrypt(ns[service.ConsumerWebPSIDTSCiphertextKey].(string))
		if value == "PSIDTS-B" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("state=%s app=%d otaq=%d rotate=%d", m.State(account.ID), appHits.Load(), otaqHits.Load(), rotateHits.Load())
	snap, err := repo.LoadCredentialSnapshot(context.Background(), account.ID)
	require.NoError(t, err)
	ns := snap.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
	got, _ := enc.Decrypt(ns[service.ConsumerWebPSIDTSCiphertextKey].(string))
	require.Equal(t, "PSIDTS-B", got)
	require.Equal(t, "keep", ns["unknown_nested"])
	require.Equal(t, "keep", snap.Credentials["unknown_top"])
	require.Equal(t, service.ConsumerWebSessionAuthenticated, m.State(account.ID))
	require.GreaterOrEqual(t, appHits.Load(), int32(1))
	require.GreaterOrEqual(t, otaqHits.Load(), int32(1))
	require.GreaterOrEqual(t, rotateHits.Load(), int32(1))
	m.InvalidateAccount(account.ID)
	require.Equal(t, service.ConsumerWebSessionEmpty, m.State(account.ID))
	marker.Store("B")
	_, err = m.AcquireSession(context.Background(), account)
	require.NoError(t, err)
	require.GreaterOrEqual(t, appHits.Load(), int32(2))
}

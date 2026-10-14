package repository

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConsumerWebAuxiliarySQLiteRestartAndCAS(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/aux.db"
	enc, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	var repo *accountRepository
	var client *ent.Client
	open := func() {
		drv, db, e := openPersonalSQLite(path)
		require.NoError(t, e)
		client = ent.NewClient(ent.Driver(drv))
		require.NoError(t, client.Schema.Create(ctx))
		_, e = db.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox (id INTEGER PRIMARY KEY AUTOINCREMENT,event_type TEXT NOT NULL,account_id INTEGER,group_id INTEGER,payload BLOB,dedup_key TEXT,created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
		require.NoError(t, e)
		_, e = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_aux_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
		require.NoError(t, e)
		repo = newAccountRepositoryWithSQL(client, db, nil)
	}
	open()
	defer func() { _ = client.Close() }()
	p, _ := enc.Encrypt("core-p")
	ts, _ := enc.Encrypt("core-ts")
	a := &service.Account{Name: "aux-test", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{service.ConsumerWebCredentialNamespace: map[string]any{service.ConsumerWebPSIDCiphertextKey: p, service.ConsumerWebPSIDTSCiphertextKey: ts}}}
	require.NoError(t, repo.Create(ctx, a))
	phase := 0
	restored := false
	var winner map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app" {
			if phase > 0 {
				c, e := r.Cookie("aux_test")
				restored = e == nil && c.Value == "aux-secret"
			}
			if phase == 2 {
				snap, e := repo.LoadCredentialSnapshot(ctx, a.ID)
				if e != nil {
					t.Error(e)
					return
				}
				winner = snap.Credentials
				_, e = repo.CompareAndSwapCredentials(ctx, a.ID, snap.Revision, service.UpdateAccountCredentialNamespace(snap.Credentials, service.ConsumerWebCredentialNamespace, map[string]any{"concurrent_marker": "newer"}))
				if e != nil {
					t.Error(e)
					return
				}
			}
			value := "aux-secret"
			if phase == 2 {
				value = "stale-secret"
			}
			w.Header().Set("Set-Cookie", "aux_test="+value+"; Path=/; Max-Age=3600")
			_, _ = w.Write([]byte(`"SNlM0e":"synthetic-token" "cfb2h":"build" "FdrFJe":"sid"`))
			return
		}
		inner := make([]any, 16)
		inner[15] = []any{map[string]any{"id": "synthetic", "display": "Synthetic", "tier": "pro"}}
		b, _ := json.Marshal(inner)
		frame, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
		_, _ = w.Write(append([]byte(")]}'\n"), append(frame, '\n')...))
	}))
	defer server.Close()
	boot := func() {
		tr := service.NewConsumerWebHTTPTransport(service.ConsumerWebHTTPTransportOptions{HTTPUpstream: localConsumerWebUpstream{}, Encryptor: enc, TestEndpoints: &service.ConsumerWebEndpointSet{App: server.URL + "/app", OTAQ: server.URL + "/otaq", Rotate: server.URL + "/rotate"}})
		m := service.NewConsumerWebSessionManager(service.ConsumerWebSessionManagerOptions{Transport: tr, Credentials: repo, Persister: service.NewConditionalConsumerWebPSIDTSPersister(repo, enc), RefreshInterval: time.Hour})
		defer m.Close()
		_, e := m.AcquireSession(ctx, a)
		require.NoError(t, e)
	}
	boot()
	snap, err := repo.LoadCredentialSnapshot(ctx, a.ID)
	require.NoError(t, err)
	ns := snap.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, p, ns[service.ConsumerWebPSIDCiphertextKey])
	require.Equal(t, ts, ns[service.ConsumerWebPSIDTSCiphertextKey])
	b, _ := json.Marshal(snap.Credentials)
	require.NotContains(t, string(b), "aux-secret")
	require.NotNil(t, ns["auxiliary_cookie_snapshot"])
	require.NoError(t, client.Close())
	open()
	phase = 1
	boot()
	require.True(t, restored)
	phase = 2
	boot()
	after, err := repo.LoadCredentialSnapshot(ctx, a.ID)
	require.NoError(t, err)
	final := after.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "newer", final["concurrent_marker"])
	require.Equal(t, winner[service.ConsumerWebCredentialNamespace].(map[string]any)["auxiliary_cookie_snapshot"], final["auxiliary_cookie_snapshot"])
}

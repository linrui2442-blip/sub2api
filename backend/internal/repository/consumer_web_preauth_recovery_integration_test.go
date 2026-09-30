package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type preAuthBudgetGuard struct {
	mu     sync.Mutex
	counts map[string]int
}

func (g *preAuthBudgetGuard) BeforeRoundTrip(role string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.counts == nil {
		g.counts = map[string]int{}
	}
	if g.counts[role] >= 1 {
		return service.ErrConsumerWebTransportTransient
	}
	g.counts[role]++
	return nil
}
func (g *preAuthBudgetGuard) count(role string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.counts[role]
}

type preAuthCASConflictPersister struct {
	repo  service.AtomicAccountCredentialsRepository
	inner service.ConditionalConsumerWebPSIDTSPersister
}

func (p preAuthCASConflictPersister) PersistPSIDTS(ctx context.Context, id int64, revision any, value string) error {
	snapshot, err := p.repo.LoadCredentialSnapshot(ctx, id)
	if err != nil {
		return err
	}
	snapshot.Credentials["admin_fixture_marker"] = "newer"
	if _, err = p.repo.CompareAndSwapCredentials(ctx, id, snapshot.Revision, snapshot.Credentials); err != nil {
		return err
	}
	return p.inner.PersistPSIDTS(ctx, id, revision, value)
}

func TestConsumerWebPreAuthRecoveryTempSQLite(t *testing.T) {
	t.Setenv(personal.EnvEnabled, "1")
	t.Setenv("RUN_MODE", "simple")
	for _, tc := range []struct {
		name              string
		replacement       bool
		rotateStatus      int
		secondToken       bool
		transportFailure  bool
		casConflict       bool
		wantAuthenticated bool
		wantApp           int32
		wantOTAQ          int32
	}{
		{name: "replacement", replacement: true, rotateStatus: 200, secondToken: true, wantAuthenticated: true, wantApp: 2, wantOTAQ: 1},
		{name: "no_replacement", rotateStatus: 200, secondToken: true, wantAuthenticated: true, wantApp: 2, wantOTAQ: 1},
		{name: "non200", rotateStatus: 403, secondToken: true, wantApp: 1},
		{name: "transport_failure", rotateStatus: 200, transportFailure: true, secondToken: true, wantApp: 1},
		{name: "cas_conflict", replacement: true, rotateStatus: 200, casConflict: true, secondToken: true, wantApp: 1},
		{name: "second_app_missing", rotateStatus: 200, wantApp: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var appHits, rotateHits, otaqHits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/app":
					if appHits.Add(1) == 2 && tc.secondToken {
						_, _ = w.Write([]byte(`"SNlM0e":"fixture-token"`))
					} else {
						_, _ = w.Write([]byte(`<html>guest</html>`))
					}
				case "/rotate":
					rotateHits.Add(1)
					if tc.transportFailure {
						hijacker := w.(http.Hijacker)
						conn, _, err := hijacker.Hijack()
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					if tc.rotateStatus != 200 {
						w.WriteHeader(tc.rotateStatus)
						return
					}
					if tc.replacement {
						w.Header().Add("Set-Cookie", "__Secure-1PSIDTS=replacement; Path=/")
					}
					w.WriteHeader(200)
				case "/otaq":
					otaqHits.Add(1)
					inner := make([]any, 16)
					inner[14] = nil
					payload, _ := json.Marshal(inner)
					outer, _ := json.Marshal([]any{[]any{"wrb.fr", "otAQ7b", string(payload)}})
					_, _ = w.Write([]byte(")]}'\n" + strconv.Itoa(len(outer)) + "\n" + string(outer) + "\n"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			path := t.TempDir() + "/recovery.db"
			drv, db, err := openPersonalSQLite(path)
			require.NoError(t, err)
			client := ent.NewClient(ent.Driver(drv))
			require.NoError(t, client.Schema.Create(context.Background()))
			_, err = db.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox (id INTEGER PRIMARY KEY AUTOINCREMENT, event_type TEXT NOT NULL, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
			require.NoError(t, err)
			_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_recovery_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close(); _ = db.Close() })
			repo := NewPersonalAwareAccountRepository(client, db, nil)
			cfg := &config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}}
			enc, err := NewAESEncryptor(cfg)
			require.NoError(t, err)
			psid, _ := enc.Encrypt("synthetic-psid")
			psidts, _ := enc.Encrypt("synthetic-psidts")
			account := &service.Account{Name: "synthetic", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{service.ConsumerWebCredentialNamespace: map[string]any{service.ConsumerWebPSIDCiphertextKey: psid, service.ConsumerWebPSIDTSCiphertextKey: psidts}}}
			require.NoError(t, repo.Create(context.Background(), account))
			gateway := service.NewGatewayService(repo, nil, nil, nil, nil, cfg, nil, nil, nil, nil, NewHTTPUpstream(cfg), nil, nil, nil, nil, nil, nil, nil, nil, nil)
			selectionCtx := context.WithValue(context.Background(), ctxkey.ForcePlatform, service.PlatformAntigravity)
			selected, err := gateway.SelectAccountWithLoadAwareness(selectionCtx, nil, "h2-local", "gemini-3.1-pro-high", nil, "", 0)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, account.ID, selected.Account.ID)
			if selected.ReleaseFunc != nil {
				defer selected.ReleaseFunc()
			}
			transport := service.NewConsumerWebHTTPTransport(service.ConsumerWebHTTPTransportOptions{HTTPUpstream: NewHTTPUpstream(cfg), Encryptor: enc, TestEndpoints: &service.ConsumerWebEndpointSet{App: srv.URL + "/app", OTAQ: srv.URL + "/otaq", Rotate: srv.URL + "/rotate"}})
			var persister service.ConditionalConsumerWebPSIDTSPersister = service.NewConditionalConsumerWebPSIDTSPersister(repo, enc)
			if tc.casConflict {
				persister = preAuthCASConflictPersister{repo: repo.(service.AtomicAccountCredentialsRepository), inner: persister}
			}
			manager := service.NewConsumerWebSessionManager(service.ConsumerWebSessionManagerOptions{Transport: transport, Credentials: repo.(service.AtomicAccountCredentialsRepository), Persister: persister, RefreshInterval: time.Hour})
			defer manager.Close()
			guard := &preAuthBudgetGuard{}
			_, err = manager.AcquireSession(service.WithHTTPUpstreamDispatchGuard(context.Background(), guard), selected.Account)
			require.Equal(t, tc.wantAuthenticated, err == nil)
			require.Equal(t, tc.wantApp, appHits.Load())
			require.Equal(t, int32(1), rotateHits.Load())
			require.Equal(t, tc.wantOTAQ, otaqHits.Load())
			require.Equal(t, 1, guard.count("APP"))
			require.Equal(t, 1, guard.count("PREAUTH_ROTATE"))
			if tc.wantApp == 2 {
				require.Equal(t, 1, guard.count("RETRY_APP"))
			}
			if tc.wantOTAQ == 1 {
				require.Equal(t, 1, guard.count("OTAQ"))
			}
			if tc.name == "replacement" {
				for _, role := range []string{"APP", "PREAUTH_ROTATE", "RETRY_APP", "OTAQ"} {
					require.Error(t, guard.BeforeRoundTrip(role))
					require.Equal(t, 1, guard.count(role))
				}
			}
			snap, err := repo.(service.AtomicAccountCredentialsRepository).LoadCredentialSnapshot(context.Background(), account.ID)
			require.NoError(t, err)
			ns := snap.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
			got, err := enc.Decrypt(ns[service.ConsumerWebPSIDTSCiphertextKey].(string))
			require.NoError(t, err)
			if tc.replacement && !tc.casConflict {
				require.Equal(t, "replacement", got)
			} else {
				require.Equal(t, "synthetic-psidts", got)
			}
			if tc.casConflict {
				require.Equal(t, "newer", snap.Credentials["admin_fixture_marker"])
			}
		})
	}
}

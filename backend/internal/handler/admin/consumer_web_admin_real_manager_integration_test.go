package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type adminLocalConsumerWebUpstream struct{}

func (adminLocalConsumerWebUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return http.DefaultClient.Do(r)
}
func (adminLocalConsumerWebUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return http.DefaultClient.Do(r)
}

type adminManagerInvalidationRecorder struct {
	delegate     service.ConsumerWebSessionInvalidator
	calls        atomic.Int32
	durable      *atomic.Bool
	afterDurable atomic.Bool
}

func (r *adminManagerInvalidationRecorder) InvalidateAccount(id int64) {
	r.calls.Add(1)
	if r.durable != nil && r.durable.Load() {
		r.afterDurable.Store(true)
	}
	r.delegate.InvalidateAccount(id)
}

// TestConsumerWebAdminReplacementInvalidatesRealManagerAndReacquires proves
// the production admin credential mutation path durably replaces credentials,
// invalidates the same real manager instance, and makes the next acquisition
// bootstrap a new session from the latest SQLite snapshot.
func TestConsumerWebAdminReplacementInvalidatesRealManagerAndReacquires(t *testing.T) {
	ctx := context.Background()
	var appHits, otaqHits atomic.Int32
	var marker atomic.Value
	marker.Store("A")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app":
			appHits.Add(1)
			_, _ = w.Write([]byte(`"SNlM0e":"synthetic-token" "qKIAYe":"push" "cfb2h":"build" "FdrFJe":"sid" "TuX5cc":"en"`))
		case "/otaq":
			otaqHits.Add(1)
			inner := make([]any, 16)
			inner[15] = []any{map[string]any{"id": "synthetic-model", "display": "Synthetic", "tier": "pro"}}
			b, _ := json.Marshal(inner)
			frame, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(b)})
			_, _ = w.Write(append([]byte(")]}'\n"), append(frame, '\n')...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/admin-manager.db?_fk=1")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA busy_timeout = 5000")
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(ctx))
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox (id INTEGER PRIMARY KEY AUTOINCREMENT, event_type TEXT NOT NULL, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_test_scheduler_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)
	defer func() { _ = client.Close(); _ = db.Close() }()
	repo := repository.NewAccountRepository(client, db, nil)
	atomicRepo, ok := service.AtomicCredentialsRepository(repo)
	require.True(t, ok)
	enc, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	psidA, _ := enc.Encrypt("PSID-A")
	psidtsA, _ := enc.Encrypt("PSIDTS-A")
	account := &service.Account{Name: "synthetic", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"oauth_marker": "keep", service.ConsumerWebCredentialNamespace: map[string]any{service.ConsumerWebPSIDCiphertextKey: psidA, service.ConsumerWebPSIDTSCiphertextKey: psidtsA}}}
	require.NoError(t, repo.Create(ctx, account))

	tr := service.NewConsumerWebHTTPTransport(service.ConsumerWebHTTPTransportOptions{HTTPUpstream: adminLocalConsumerWebUpstream{}, Encryptor: enc, TestEndpoints: &service.ConsumerWebEndpointSet{App: server.URL + "/app", OTAQ: server.URL + "/otaq", Rotate: server.URL + "/rotate"}})
	m := service.NewConsumerWebSessionManager(service.ConsumerWebSessionManagerOptions{Transport: tr, Credentials: atomicRepo, Persister: service.NewConditionalConsumerWebPSIDTSPersister(repo, enc), RefreshInterval: time.Hour})
	defer m.Close()
	g1, err := m.AcquireSession(ctx, account)
	require.NoError(t, err)
	require.NotNil(t, g1)
	appHitsAfterG1, otaqHitsAfterG1 := appHits.Load(), otaqHits.Load()
	require.Equal(t, service.ConsumerWebSessionAuthenticated, m.State(account.ID))

	svc := newStubAdminService()
	var durableCommitted atomic.Bool
	svc.getAccountFn = func(ctx context.Context, id int64) (*service.Account, error) { return repo.GetByID(ctx, id) }
	svc.updateAccountFn = func(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
		current, err := repo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		current.Credentials = input.Credentials
		if err := repo.Update(ctx, current); err != nil {
			return nil, err
		}
		updater, ok := repo.(interface {
			UpdateCredentials(context.Context, int64, map[string]any) error
		})
		require.True(t, ok)
		if err := updater.UpdateCredentials(ctx, id, input.Credentials); err != nil {
			return nil, err
		}
		durableCommitted.Store(true)
		return repo.GetByID(ctx, id)
	}
	recorder := &adminManagerInvalidationRecorder{delegate: m, durable: &durableCommitted}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetConsumerWebCredentialEncryptor(enc)
	h.SetConsumerWebSessionInvalidator(recorder)

	marker.Store("B")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/"+strconvFormat(account.ID)+"/consumer-web-credentials", bytes.NewBufferString(`{"secure_1psid":"PSID-B","secure_1psidts":"PSIDTS-B"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconvFormat(account.ID)}}
	h.UpdateConsumerWebCredentials(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int32(1), recorder.calls.Load())
	require.True(t, recorder.afterDurable.Load())
	require.Equal(t, service.ConsumerWebSessionEmpty, m.State(account.ID))

	snap, err := atomicRepo.LoadCredentialSnapshot(ctx, account.ID)
	require.NoError(t, err)
	ns := snap.Credentials[service.ConsumerWebCredentialNamespace].(map[string]any)
	gotPSID, _ := enc.Decrypt(ns[service.ConsumerWebPSIDCiphertextKey].(string))
	gotPSIDTS, _ := enc.Decrypt(ns[service.ConsumerWebPSIDTSCiphertextKey].(string))
	require.Equal(t, "PSID-B", gotPSID)
	require.Equal(t, "PSIDTS-B", gotPSIDTS)
	require.Equal(t, "keep", snap.Credentials["oauth_marker"])

	g2, err := m.AcquireSession(ctx, account)
	require.NoError(t, err)
	require.Equal(t, service.ConsumerWebSessionAuthenticated, m.State(account.ID))
	require.NotSame(t, g1, g2)
	require.GreaterOrEqual(t, appHits.Load(), int32(2))
	require.GreaterOrEqual(t, otaqHits.Load(), int32(2))
	t.Logf("admin replacement durable; manager invalidations=%d app=%d otaq=%d g1_app=%d g1_otaq=%d", recorder.calls.Load(), appHits.Load(), otaqHits.Load(), appHitsAfterG1, otaqHitsAfterG1)
}

func strconvFormat(id int64) string {
	if id == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", id)
}

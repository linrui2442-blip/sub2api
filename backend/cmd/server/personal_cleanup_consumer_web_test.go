package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type cleanupLocalHTTPUpstream struct{}

func (cleanupLocalHTTPUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return http.DefaultClient.Do(r)
}
func (cleanupLocalHTTPUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return cleanupLocalHTTPUpstream{}.Do(r, p, id, n)
}

type cleanupAtomicRepository struct {
	delegate service.AtomicAccountCredentialsRepository
	loads    atomic.Int32
}

func (r *cleanupAtomicRepository) LoadCredentialSnapshot(ctx context.Context, id int64) (*service.AccountCredentialSnapshot, error) {
	r.loads.Add(1)
	return r.delegate.LoadCredentialSnapshot(ctx, id)
}
func (r *cleanupAtomicRepository) CompareAndSwapCredentials(ctx context.Context, id int64, revision any, credentials map[string]any) (*service.AccountCredentialSnapshot, error) {
	return r.delegate.CompareAndSwapCredentials(ctx, id, revision, credentials)
}

type cleanupRecordingTransport struct {
	delegate          service.ConsumerWebSessionTransport
	db                *sql.DB
	closeCalls        atomic.Int32
	dbOpenDuringClose atomic.Bool
}

func (t *cleanupRecordingTransport) Bootstrap(ctx context.Context, account *service.Account, credentials map[string]any, revision any) (service.ConsumerWebBootstrapResult, error) {
	return t.delegate.Bootstrap(ctx, account, credentials, revision)
}
func (t *cleanupRecordingTransport) RotatePSIDTS(ctx context.Context, account *service.Account, handle service.ConsumerWebSessionHandle) (service.ConsumerWebRotationResult, error) {
	return t.delegate.RotatePSIDTS(ctx, account, handle)
}
func (t *cleanupRecordingTransport) Close(ctx context.Context, handle service.ConsumerWebSessionHandle) error {
	t.closeCalls.Add(1)
	if t.db != nil && t.db.PingContext(ctx) == nil {
		t.dbOpenDuringClose.Store(true)
	}
	return t.delegate.Close(ctx, handle)
}

type cleanupManualClock struct {
	mu     sync.Mutex
	timers []*cleanupManualTimer
}

type cleanupManualTimer struct {
	mu      sync.Mutex
	stopped bool
	fn      func()
}

func (*cleanupManualClock) Now() time.Time { return time.Now() }
func (c *cleanupManualClock) AfterFunc(_ time.Duration, fn func()) service.ConsumerWebTimer {
	timer := &cleanupManualTimer{fn: fn}
	c.mu.Lock()
	c.timers = append(c.timers, timer)
	c.mu.Unlock()
	return timer
}
func (t *cleanupManualTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}
func (c *cleanupManualClock) fireAll() {
	c.mu.Lock()
	timers := append([]*cleanupManualTimer(nil), c.timers...)
	c.mu.Unlock()
	for _, timer := range timers {
		timer.mu.Lock()
		if timer.stopped {
			timer.mu.Unlock()
			continue
		}
		fn := timer.fn
		timer.mu.Unlock()
		fn()
	}
}

func TestPersonalCleanupClosesActiveConsumerWebManagerBeforeDatabase(t *testing.T) {
	ctx := context.Background()
	var appHits, otaqHits, rotateHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app":
			appHits.Add(1)
			_, _ = w.Write([]byte(`"SNlM0e":"synthetic-token" "qKIAYe":"push" "cfb2h":"build" "FdrFJe":"sid" "TuX5cc":"en"`))
		case "/otaq":
			otaqHits.Add(1)
			inner := make([]any, 16)
			inner[15] = []any{map[string]any{"id": "synthetic-model", "display": "Synthetic", "tier": "pro"}}
			payload, _ := json.Marshal(inner)
			frame, _ := json.Marshal([]any{"wrb.fr", "otAQ7b", string(payload)})
			_, _ = w.Write(append([]byte(")]}'\n"), append(frame, '\n')...))
		case "/rotate":
			rotateHits.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/cleanup.db?_fk=1")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	driver := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(driver))
	require.NoError(t, client.Schema.Create(ctx))
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS scheduler_outbox (id INTEGER PRIMARY KEY AUTOINCREMENT, event_type TEXT NOT NULL, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_cleanup_scheduler_outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`)
	require.NoError(t, err)

	repo := repository.NewAccountRepository(client, db, nil)
	atomicRepo, ok := service.AtomicCredentialsRepository(repo)
	require.True(t, ok)
	countingRepo := &cleanupAtomicRepository{delegate: atomicRepo}
	encryptor, err := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	psid, _ := encryptor.Encrypt("synthetic-psid")
	psidts, _ := encryptor.Encrypt("synthetic-psidts")
	account := &service.Account{Name: "shutdown-test", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{service.ConsumerWebCredentialNamespace: map[string]any{service.ConsumerWebPSIDCiphertextKey: psid, service.ConsumerWebPSIDTSCiphertextKey: psidts}}}
	require.NoError(t, repo.Create(ctx, account))

	realTransport := service.NewConsumerWebHTTPTransport(service.ConsumerWebHTTPTransportOptions{HTTPUpstream: cleanupLocalHTTPUpstream{}, Encryptor: encryptor, TestEndpoints: &service.ConsumerWebEndpointSet{App: server.URL + "/app", OTAQ: server.URL + "/otaq", Rotate: server.URL + "/rotate"}})
	recordingTransport := &cleanupRecordingTransport{delegate: realTransport, db: db}
	clock := &cleanupManualClock{}
	manager := service.NewConsumerWebSessionManager(service.ConsumerWebSessionManagerOptions{Transport: recordingTransport, Credentials: countingRepo, Persister: service.NewConditionalConsumerWebPSIDTSPersister(repo, encryptor), Clock: clock, RefreshInterval: time.Minute})

	session, err := manager.AcquireSession(ctx, account)
	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, service.ConsumerWebSessionAuthenticated, manager.State(account.ID))
	beforeApp, beforeOTAQ, beforeRotate := appHits.Load(), otaqHits.Load(), rotateHits.Load()
	beforeLoads := countingRepo.loads.Load()

	cleanup := providePersonalCleanup(client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, manager)
	require.NotNil(t, cleanup)
	cleanup()

	require.Equal(t, service.ConsumerWebSessionClosed, manager.State(account.ID))
	require.Equal(t, int32(1), recordingTransport.closeCalls.Load())
	require.True(t, recordingTransport.dbOpenDuringClose.Load())
	require.Error(t, db.Ping())

	clock.fireAll()
	require.Equal(t, beforeApp, appHits.Load())
	require.Equal(t, beforeOTAQ, otaqHits.Load())
	require.Equal(t, beforeRotate, rotateHits.Load())

	_, err = manager.AcquireSession(ctx, account)
	require.ErrorIs(t, err, service.ErrConsumerWebManagerClosed)
	require.Equal(t, beforeLoads, countingRepo.loads.Load())
	require.Equal(t, beforeApp, appHits.Load())
	require.Equal(t, beforeOTAQ, otaqHits.Load())
	require.Equal(t, beforeRotate, rotateHits.Load())

	require.NoError(t, manager.Close())
	require.Equal(t, int32(1), recordingTransport.closeCalls.Load())
}

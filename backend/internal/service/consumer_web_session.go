package service

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"time"
)

var (
	ErrConsumerWebManagerClosed   = errors.New("consumer web session manager is closed")
	ErrConsumerWebAuthUnavailable = errors.New("consumer web authentication unavailable")
	ErrConsumerWebPersistence     = errors.New("consumer web session persistence failed")
)

type ConsumerWebLifecycleErrorKind string

const (
	ConsumerWebTransient          ConsumerWebLifecycleErrorKind = "TRANSIENT"
	ConsumerWebAuthLost           ConsumerWebLifecycleErrorKind = "AUTH_LOST"
	ConsumerWebCredentialStale    ConsumerWebLifecycleErrorKind = "CREDENTIAL_STALE"
	ConsumerWebPersistenceFailure ConsumerWebLifecycleErrorKind = "PERSISTENCE_FAILURE"
	ConsumerWebManagerClosed      ConsumerWebLifecycleErrorKind = "MANAGER_CLOSED"
)

type ConsumerWebLifecycleError struct {
	Kind ConsumerWebLifecycleErrorKind
	Err  error
}

func (e *ConsumerWebLifecycleError) Error() string {
	if e.Err == nil {
		return string(e.Kind)
	}
	return string(e.Kind) + ": " + e.Err.Error()
}
func (e *ConsumerWebLifecycleError) Unwrap() error { return e.Err }

type ConsumerWebSessionState string

const (
	ConsumerWebSessionEmpty           ConsumerWebSessionState = "EMPTY"
	ConsumerWebSessionBootstrapping   ConsumerWebSessionState = "BOOTSTRAPPING"
	ConsumerWebSessionAuthenticated   ConsumerWebSessionState = "AUTHENTICATED"
	ConsumerWebSessionRefreshing      ConsumerWebSessionState = "REFRESHING"
	ConsumerWebSessionDegraded        ConsumerWebSessionState = "DEGRADED"
	ConsumerWebSessionUnauthenticated ConsumerWebSessionState = "UNAUTHENTICATED"
	ConsumerWebSessionClosed          ConsumerWebSessionState = "CLOSED"
)

type ConsumerWebSessionTransport interface {
	Bootstrap(context.Context, *Account, map[string]any, any) (ConsumerWebBootstrapResult, error)
	RotatePSIDTS(context.Context, *Account, ConsumerWebSessionHandle) (ConsumerWebRotationResult, error)
	Close(context.Context, ConsumerWebSessionHandle) error
}
type consumerWebPreAuthRotator interface {
	PreAuthRotatePSIDTS(context.Context, *Account, map[string]any) (ConsumerWebRotationResult, error)
}
type ConsumerWebSessionInvalidator interface {
	InvalidateAccount(accountID int64)
}
type ConsumerWebSessionHandle interface{}
type ConsumerWebBootstrapResult struct {
	Authenticated                           bool
	BuildLabel, SessionID, Language, PushID string
	Session                                 ConsumerWebSessionHandle
	Registry                                []ConsumerWebModel
}
type ConsumerWebRotationResult struct {
	Authenticated bool
	PSIDTS        string
	Session       ConsumerWebSessionHandle
}

// ConsumerWebModel is session-scoped otAQ metadata. Its transport profile is
// never persisted as an account mapping or credential.
type ConsumerWebModel struct {
	ID, Display, Tier       string
	Capacity, CapacityField int
	ModelNumber             int
	ProfilePresent          bool
}

type ConsumerWebSessionClock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) ConsumerWebTimer
}
type ConsumerWebTimer interface{ Stop() bool }
type realConsumerWebClock struct{}

func (realConsumerWebClock) Now() time.Time { return time.Now() }
func (realConsumerWebClock) AfterFunc(d time.Duration, f func()) ConsumerWebTimer {
	return consumerWebTimer{time.AfterFunc(d, f)}
}

type consumerWebTimer struct{ t *time.Timer }

func (t consumerWebTimer) Stop() bool { return t.t.Stop() }

type consumerWebSessionEntry struct {
	mu          sync.Mutex
	account     *Account
	state       ConsumerWebSessionState
	generation  uint64
	revision    any
	session     ConsumerWebSessionHandle
	registry    []ConsumerWebModel
	timer       ConsumerWebTimer
	nextRefresh time.Time
	cancel      context.CancelFunc
}
type ConsumerWebSessionManager struct {
	transport                      ConsumerWebSessionTransport
	credentials                    AtomicAccountCredentialsRepository
	persister                      ConditionalConsumerWebPSIDTSPersister
	clock                          ConsumerWebSessionClock
	refreshInterval, refreshJitter time.Duration
	root                           context.Context
	cancel                         context.CancelFunc
	closed                         chan struct{}
	entries                        sync.Map
	closeOnce                      sync.Once
}
type ConsumerWebSessionManagerOptions struct {
	Transport                      ConsumerWebSessionTransport
	Credentials                    AtomicAccountCredentialsRepository
	Persister                      ConditionalConsumerWebPSIDTSPersister
	Clock                          ConsumerWebSessionClock
	RefreshInterval, RefreshJitter time.Duration
}

func NewConsumerWebSessionManager(o ConsumerWebSessionManagerOptions) *ConsumerWebSessionManager {
	clock := o.Clock
	if clock == nil {
		clock = realConsumerWebClock{}
	}
	interval := o.RefreshInterval
	if interval == 0 {
		interval = 10 * time.Minute
	}
	m := &ConsumerWebSessionManager{transport: o.Transport, credentials: o.Credentials, persister: o.Persister, clock: clock, refreshInterval: interval, refreshJitter: o.RefreshJitter, closed: make(chan struct{})}
	m.root, m.cancel = context.WithCancel(context.Background())
	return m
}
func (m *ConsumerWebSessionManager) entry(id int64) *consumerWebSessionEntry {
	if v, ok := m.entries.Load(id); ok {
		return v.(*consumerWebSessionEntry)
	}
	e := &consumerWebSessionEntry{state: ConsumerWebSessionEmpty}
	actual, _ := m.entries.LoadOrStore(id, e)
	return actual.(*consumerWebSessionEntry)
}
func (m *ConsumerWebSessionManager) AcquireSession(ctx context.Context, account *Account) (ConsumerWebSessionHandle, error) {
	if account == nil {
		return nil, ErrAccountNilInput
	}
	select {
	case <-m.closed:
		return nil, ErrConsumerWebManagerClosed
	default:
	}
	e := m.entry(account.ID)
	e.mu.Lock()
	if e.state == ConsumerWebSessionAuthenticated && e.session != nil {
		// The database remains authoritative even if another process manually
		// replaced credentials while this manager held an authenticated jar.
		latest, loadErr := m.credentials.LoadCredentialSnapshot(ctx, account.ID)
		if loadErr != nil {
			e.mu.Unlock()
			return nil, wrapConsumerWebAcquireError(loadErr)
		}
		if reflect.DeepEqual(latest.Revision, e.revision) {
			s := e.session
			e.mu.Unlock()
			return s, nil
		}
		_ = m.transport.Close(context.Background(), e.session)
		e.session = nil
		e.registry = nil
		e.state = ConsumerWebSessionEmpty
		e.generation++
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	if e.state == ConsumerWebSessionBootstrapping {
		e.mu.Unlock()
		return m.acquireWait(ctx, e)
	}
	e.state = ConsumerWebSessionBootstrapping
	e.account = account
	startGen := e.generation
	e.mu.Unlock()
	if m.credentials == nil || m.transport == nil {
		e.mu.Lock()
		e.state = ConsumerWebSessionDegraded
		e.mu.Unlock()
		return nil, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireSessionInitFailed, Err: ErrConsumerWebAuthUnavailable}
	}
	snapshot, err := m.credentials.LoadCredentialSnapshot(ctx, account.ID)
	if err != nil {
		m.fail(e, err)
		return nil, wrapConsumerWebAcquireError(err)
	}
	bootstrapCtx, cancel := context.WithCancel(ctx)
	stopClose := context.AfterFunc(m.root, cancel)
	defer func() { stopClose(); cancel() }()
	result, err := m.transport.Bootstrap(bootstrapCtx, account, snapshot.Credentials, snapshot.Revision)
	if errors.Is(err, ErrConsumerWebAPPTokenMissing) && bootstrapCtx.Err() == nil {
		if rotator, ok := m.transport.(consumerWebPreAuthRotator); ok {
			rotation, rotateErr := rotator.PreAuthRotatePSIDTS(bootstrapCtx, account, snapshot.Credentials)
			if rotateErr != nil {
				err = rotateErr
			} else if !rotation.Authenticated {
				err = ErrConsumerWebAuthUnavailable
			} else {
				if rotation.PSIDTS != "" {
					if m.persister == nil {
						err = ErrConsumerWebPersistence
					} else {
						err = m.persister.PersistPSIDTS(bootstrapCtx, account.ID, snapshot.Revision, rotation.PSIDTS)
					}
				}
				if err == nil || errors.Is(err, ErrConsumerWebAPPTokenMissing) {
					snapshot, err = m.credentials.LoadCredentialSnapshot(bootstrapCtx, account.ID)
					if err == nil && bootstrapCtx.Err() == nil {
						result, err = m.transport.Bootstrap(withConsumerWebRecoveryRole(bootstrapCtx, "RETRY_APP"), account, snapshot.Credentials, snapshot.Revision)
					}
				}
			}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if bootstrapCtx.Err() != nil || e.generation != startGen || e.state == ConsumerWebSessionClosed || e.state == ConsumerWebSessionEmpty {
		if result.Session != nil {
			_ = m.transport.Close(context.Background(), result.Session)
		}
		return nil, ErrConsumerWebAuthUnavailable
	}
	if err != nil {
		e.state = ConsumerWebSessionDegraded
		return nil, wrapConsumerWebAcquireError(err)
	}
	if !result.Authenticated || result.Session == nil {
		e.state = ConsumerWebSessionUnauthenticated
		return nil, ErrConsumerWebAuthUnavailable
	}
	e.generation++
	e.revision = snapshot.Revision
	e.session = result.Session
	e.registry = append([]ConsumerWebModel(nil), result.Registry...)
	e.state = ConsumerWebSessionAuthenticated
	m.armLocked(e)
	return e.session, nil
}
func (m *ConsumerWebSessionManager) acquireWait(ctx context.Context, e *consumerWebSessionEntry) (ConsumerWebSessionHandle, error) {
	for {
		e.mu.Lock()
		state, s := e.state, e.session
		e.mu.Unlock()
		if state == ConsumerWebSessionAuthenticated {
			return s, nil
		}
		if state == ConsumerWebSessionUnauthenticated {
			return nil, ErrConsumerWebAuthUnavailable
		}
		if state == ConsumerWebSessionDegraded || state == ConsumerWebSessionClosed || state == ConsumerWebSessionEmpty {
			return nil, ErrConsumerWebAuthUnavailable
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
func (m *ConsumerWebSessionManager) fail(e *consumerWebSessionEntry, _ error) {
	e.mu.Lock()
	e.state = ConsumerWebSessionDegraded
	e.mu.Unlock()
}
func (m *ConsumerWebSessionManager) armLocked(e *consumerWebSessionEntry) {
	if e.timer != nil {
		e.timer.Stop()
	}
	d := m.refreshInterval
	if m.refreshJitter != 0 {
		d += m.refreshJitter
	}
	e.nextRefresh = m.clock.Now().Add(d)
	e.timer = m.clock.AfterFunc(d, func() { m.refresh(e) })
}
func (m *ConsumerWebSessionManager) refresh(e *consumerWebSessionEntry) {
	e.mu.Lock()
	if e.state != ConsumerWebSessionAuthenticated || e.account == nil {
		e.mu.Unlock()
		return
	}
	e.state = ConsumerWebSessionRefreshing
	account, session, rev, gen := e.account, e.session, e.revision, e.generation
	e.mu.Unlock()
	result, err := m.transport.RotatePSIDTS(m.root, account, session)
	if err != nil {
		e.mu.Lock()
		if e.generation == gen && e.state == ConsumerWebSessionRefreshing && m.root.Err() == nil {
			e.state = ConsumerWebSessionDegraded
		}
		e.mu.Unlock()
		return
	}
	if !result.Authenticated {
		e.mu.Lock()
		if e.generation == gen && e.state == ConsumerWebSessionRefreshing && m.root.Err() == nil {
			e.state = ConsumerWebSessionUnauthenticated
		}
		e.mu.Unlock()
		return
	}
	if result.PSIDTS != "" {
		if m.persister == nil {
			err = ErrConsumerWebPersistence
		} else {
			err = m.persister.PersistPSIDTS(m.root, account.ID, rev, result.PSIDTS)
		}
	}
	latest, loadErr := m.credentials.LoadCredentialSnapshot(m.root, account.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generation != gen || e.state != ConsumerWebSessionRefreshing || m.root.Err() != nil {
		return
	}
	if errors.Is(err, ErrCredentialCASConflict) || (err == nil && loadErr == nil && result.PSIDTS == "" && !reflect.DeepEqual(latest.Revision, rev)) {
		slog.Warn("consumer_web_refresh_credential_stale", "account_id", account.ID)
		_ = m.transport.Close(context.Background(), e.session)
		e.state = ConsumerWebSessionEmpty
		e.session = nil
		e.registry = nil
		e.generation++
		return
	}
	if err != nil {
		slog.Warn("consumer_web_refresh_persist_failed", "account_id", account.ID, "cause", string(ConsumerWebPersistenceFailure))
		e.state = ConsumerWebSessionDegraded
		return
	}
	if loadErr != nil || latest == nil {
		slog.Warn("consumer_web_refresh_revision_reload_failed", "account_id", account.ID, "cause", string(ConsumerWebPersistenceFailure))
		e.state = ConsumerWebSessionDegraded
		return
	}
	e.revision = latest.Revision
	if result.Session != nil {
		e.session = result.Session
	}
	e.state = ConsumerWebSessionAuthenticated
	m.armLocked(e)
}
func (m *ConsumerWebSessionManager) InvalidateAccount(id int64) {
	if v, ok := m.entries.Load(id); ok {
		e := v.(*consumerWebSessionEntry)
		e.mu.Lock()
		if e.timer != nil {
			e.timer.Stop()
		}
		if e.cancel != nil {
			e.cancel()
		}
		e.generation++
		e.state = ConsumerWebSessionEmpty
		e.session = nil
		e.registry = nil
		e.mu.Unlock()
	}
}
func (m *ConsumerWebSessionManager) Close() error {
	m.closeOnce.Do(func() {
		close(m.closed)
		m.cancel()
		m.entries.Range(func(_, v any) bool {
			e := v.(*consumerWebSessionEntry)
			e.mu.Lock()
			if e.timer != nil {
				e.timer.Stop()
			}
			if e.session != nil && m.transport != nil {
				_ = m.transport.Close(context.Background(), e.session)
			}
			e.generation++
			e.state = ConsumerWebSessionClosed
			e.session = nil
			e.registry = nil
			e.mu.Unlock()
			return true
		})
	})
	return nil
}
func (m *ConsumerWebSessionManager) State(id int64) ConsumerWebSessionState {
	e := m.entry(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Registry returns an isolated copy of the authenticated account's current
// model registry. Callers must first acquire that account's session.
func (m *ConsumerWebSessionManager) Registry(id int64) []ConsumerWebModel {
	e := m.entry(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != ConsumerWebSessionAuthenticated {
		return nil
	}
	return append([]ConsumerWebModel(nil), e.registry...)
}

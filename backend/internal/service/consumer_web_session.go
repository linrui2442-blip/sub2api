package service

import (
	"context"
	"errors"
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
type ConsumerWebModel struct{ ID, Display, Tier string }

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
		s := e.session
		e.mu.Unlock()
		return s, nil
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
		return nil, ErrConsumerWebAuthUnavailable
	}
	snapshot, err := m.credentials.LoadCredentialSnapshot(ctx, account.ID)
	if err != nil {
		m.fail(e, err)
		return nil, err
	}
	result, err := m.transport.Bootstrap(ctx, account, snapshot.Credentials, snapshot.Revision)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.state = ConsumerWebSessionDegraded
		return nil, err
	}
	if !result.Authenticated || result.Session == nil {
		e.state = ConsumerWebSessionUnauthenticated
		return nil, ErrConsumerWebAuthUnavailable
	}
	if e.generation != startGen || e.state == ConsumerWebSessionEmpty {
		_ = m.transport.Close(ctx, result.Session)
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
		if e.generation == gen {
			e.state = ConsumerWebSessionDegraded
		}
		e.mu.Unlock()
		return
	}
	if !result.Authenticated || m.persister == nil {
		e.mu.Lock()
		e.state = ConsumerWebSessionUnauthenticated
		e.mu.Unlock()
		return
	}
	err = m.persister.PersistPSIDTS(m.root, account.ID, rev, result.PSIDTS)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generation != gen {
		return
	}
	if errors.Is(err, ErrCredentialCASConflict) {
		e.state = ConsumerWebSessionEmpty
		e.session = nil
		e.registry = nil
		e.generation++
		return
	}
	if err != nil {
		e.state = ConsumerWebSessionDegraded
		return
	}
	if m.credentials != nil {
		if latest, loadErr := m.credentials.LoadCredentialSnapshot(m.root, account.ID); loadErr == nil {
			e.revision = latest.Revision
		}
	}
	e.session = result.Session
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

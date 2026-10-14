package service

import (
	"context"
	"errors"
	"log/slog"
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
	mu              sync.Mutex
	account         *Account
	state           ConsumerWebSessionState
	generation      uint64
	revision        any
	core            consumerWebCoreIdentity
	refreshRetry    bool
	refreshErrors   uint
	refreshTimerSeq uint64
	session         ConsumerWebSessionHandle
	registry        []ConsumerWebModel
	timer           ConsumerWebTimer
	nextRefresh     time.Time
	cancel          context.CancelFunc
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
		if coreIdentity(latest.Credentials) == e.core {
			e.revision = latest.Revision
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
	if e.refreshRetry {
		if e.timer != nil {
			e.timer.Stop()
		}
		e.refreshRetry = false
		if e.session != nil {
			_ = m.transport.Close(context.Background(), e.session)
		}
		e.session = nil
		e.registry = nil
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
	bootstrapCtx = m.auxiliaryContext(bootstrapCtx, account, &snapshot)
	result, err := m.transport.Bootstrap(bootstrapCtx, account, snapshot.Credentials, snapshot.Revision)
	otaqUnauthenticated := errors.Is(err, ErrConsumerWebOTAQUnauthenticated)
	if (errors.Is(err, ErrConsumerWebAPPTokenMissing) || otaqUnauthenticated) && bootstrapCtx.Err() == nil {
		if rotator, ok := m.transport.(consumerWebPreAuthRotator); ok {
			rotation, rotateErr := rotator.PreAuthRotatePSIDTS(bootstrapCtx, account, snapshot.Credentials)
			consumerWebBootstrapTraceFromContext(bootstrapCtx).updateLast(ConsumerWebStageRotateCookies, func(s *ConsumerWebBootstrapStep) {
				s.RotateReplacementPresent = rotation.PSIDTS != ""
				if rotateErr != nil {
					s.RotatePersistResult = "NOT_ATTEMPTED_ROTATE_FAILED"
				} else if rotation.PSIDTS == "" {
					s.RotatePersistResult = "NOT_ATTEMPTED_NO_REPLACEMENT"
				}
			})
			if rotateErr != nil {
				err = rotateErr
			} else if otaqUnauthenticated && rotation.PSIDTS == "" {
				// A 200 without replacement is not recovery of a rejected session.
				err = ErrConsumerWebOTAQUnauthenticated
			} else if !rotation.Authenticated {
				err = ErrConsumerWebAuthUnavailable
			} else {
				if rotation.PSIDTS != "" {
					consumerWebBootstrapTraceFromContext(bootstrapCtx).updateLast(ConsumerWebStageRotateCookies, func(s *ConsumerWebBootstrapStep) { s.RotatePersistAttempted = true })
					if m.persister == nil {
						err = ErrConsumerWebPersistence
					} else {
						err = m.persister.PersistPSIDTS(bootstrapCtx, account.ID, snapshot.Revision, rotation.PSIDTS)
					}
					consumerWebBootstrapTraceFromContext(bootstrapCtx).updateLast(ConsumerWebStageRotateCookies, func(s *ConsumerWebBootstrapStep) {
						s.RotateCASConflict = errors.Is(err, ErrCredentialCASConflict)
						if err == nil {
							s.RotatePersistResult = "PASS"
						} else if s.RotateCASConflict {
							s.RotatePersistResult = "CAS_CONFLICT"
						} else {
							s.RotatePersistResult = "FAILED"
						}
					})
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
	e.core = coreIdentity(snapshot.Credentials)
	e.session = result.Session
	e.registry = append([]ConsumerWebModel(nil), result.Registry...)
	e.state = ConsumerWebSessionAuthenticated
	e.refreshErrors = 0
	e.refreshRetry = false
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
	d := m.refreshInterval
	if m.refreshJitter != 0 {
		d += m.refreshJitter
	}
	m.armRefreshAfterLocked(e, d)
}
func (m *ConsumerWebSessionManager) refresh(e *consumerWebSessionEntry) {
	m.refreshExpected(e, nil)
}
func (m *ConsumerWebSessionManager) refreshExpected(e *consumerWebSessionEntry, timerSeq *uint64) {
	e.mu.Lock()
	if timerSeq != nil && *timerSeq != e.refreshTimerSeq {
		e.mu.Unlock()
		return
	}
	if (e.state != ConsumerWebSessionAuthenticated && !(e.state == ConsumerWebSessionDegraded && e.refreshRetry)) || e.account == nil || m.root.Err() != nil {
		e.mu.Unlock()
		return
	}
	e.state = ConsumerWebSessionRefreshing
	e.refreshRetry = false
	account, session, rev, gen, core := e.account, e.session, e.revision, e.generation, e.core
	e.mu.Unlock()
	// Do not dispatch another request using an externally superseded core pair.
	before, loadErr := m.credentials.LoadCredentialSnapshot(m.root, account.ID)
	if loadErr != nil || before == nil {
		m.refreshFailed(e, gen, ErrConsumerWebPersistence, "PRE_ROTATE_LOCAL_LOAD")
		return
	}
	if coreIdentity(before.Credentials) != core {
		e.mu.Lock()
		if e.generation == gen && e.state == ConsumerWebSessionRefreshing && m.root.Err() == nil {
			m.stopRefreshLocked(e, "CORE_CHANGED_TERMINAL_STOP")
		}
		e.mu.Unlock()
		return
	}
	result, err := m.transport.RotatePSIDTS(m.root, account, session)
	if err != nil {
		m.refreshFailed(e, gen, err, "ROTATE")
		return
	}
	if !result.Authenticated {
		m.refreshFailed(e, gen, ErrConsumerWebAuthLost, "ROTATE_RESULT")
		return
	}
	latest, branch, err := m.reconcileRefresh(account.ID, rev, core, result.PSIDTS)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generation != gen || e.state != ConsumerWebSessionRefreshing || m.root.Err() != nil {
		return
	}
	slog.Info("consumer_web_refresh_result", "safe_class", branch)
	if branch == "CORE_CREDENTIAL_CHANGED_SESSION_STALE" || branch == "CAS_CONFLICT_CORE_CHANGED_ABORTED" {
		m.stopRefreshLocked(e, "CORE_CHANGED_TERMINAL_STOP")
		return
	}
	if err != nil {
		slog.Warn("consumer_web_refresh_persist_failed", "account_id", account.ID, "cause", string(ConsumerWebPersistenceFailure))
		m.retryRefreshLocked(e, "TRANSIENT_LOCAL_ERROR", "RECONCILE")
		return
	}
	if latest == nil {
		slog.Warn("consumer_web_refresh_revision_reload_failed", "account_id", account.ID, "cause", string(ConsumerWebPersistenceFailure))
		m.retryRefreshLocked(e, "TRANSIENT_LOCAL_ERROR", "RECONCILE_LOAD")
		return
	}
	e.revision = latest.Revision
	e.core = coreIdentity(latest.Credentials)
	if result.Session != nil {
		e.session = result.Session
	}
	e.state = ConsumerWebSessionAuthenticated
	event := "REFRESH_SUCCESS"
	if e.refreshErrors != 0 {
		event = "REFRESH_RECOVERED_AFTER_ERROR"
	}
	e.refreshErrors = 0
	e.refreshRetry = false
	m.armLocked(e)
	slog.Info("consumer_web_refresh_lifecycle", "safe_class", event, "outcome", "SUCCESS", "timer_rearmed", true)
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

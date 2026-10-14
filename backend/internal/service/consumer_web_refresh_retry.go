package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"
)

// Timer identity prevents a stopped callback from stealing a newer lifecycle.
func (m *ConsumerWebSessionManager) armRefreshAfterLocked(e *consumerWebSessionEntry, d time.Duration) {
	if e.timer != nil {
		e.timer.Stop()
	}
	if d <= 0 {
		d = 10 * time.Minute
	}
	e.refreshTimerSeq++
	seq := e.refreshTimerSeq
	e.nextRefresh = m.clock.Now().Add(d)
	e.timer = m.clock.AfterFunc(d, func() {
		m.refreshExpected(e, &seq)
	})
}

func refreshErrorClass(err error) string {
	if errors.Is(err, ErrConsumerWebAuthLost) || errors.Is(err, ErrConsumerWebOTAQUnauthenticated) {
		return "SESSION_EXPLICITLY_INVALID_TERMINAL"
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) {
		return "TRANSIENT_NETWORK_ERROR"
	}
	if errors.Is(err, ErrConsumerWebRateLimited) {
		return "TRANSIENT_PROVIDER_ERROR"
	}
	// This sentinel represents both HTTP 5xx and transport errors. Do not guess.
	if errors.Is(err, ErrConsumerWebPersistence) || errors.Is(err, ErrCredentialCASConflict) {
		return "TRANSIENT_LOCAL_ERROR"
	}
	return "UNKNOWN_REFRESH_ERROR"
}

func (m *ConsumerWebSessionManager) refreshFailed(e *consumerWebSessionEntry, gen uint64, err error, stage string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generation != gen || e.state != ConsumerWebSessionRefreshing || m.root.Err() != nil {
		return
	}
	// Catch an external core edit that raced the failed request. A load failure
	// remains recoverable, but the next attempt must pass this gate again.
	latest, loadErr := m.credentials.LoadCredentialSnapshot(m.root, e.account.ID)
	if loadErr == nil && latest != nil && coreIdentity(latest.Credentials) != e.core {
		m.stopRefreshLocked(e, "CORE_CHANGED_TERMINAL_STOP")
		return
	}
	class := refreshErrorClass(err)
	if class == "SESSION_EXPLICITLY_INVALID_TERMINAL" {
		m.stopRefreshLocked(e, "TERMINAL_SESSION_STOP")
		return
	}
	m.retryRefreshLocked(e, class, stage)
}

func (m *ConsumerWebSessionManager) retryRefreshLocked(e *consumerWebSessionEntry, class, stage string) {
	if e.refreshErrors < 3 {
		e.refreshErrors++
	}
	base := m.refreshInterval + m.refreshJitter
	if base < time.Minute {
		base = time.Minute
	}
	if base > 10*time.Minute {
		base = 10 * time.Minute
	}
	d := base * time.Duration(1<<(e.refreshErrors-1))
	e.state = ConsumerWebSessionDegraded // Scheduling is not an authentication proof.
	e.refreshRetry = true
	event := "REFRESH_ERROR_RETRY_SCHEDULED"
	if e.refreshErrors > 1 {
		event = "REFRESH_ERROR_BACKOFF_SCHEDULED"
	}
	m.armRefreshAfterLocked(e, d)
	slog.Info("consumer_web_refresh_lifecycle", "safe_class", event, "outcome", class, "stage", stage, "error_streak_capped", e.refreshErrors, "retry_after_seconds", int64(d/time.Second), "timer_rearmed", true)
}

func (m *ConsumerWebSessionManager) stopRefreshLocked(e *consumerWebSessionEntry, event string) {
	if e.timer != nil {
		e.timer.Stop()
	}
	if e.session != nil {
		_ = m.transport.Close(context.Background(), e.session)
	}
	e.session = nil
	e.registry = nil
	e.refreshRetry = false
	e.generation++
	e.state = ConsumerWebSessionEmpty
	outcome := "CORE_CREDENTIAL_CHANGED_TERMINAL"
	if event == "TERMINAL_SESSION_STOP" {
		e.state = ConsumerWebSessionUnauthenticated
		outcome = "SESSION_EXPLICITLY_INVALID_TERMINAL"
	}
	slog.Info("consumer_web_refresh_lifecycle", "safe_class", event, "outcome", outcome, "timer_rearmed", false)
}

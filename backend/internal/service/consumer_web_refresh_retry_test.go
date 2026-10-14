package service

import (
	"bytes"
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestConsumerWebR25SafeClassificationAndLogs(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "TRANSIENT_NETWORK_ERROR"},
		{ErrConsumerWebRateLimited, "TRANSIENT_PROVIDER_ERROR"},
		{ErrConsumerWebPersistence, "TRANSIENT_LOCAL_ERROR"},
		{ErrConsumerWebOTAQUnauthenticated, "SESSION_EXPLICITLY_INVALID_TERMINAL"},
		{errors.New("secret-error-marker"), "UNKNOWN_REFRESH_ERROR"},
	} {
		require.Equal(t, tc.want, refreshErrorClass(tc.err))
	}
	var out bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	defer slog.SetDefault(old)
	creds := durabilityCredentials()
	tr := &r25Transport{err: errors.New("secret-error-marker")}
	clock := &r24Clock{}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Clock: clock})
	defer m.Close()
	a := testSessionAccount(28)
	_, err := m.AcquireSession(context.Background(), a)
	require.NoError(t, err)
	clock.fire(t)
	require.False(t, strings.Contains(out.String(), "secret-error-marker"))
	require.Contains(t, out.String(), "UNKNOWN_REFRESH_ERROR")
	require.Contains(t, out.String(), "REFRESH_ERROR_RETRY_SCHEDULED")
	tr.err = nil
	tr.invalid = true
	clock.fire(t)
	require.Equal(t, ConsumerWebSessionUnauthenticated, m.State(a.ID))
	require.Zero(t, clock.active())
}

type r25Transport struct {
	durabilityTransport
	err     error
	invalid bool
	hook    func()
}

func (f *r25Transport) RotatePSIDTS(ctx context.Context, a *Account, h ConsumerWebSessionHandle) (ConsumerWebRotationResult, error) {
	if f.hook != nil {
		f.hook()
	}
	if f.err != nil {
		f.rotates.Add(1)
		return ConsumerWebRotationResult{}, f.err
	}
	if f.invalid {
		f.rotates.Add(1)
		return ConsumerWebRotationResult{}, nil
	}
	return f.durabilityTransport.RotatePSIDTS(ctx, a, h)
}

func TestConsumerWebR25ErrorBackoffRecovery(t *testing.T) {
	ctx := context.Background()
	creds := durabilityCredentials()
	tr := &r25Transport{}
	clock := &r24Clock{}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: &durabilityPersister{creds: creds}, Clock: clock})
	defer m.Close()
	a := testSessionAccount(25)
	_, err := m.AcquireSession(ctx, a)
	require.NoError(t, err)
	clock.fire(t)
	clock.fire(t)
	tr.err = context.DeadlineExceeded
	for i, want := range []time.Duration{10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 40 * time.Minute, 40 * time.Minute} {
		before := tr.rotates.Load()
		clock.fire(t)
		require.Equal(t, before+1, tr.rotates.Load())
		require.Equal(t, ConsumerWebSessionDegraded, m.State(a.ID))
		require.Equal(t, 1, clock.active())
		e := m.entry(a.ID)
		require.True(t, e.refreshRetry)
		require.Equal(t, want, e.nextRefresh.Sub(clock.Now()))
		require.NotNil(t, e.session)
		require.LessOrEqual(t, e.refreshErrors, uint(3))
		_ = i
	}
	tr.err = nil
	clock.fire(t)
	e := m.entry(a.ID)
	require.Equal(t, ConsumerWebSessionAuthenticated, m.State(a.ID))
	require.Zero(t, e.refreshErrors)
	require.False(t, e.refreshRetry)
	require.Equal(t, 10*time.Minute, e.nextRefresh.Sub(clock.Now()))
	require.Equal(t, 1, clock.active())
	clock.fire(t)
	require.Equal(t, 1, clock.active())
}

func TestConsumerWebR25CoreEditAndTerminalStop(t *testing.T) {
	for _, kind := range []string{"psid", "psidts", "auth", "unknown", "during_error"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			creds := durabilityCredentials()
			tr := &r25Transport{}
			clock := &r24Clock{}
			m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: &durabilityPersister{creds: creds}, Clock: clock})
			defer m.Close()
			a := testSessionAccount(26)
			_, err := m.AcquireSession(ctx, a)
			require.NoError(t, err)
			change := func() {
				s, _ := creds.LoadCredentialSnapshot(ctx, a.ID)
				field := ConsumerWebPSIDTSCiphertextKey
				if kind == "psid" {
					field = ConsumerWebPSIDCiphertextKey
				}
				_, e := creds.CompareAndSwapCredentials(ctx, a.ID, s.Revision, UpdateAccountCredentialNamespace(s.Credentials, ConsumerWebCredentialNamespace, map[string]any{field: "new-winner"}))
				require.NoError(t, e)
			}
			if kind == "psid" || kind == "psidts" {
				tr.err = context.DeadlineExceeded
				clock.fire(t)
				change()
			}
			if kind == "during_error" {
				tr.hook = change
				tr.err = context.DeadlineExceeded
			}
			if kind == "auth" {
				tr.err = ErrConsumerWebAuthLost
			}
			if kind == "unknown" {
				tr.err = errors.New("synthetic-secret-must-not-be-logged")
			}
			clock.fire(t)
			if kind == "unknown" {
				require.Equal(t, 1, clock.active())
				require.Equal(t, ConsumerWebSessionDegraded, m.State(a.ID))
			} else {
				require.Zero(t, clock.active())
				require.Nil(t, m.entry(a.ID).session)
				for _, timer := range clock.timers {
					timer.fn()
				}
				require.Zero(t, clock.active())
				if kind != "auth" {
					s, _ := creds.LoadCredentialSnapshot(ctx, a.ID)
					ns := s.Credentials[ConsumerWebCredentialNamespace].(map[string]any)
					require.True(t, ns[ConsumerWebPSIDCiphertextKey] == "new-winner" || ns[ConsumerWebPSIDTSCiphertextKey] == "new-winner")
				}
			}
		})
	}
}

func TestConsumerWebR25CancelledTimerCannotRevive(t *testing.T) {
	creds := durabilityCredentials()
	tr := &r25Transport{}
	clock := &r24Clock{}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Clock: clock})
	a := testSessionAccount(27)
	_, err := m.AcquireSession(context.Background(), a)
	require.NoError(t, err)
	old := clock.timers[0]
	clock.fire(t)
	n := tr.rotates.Load()
	old.fn()
	require.Equal(t, n, tr.rotates.Load())
	m.Close()
	for _, timer := range clock.timers {
		timer.fn()
	}
	require.Equal(t, n, tr.rotates.Load())
	require.Zero(t, clock.active())
}

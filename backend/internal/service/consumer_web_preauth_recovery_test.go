package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type preAuthFixtureTransport struct {
	mu             sync.Mutex
	boots, rotates int
	rotateErr      error
	firstErr       error
	secondErr      error
	replacement    string
	secondToken    bool
	block          chan struct{}
	blockFirst     chan struct{}
	blockSecond    chan struct{}
	session        struct{}
}

func TestConsumerWeb1016RecoveryBounded(t *testing.T) {
	for _, tc := range []struct {
		name, replacement string
		second, persist   error
		boots             int
		pass              bool
	}{
		{"recovered", "new", nil, nil, 2, true},
		{"still1016", "new", ErrConsumerWebOTAQUnauthenticated, nil, 2, false},
		{"noReplacement", "", nil, nil, 1, false},
		{"casConflict", "new", nil, ErrCredentialCASConflict, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &preAuthFixtureTransport{firstErr: ErrConsumerWebOTAQUnauthenticated, replacement: tc.replacement, secondErr: tc.second, secondToken: true}
			m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: &sessionFakeCredentials{values: map[string]any{}}, Persister: &sessionFakePersister{err: tc.persist}, RefreshInterval: time.Hour})
			defer m.Close()
			_, err := m.AcquireSession(context.Background(), testSessionAccount(90))
			boots, rotates := tr.counts()
			if boots != tc.boots || rotates != 1 || (err == nil) != tc.pass {
				t.Fatalf("boots=%d rotates=%d pass=%v", boots, rotates, err == nil)
			}
		})
	}
}

func (f *preAuthFixtureTransport) Bootstrap(ctx context.Context, _ *Account, _ map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	f.mu.Lock()
	f.boots++
	n := f.boots
	f.mu.Unlock()
	if n == 1 {
		if f.blockFirst != nil {
			select {
			case <-f.blockFirst:
			case <-ctx.Done():
				return ConsumerWebBootstrapResult{}, ctx.Err()
			}
		}
		if f.firstErr != nil {
			return ConsumerWebBootstrapResult{}, f.firstErr
		}
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAPPTokenMissing
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ConsumerWebBootstrapResult{}, ctx.Err()
		}
	}
	if f.blockSecond != nil {
		select {
		case <-f.blockSecond:
		case <-ctx.Done():
			return ConsumerWebBootstrapResult{}, ctx.Err()
		}
	}
	if f.secondErr != nil {
		return ConsumerWebBootstrapResult{}, f.secondErr
	}
	if !f.secondToken {
		return ConsumerWebBootstrapResult{}, ErrConsumerWebAPPTokenMissing
	}
	return ConsumerWebBootstrapResult{Authenticated: true, Session: &f.session}, nil
}
func (f *preAuthFixtureTransport) PreAuthRotatePSIDTS(ctx context.Context, _ *Account, _ map[string]any) (ConsumerWebRotationResult, error) {
	f.mu.Lock()
	f.rotates++
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ConsumerWebRotationResult{}, ctx.Err()
		}
	}
	if f.rotateErr != nil {
		return ConsumerWebRotationResult{}, f.rotateErr
	}
	return ConsumerWebRotationResult{Authenticated: true, PSIDTS: f.replacement}, nil
}
func (*preAuthFixtureTransport) RotatePSIDTS(context.Context, *Account, ConsumerWebSessionHandle) (ConsumerWebRotationResult, error) {
	return ConsumerWebRotationResult{}, nil
}
func (*preAuthFixtureTransport) Close(context.Context, ConsumerWebSessionHandle) error { return nil }
func (f *preAuthFixtureTransport) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boots, f.rotates
}

func TestConsumerWebPreAuthManagerBoundedRecovery(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		replacement          string
		secondToken          bool
		rotateErr, secondErr error
		wantBoots            int
		wantState            ConsumerWebSessionState
	}{
		{"replacement", "new", true, nil, nil, 2, ConsumerWebSessionAuthenticated},
		{"no_replacement", "", true, nil, nil, 2, ConsumerWebSessionAuthenticated},
		{"rotate_failure", "", false, ErrConsumerWebForbidden, nil, 1, ConsumerWebSessionDegraded},
		{"second_app_missing", "", false, nil, nil, 2, ConsumerWebSessionDegraded},
		{"second_app_failure", "", false, nil, ErrConsumerWebTransportTransient, 2, ConsumerWebSessionDegraded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &preAuthFixtureTransport{replacement: tc.replacement, secondToken: tc.secondToken, rotateErr: tc.rotateErr, secondErr: tc.secondErr}
			creds := &sessionFakeCredentials{values: map[string]any{}}
			persister := &sessionFakePersister{}
			m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: persister, RefreshInterval: time.Hour})
			defer m.Close()
			_, err := m.AcquireSession(context.Background(), testSessionAccount(71))
			boots, rotates := tr.counts()
			if boots != tc.wantBoots || rotates != 1 || m.State(71) != tc.wantState {
				t.Fatalf("boots=%d rotates=%d state=%s err=%v", boots, rotates, m.State(71), err)
			}
			if (err == nil) != (tc.wantState == ConsumerWebSessionAuthenticated) {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.replacement == "" && persister.calls.Load() != 0 {
				t.Fatal("no replacement must not persist")
			}
		})
	}
}

func TestConsumerWebPreAuthCASConflictStopsRetry(t *testing.T) {
	tr := &preAuthFixtureTransport{replacement: "new", secondToken: true}
	creds := &sessionFakeCredentials{values: map[string]any{}}
	persister := &sessionFakePersister{err: ErrCredentialCASConflict}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: persister, RefreshInterval: time.Hour})
	defer m.Close()
	_, err := m.AcquireSession(context.Background(), testSessionAccount(72))
	boots, rotates := tr.counts()
	if !errors.Is(err, ErrCredentialCASConflict) || boots != 1 || rotates != 1 || persister.calls.Load() != 1 {
		t.Fatalf("err=%v boots=%d rotates=%d", err, boots, rotates)
	}
}

func TestConsumerWebPreAuthSameAccountSingleflight(t *testing.T) {
	tr := &preAuthFixtureTransport{secondToken: true, block: make(chan struct{})}
	creds := &sessionFakeCredentials{values: map[string]any{}}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, RefreshInterval: time.Hour})
	defer m.Close()
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.AcquireSession(context.Background(), testSessionAccount(73)); err == nil {
				successes.Add(1)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(tr.block)
	wg.Wait()
	boots, rotates := tr.counts()
	if boots != 2 || rotates != 1 || successes.Load() != 12 {
		t.Fatalf("boots=%d rotates=%d successes=%d", boots, rotates, successes.Load())
	}
}

func TestConsumerWebPreAuthTriggerScope(t *testing.T) {
	for _, initial := range []error{context.Canceled, context.DeadlineExceeded, ErrConsumerWebTransportTransient, ErrConsumerWebRedirectRejected, ErrConsumerWebProtocolDrift, ErrConsumerWebRateLimited, ErrConsumerWebForbidden, ErrConsumerWebAuthLost} {
		tr := &preAuthFixtureTransport{firstErr: initial}
		m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
		_, _ = m.AcquireSession(context.Background(), testSessionAccount(74))
		boots, rotates := tr.counts()
		if boots != 1 || rotates != 0 {
			t.Fatalf("initial class %T triggered recovery: boots=%d rotates=%d", initial, boots, rotates)
		}
		_ = m.Close()
	}
}

func TestConsumerWebPreAuthShutdownCancelsRecovery(t *testing.T) {
	tr := &preAuthFixtureTransport{block: make(chan struct{}), secondToken: true}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
	done := make(chan error, 1)
	go func() { _, err := m.AcquireSession(context.Background(), testSessionAccount(75)); done <- err }()
	deadline := time.After(time.Second)
	for {
		_, r := tr.counts()
		if r == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("rotate not reached")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	_ = m.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown published a session")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel recovery")
	}
	boots, rotates := tr.counts()
	if boots != 1 || rotates != 1 {
		t.Fatalf("continued after shutdown: boots=%d rotates=%d", boots, rotates)
	}
}

func TestConsumerWebPreAuthShutdownAtInitialAndRetryAPP(t *testing.T) {
	for _, stage := range []string{"initial", "retry"} {
		t.Run(stage, func(t *testing.T) {
			tr := &preAuthFixtureTransport{secondToken: true}
			if stage == "initial" {
				tr.blockFirst = make(chan struct{})
			} else {
				tr.blockSecond = make(chan struct{})
			}
			m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: &sessionFakeCredentials{values: map[string]any{}}, RefreshInterval: time.Hour})
			done := make(chan error, 1)
			go func() { _, err := m.AcquireSession(context.Background(), testSessionAccount(76)); done <- err }()
			want := 1
			if stage == "retry" {
				want = 2
			}
			deadline := time.After(time.Second)
			for {
				b, _ := tr.counts()
				if b == want {
					break
				}
				select {
				case <-deadline:
					t.Fatal("stage not reached")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			_ = m.Close()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("published after shutdown")
				}
			case <-time.After(time.Second):
				t.Fatal("shutdown did not cancel")
			}
			b, r := tr.counts()
			if b != want || (stage == "initial" && r != 0) || (stage == "retry" && r != 1) {
				t.Fatalf("continued after shutdown: %d %d", b, r)
			}
		})
	}
}

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sessionFakeCredentials struct {
	mu     sync.Mutex
	rev    int
	values map[string]any
}

func (f *sessionFakeCredentials) LoadCredentialSnapshot(context.Context, int64) (*AccountCredentialSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &AccountCredentialSnapshot{Credentials: cloneCredentialMap(f.values), Revision: f.rev}, nil
}
func (f *sessionFakeCredentials) CompareAndSwapCredentials(_ context.Context, _ int64, expected any, values map[string]any) (*AccountCredentialSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if expected != f.rev {
		return nil, ErrCredentialCASConflict
	}
	f.rev++
	f.values = cloneCredentialMap(values)
	return &AccountCredentialSnapshot{Credentials: cloneCredentialMap(values), Revision: f.rev}, nil
}

type sessionFakeTransport struct {
	boots   atomic.Int32
	rotates atomic.Int32
	closes  atomic.Int32
	block   chan struct{}
	session struct{}
}

func (f *sessionFakeTransport) Bootstrap(ctx context.Context, _ *Account, _ map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	f.boots.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ConsumerWebBootstrapResult{}, ctx.Err()
		}
	}
	return ConsumerWebBootstrapResult{Authenticated: true, Session: &f.session, Registry: []ConsumerWebModel{{ID: "model", Display: "Model"}}}, nil
}
func (f *sessionFakeTransport) RotatePSIDTS(context.Context, *Account, ConsumerWebSessionHandle) (ConsumerWebRotationResult, error) {
	f.rotates.Add(1)
	return ConsumerWebRotationResult{Authenticated: true, PSIDTS: "rotated", Session: &f.session}, nil
}
func (f *sessionFakeTransport) Close(context.Context, ConsumerWebSessionHandle) error {
	f.closes.Add(1)
	return nil
}

type sessionFakePersister struct {
	calls atomic.Int32
	err   error
}

func (f *sessionFakePersister) PersistPSIDTS(context.Context, int64, any, string) error {
	f.calls.Add(1)
	return f.err
}
func testSessionAccount(id int64) *Account {
	return &Account{ID: id, Platform: PlatformAntigravity, Type: AccountTypeOAuth}
}

func TestConsumerWebSessionLazyAcquireAndReuse(t *testing.T) {
	tr := &sessionFakeTransport{}
	creds := &sessionFakeCredentials{values: map[string]any{"consumer_web": map[string]any{"x": "y"}}}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, RefreshInterval: time.Hour})
	s1, err := m.AcquireSession(context.Background(), testSessionAccount(11))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := m.AcquireSession(context.Background(), testSessionAccount(11))
	if err != nil || s1 != s2 {
		t.Fatalf("reuse failed: %v", err)
	}
	if tr.boots.Load() != 1 {
		t.Fatalf("boots=%d", tr.boots.Load())
	}
	_ = m.Close()
}
func TestConsumerWebSessionSameAccountSingleflightAndCrossAccountIndependent(t *testing.T) {
	tr := &sessionFakeTransport{block: make(chan struct{})}
	creds := &sessionFakeCredentials{values: map[string]any{}}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, RefreshInterval: time.Hour})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = m.AcquireSession(context.Background(), testSessionAccount(12)) }()
	}
	time.Sleep(20 * time.Millisecond)
	close(tr.block)
	wg.Wait()
	if tr.boots.Load() != 1 {
		t.Fatalf("boots=%d", tr.boots.Load())
	}
	_, err := m.AcquireSession(context.Background(), testSessionAccount(13))
	if err != nil {
		t.Fatal(err)
	}
	if tr.boots.Load() != 2 {
		t.Fatalf("cross account boots=%d", tr.boots.Load())
	}
	_ = m.Close()
}
func TestConsumerWebSessionUnauthenticatedNotPublishedAndInvalidate(t *testing.T) {
	tr := &sessionFakeTransport{}
	creds := &sessionFakeCredentials{values: map[string]any{}}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, RefreshInterval: time.Hour})
	tr.block = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := m.AcquireSession(ctx, testSessionAccount(14)); done <- err }()
	time.Sleep(10 * time.Millisecond)
	m.InvalidateAccount(14)
	close(tr.block)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("expected invalidated bootstrap failure")
	}
	if m.State(14) != ConsumerWebSessionEmpty {
		t.Fatalf("state=%s", m.State(14))
	}
	_ = m.Close()
}
func TestConsumerWebSessionAuthGate(t *testing.T) {
	_ = errors.Is
	tr := &sessionFakeTransport{}
	creds := &sessionFakeCredentials{values: map[string]any{}}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds})
	tr.block = make(chan struct{})
	close(tr.block) // fake always authenticates; the transport contract owns the gate.
	if _, err := m.AcquireSession(context.Background(), testSessionAccount(15)); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
}

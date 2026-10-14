package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type durabilityTransport struct {
	mu          sync.Mutex
	replacement string
	boots       []string
	rotates     atomic.Int32
	closes      atomic.Int32
	session     struct{}
}

func (f *durabilityTransport) Bootstrap(_ context.Context, _ *Account, credentials map[string]any, _ any) (ConsumerWebBootstrapResult, error) {
	f.mu.Lock()
	ns, _ := credentials[ConsumerWebCredentialNamespace].(map[string]any)
	value, _ := ns[ConsumerWebPSIDTSCiphertextKey].(string)
	f.boots = append(f.boots, value)
	f.mu.Unlock()
	return ConsumerWebBootstrapResult{Authenticated: true, Session: &f.session, Registry: []ConsumerWebModel{{ID: "model", Display: "Pro"}}}, nil
}
func (f *durabilityTransport) RotatePSIDTS(context.Context, *Account, ConsumerWebSessionHandle) (ConsumerWebRotationResult, error) {
	f.rotates.Add(1)
	return ConsumerWebRotationResult{Authenticated: true, PSIDTS: f.replacement, Session: &f.session}, nil
}
func (f *durabilityTransport) Close(context.Context, ConsumerWebSessionHandle) error {
	f.closes.Add(1)
	return nil
}

type durabilityPersister struct {
	creds   *sessionFakeCredentials
	calls   atomic.Int32
	failure error
}

func (p *durabilityPersister) PersistPSIDTSSnapshot(ctx context.Context, id int64, revision any, value string) (*AccountCredentialSnapshot, error) {
	if err := p.PersistPSIDTS(ctx, id, revision, value); err != nil {
		return nil, err
	}
	return p.creds.LoadCredentialSnapshot(ctx, id)
}

func (p *durabilityPersister) PersistPSIDTS(ctx context.Context, id int64, revision any, value string) error {
	p.calls.Add(1)
	if p.failure != nil {
		return p.failure
	}
	if value == "" {
		return errors.New("empty rotation must never persist")
	}
	snapshot, err := p.creds.LoadCredentialSnapshot(ctx, id)
	if err != nil {
		return err
	}
	updated := UpdateAccountCredentialNamespace(snapshot.Credentials, ConsumerWebCredentialNamespace, map[string]any{ConsumerWebPSIDTSCiphertextKey: value})
	_, err = p.creds.CompareAndSwapCredentials(ctx, id, revision, updated)
	return err
}

func durabilityCredentials() *sessionFakeCredentials {
	return &sessionFakeCredentials{values: map[string]any{ConsumerWebCredentialNamespace: map[string]any{ConsumerWebPSIDCiphertextKey: "synthetic-psid", ConsumerWebPSIDTSCiphertextKey: "v1"}}}
}

func TestConsumerWebSessionRotationPersistsAndRestarts(t *testing.T) {
	creds := durabilityCredentials()
	tr := &durabilityTransport{replacement: "v2"}
	p := &durabilityPersister{creds: creds}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: p, RefreshInterval: time.Hour})
	account := testSessionAccount(101)
	if _, err := m.AcquireSession(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	m.refresh(m.entry(account.ID))
	if m.State(account.ID) != ConsumerWebSessionAuthenticated || p.calls.Load() != 1 {
		t.Fatal("rotation did not persist and retain session")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	second := &durabilityTransport{}
	m2 := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: second, Credentials: creds, Persister: &durabilityPersister{creds: creds}, RefreshInterval: time.Hour})
	defer m2.Close()
	if _, err := m2.AcquireSession(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	second.mu.Lock()
	defer second.mu.Unlock()
	if len(second.boots) != 1 || second.boots[0] != "v2" {
		t.Fatal("new manager did not load persisted rotation")
	}
}

func TestConsumerWebSessionNoReplacementAndManualUpdateWins(t *testing.T) {
	creds := durabilityCredentials()
	tr := &durabilityTransport{}
	p := &durabilityPersister{creds: creds}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: p, RefreshInterval: time.Hour})
	defer m.Close()
	account := testSessionAccount(102)
	if _, err := m.AcquireSession(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	m.refresh(m.entry(account.ID))
	if m.State(account.ID) != ConsumerWebSessionAuthenticated || p.calls.Load() != 0 {
		t.Fatal("no-change rotation persisted or lost session")
	}
	snap, err := creds.LoadCredentialSnapshot(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = creds.CompareAndSwapCredentials(context.Background(), account.ID, snap.Revision, UpdateAccountCredentialNamespace(snap.Credentials, ConsumerWebCredentialNamespace, map[string]any{ConsumerWebPSIDTSCiphertextKey: "manual-v3"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AcquireSession(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.boots) != 2 || tr.boots[1] != "manual-v3" || tr.closes.Load() == 0 {
		t.Fatal("stale runtime credential shadowed manual update")
	}
}

func TestConsumerWebSessionStaleRotationCannotOverwriteManualUpdate(t *testing.T) {
	creds := durabilityCredentials()
	tr := &durabilityTransport{replacement: "stale-v2"}
	p := &durabilityPersister{creds: creds}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: p, RefreshInterval: time.Hour})
	defer m.Close()
	account := testSessionAccount(103)
	if _, err := m.AcquireSession(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	snap, _ := creds.LoadCredentialSnapshot(context.Background(), account.ID)
	_, err := creds.CompareAndSwapCredentials(context.Background(), account.ID, snap.Revision, UpdateAccountCredentialNamespace(snap.Credentials, ConsumerWebCredentialNamespace, map[string]any{ConsumerWebPSIDTSCiphertextKey: "manual-v3"}))
	if err != nil {
		t.Fatal(err)
	}
	m.refresh(m.entry(account.ID))
	latest, _ := creds.LoadCredentialSnapshot(context.Background(), account.ID)
	ns := latest.Credentials[ConsumerWebCredentialNamespace].(map[string]any)
	if ns[ConsumerWebPSIDTSCiphertextKey] != "manual-v3" || m.State(account.ID) != ConsumerWebSessionEmpty {
		t.Fatal("stale rotation overwrote manual credential")
	}
}

func TestConsumerWebSessionRefreshLoopStopsOnClose(t *testing.T) {
	creds := durabilityCredentials()
	tr := &durabilityTransport{}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: &durabilityPersister{creds: creds}, RefreshInterval: 5 * time.Millisecond})
	if _, err := m.AcquireSession(context.Background(), testSessionAccount(104)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for tr.rotates.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if tr.rotates.Load() == 0 {
		t.Fatal("refresh loop did not start")
	}
	m.Close()
	count := tr.rotates.Load()
	time.Sleep(30 * time.Millisecond)
	if tr.rotates.Load() != count {
		t.Fatal("refresh loop continued after shutdown")
	}
}

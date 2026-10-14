package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

type r24Timer struct {
	active bool
	fn     func()
}

func (t *r24Timer) Stop() bool { old := t.active; t.active = false; return old }

type r24Clock struct{ timers []*r24Timer }

func (c *r24Clock) Now() time.Time { return time.Unix(0, 0) }
func (c *r24Clock) AfterFunc(_ time.Duration, fn func()) ConsumerWebTimer {
	t := &r24Timer{true, fn}
	c.timers = append(c.timers, t)
	return t
}
func (c *r24Clock) fire(t *testing.T) {
	t.Helper()
	var a []*r24Timer
	for _, v := range c.timers {
		if v.active {
			a = append(a, v)
		}
	}
	require.Len(t, a, 1)
	a[0].active = false
	a[0].fn()
}
func (c *r24Clock) active() int {
	n := 0
	for _, v := range c.timers {
		if v.active {
			n++
		}
	}
	return n
}

// Embedded interface supplies unused repository methods; actual CAS/load use
// the same in-memory atomic repository as the existing session tests.
type r24Repo struct {
	AccountRepository
	*sessionFakeCredentials
}

type r24RacingRepo struct {
	*r24Repo
	calls       int
	changeField string
	every       bool
}

func (r *r24RacingRepo) CompareAndSwapCredentials(ctx context.Context, id int64, rev any, values map[string]any) (*AccountCredentialSnapshot, error) {
	r.calls++
	if r.calls == 1 || r.every {
		s, _ := r.LoadCredentialSnapshot(ctx, id)
		_, err := r.sessionFakeCredentials.CompareAndSwapCredentials(ctx, id, s.Revision, UpdateAccountCredentialNamespace(s.Credentials, ConsumerWebCredentialNamespace, map[string]any{r.changeField: "concurrent-winner"}))
		if err != nil {
			return nil, err
		}
	}
	return r.sessionFakeCredentials.CompareAndSwapCredentials(ctx, id, rev, values)
}

func TestConsumerWebR24ProductionPersisterRaceAndBound(t *testing.T) {
	for _, tc := range []struct {
		field  string
		every  bool
		calls  int
		branch string
	}{
		{consumerWebAuxKey, false, 2, "CAS_CONFLICT_CORE_UNCHANGED_RECONCILED"},
		{ConsumerWebPSIDCiphertextKey, false, 1, "CAS_CONFLICT_CORE_CHANGED_ABORTED"},
		{ConsumerWebPSIDTSCiphertextKey, false, 1, "CAS_CONFLICT_CORE_CHANGED_ABORTED"},
		{consumerWebAuxKey, true, 2, "REFRESH_ERROR"},
	} {
		t.Run(tc.branch+tc.field, func(t *testing.T) {
			ctx := context.Background()
			creds := durabilityCredentials()
			s, _ := creds.LoadCredentialSnapshot(ctx, 24)
			repo := &r24RacingRepo{r24Repo: &r24Repo{sessionFakeCredentials: creds}, changeField: tc.field, every: tc.every}
			p := &conditionalConsumerWebPSIDTSPersister{repo: repo, encryptor: consumerWebTransportEncryptor{}}
			m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Credentials: creds, Persister: p})
			defer m.Close()
			_, branch, err := m.reconcileRefresh(24, s.Revision, coreIdentity(s.Credentials), "replacement")
			require.Equal(t, tc.branch, branch)
			require.Equal(t, tc.calls, repo.calls)
			if tc.branch == "CAS_CONFLICT_CORE_UNCHANGED_RECONCILED" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			final, _ := creds.LoadCredentialSnapshot(ctx, 24)
			ns := final.Credentials[ConsumerWebCredentialNamespace].(map[string]any)
			require.Equal(t, "concurrent-winner", ns[tc.field])
			if err == nil {
				v, e := (consumerWebTransportEncryptor{}).Decrypt(ns[ConsumerWebPSIDTSCiphertextKey].(string))
				require.NoError(t, e)
				require.True(t, v == "replacement")
			}
		})
	}
}

func TestConsumerWebR24AuxPersistenceAndTimer(t *testing.T) {
	ctx := context.Background()
	creds := durabilityCredentials()
	repo := &r24Repo{sessionFakeCredentials: creds}
	p := &conditionalConsumerWebPSIDTSPersister{repo: repo, encryptor: consumerWebTransportEncryptor{}}
	clock := &r24Clock{}
	tr := &durabilityTransport{}
	m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: p, Clock: clock})
	defer m.Close()
	a := testSessionAccount(24)
	_, err := m.AcquireSession(ctx, a)
	require.NoError(t, err)
	before, _ := creds.LoadCredentialSnapshot(ctx, a.ID)
	after, err := p.PersistAuxiliary(ctx, a.ID, before.Revision, []consumerWebAuxEntry{{Source: consumerWebAppURL, Cookie: http.Cookie{Name: "synthetic_aux", Value: "synthetic"}}})
	require.NoError(t, err)
	require.NotEqual(t, before.Revision, after.Revision)
	require.True(t, coreIdentity(before.Credentials) == coreIdentity(after.Credentials))
	clock.fire(t)
	require.Equal(t, ConsumerWebSessionAuthenticated, m.State(a.ID))
	require.Zero(t, tr.closes.Load())
	require.Equal(t, 1, clock.active())
	clock.fire(t)
	require.Equal(t, int32(2), tr.rotates.Load())
	require.Equal(t, 1, clock.active())
}

func TestConsumerWebR24CoreAndAuxConflicts(t *testing.T) {
	for _, field := range []string{consumerWebAuxKey, ConsumerWebPSIDCiphertextKey, ConsumerWebPSIDTSCiphertextKey} {
		for _, replacement := range []string{"", "rotated"} {
			t.Run(field+replacement, func(t *testing.T) {
				ctx := context.Background()
				creds := durabilityCredentials()
				tr := &durabilityTransport{replacement: replacement}
				p := &durabilityPersister{creds: creds}
				clock := &r24Clock{}
				m := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{Transport: tr, Credentials: creds, Persister: p, Clock: clock})
				defer m.Close()
				a := testSessionAccount(25)
				_, err := m.AcquireSession(ctx, a)
				require.NoError(t, err)
				s, _ := creds.LoadCredentialSnapshot(ctx, a.ID)
				_, err = creds.CompareAndSwapCredentials(ctx, a.ID, s.Revision, UpdateAccountCredentialNamespace(s.Credentials, ConsumerWebCredentialNamespace, map[string]any{field: "winner"}))
				require.NoError(t, err)
				clock.fire(t)
				after, _ := creds.LoadCredentialSnapshot(ctx, a.ID)
				ns := after.Credentials[ConsumerWebCredentialNamespace].(map[string]any)
				require.Equal(t, "winner", ns[field])
				if field == consumerWebAuxKey {
					require.Equal(t, ConsumerWebSessionAuthenticated, m.State(a.ID))
					require.Equal(t, 1, clock.active())
					if replacement != "" {
						require.Equal(t, int32(2), p.calls.Load())
						require.Equal(t, replacement, ns[ConsumerWebPSIDTSCiphertextKey])
					}
				} else {
					require.Equal(t, ConsumerWebSessionEmpty, m.State(a.ID))
					require.Zero(t, clock.active())
					if replacement != "" {
						require.Equal(t, int32(0), p.calls.Load()) // R25 rejects obsolete core before Rotate.
					}
				}
			})
		}
	}
}

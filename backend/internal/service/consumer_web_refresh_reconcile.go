package service

import (
	"context"
	"errors"
	"reflect"
)

// Only encrypted core fields are retained. Never format this identity. A
// re-encryption is conservatively treated as an edit; no secret is decrypted.
type consumerWebCoreIdentity struct{ psid, psidts string }

func coreIdentity(credentials map[string]any) consumerWebCoreIdentity {
	ns, _ := credentials[ConsumerWebCredentialNamespace].(map[string]any)
	p, _ := ns[ConsumerWebPSIDCiphertextKey].(string)
	ts, _ := ns[ConsumerWebPSIDTSCiphertextKey].(string)
	return consumerWebCoreIdentity{p, ts}
}

type consumerWebRotationSnapshotPersister interface {
	PersistPSIDTSSnapshot(context.Context, int64, any, string) (*AccountCredentialSnapshot, error)
}

func (p *conditionalConsumerWebPSIDTSPersister) PersistPSIDTSSnapshot(ctx context.Context, id int64, rev any, value string) (*AccountCredentialSnapshot, error) {
	patch, err := EncryptConsumerWebCredentialPatch(p.encryptor, nil, &value)
	if err != nil {
		return nil, err
	}
	var expected consumerWebCoreIdentity
	next, err := MutateAccountCredentialsConditional(ctx, p.repo, id, rev, func(current map[string]any) (map[string]any, error) {
		updated := UpdateAccountCredentialNamespace(current, ConsumerWebCredentialNamespace, patch)
		expected = coreIdentity(updated)
		return updated, nil
	})
	// Repository reload may see a concurrent core winner after our successful CAS.
	if err == nil && (next == nil || coreIdentity(next.Credentials) != expected) {
		return nil, ErrCredentialCASConflict
	}
	return next, err
}

func (m *ConsumerWebSessionManager) reconcileRefresh(id int64, rev any, core consumerWebCoreIdentity, replacement string) (*AccountCredentialSnapshot, string, error) {
	if replacement == "" {
		latest, err := m.credentials.LoadCredentialSnapshot(m.root, id)
		if err != nil || latest == nil {
			return nil, "REFRESH_ERROR", ErrConsumerWebPersistence
		}
		if coreIdentity(latest.Credentials) != core {
			return nil, "CORE_CREDENTIAL_CHANGED_SESSION_STALE", ErrCredentialCASConflict
		}
		branch := "NO_REPLACEMENT_CORE_UNCHANGED"
		if !reflect.DeepEqual(rev, latest.Revision) {
			branch = "REVISION_CHANGED_CORE_UNCHANGED_RECONCILED"
		}
		return latest, branch, nil
	}
	p, ok := m.persister.(consumerWebRotationSnapshotPersister)
	if !ok {
		return nil, "REFRESH_ERROR", ErrConsumerWebPersistence
	}
	next, err := p.PersistPSIDTSSnapshot(m.root, id, rev, replacement)
	if err == nil {
		return next, "REFRESH_OK", nil
	}
	if !errors.Is(err, ErrCredentialCASConflict) {
		return nil, "REFRESH_ERROR", err
	}
	latest, loadErr := m.credentials.LoadCredentialSnapshot(m.root, id)
	if loadErr != nil || latest == nil {
		return nil, "REFRESH_ERROR", ErrConsumerWebPersistence
	}
	if coreIdentity(latest.Credentials) != core {
		return nil, "CAS_CONFLICT_CORE_CHANGED_ABORTED", ErrCredentialCASConflict
	}
	// Exactly one reconciliation write; retain full revision CAS and patch only TS.
	next, err = p.PersistPSIDTSSnapshot(m.root, id, latest.Revision, replacement)
	if err == nil {
		return next, "CAS_CONFLICT_CORE_UNCHANGED_RECONCILED", nil
	}
	if errors.Is(err, ErrCredentialCASConflict) {
		winner, e := m.credentials.LoadCredentialSnapshot(m.root, id)
		if e == nil && winner != nil && coreIdentity(winner.Credentials) != core {
			return nil, "CAS_CONFLICT_CORE_CHANGED_ABORTED", err
		}
	}
	return nil, "REFRESH_ERROR", err
}

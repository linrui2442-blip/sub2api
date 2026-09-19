package service

import (
	"context"
	"errors"
	"log/slog"
)

var ErrCredentialCASConflict = errors.New("account credential compare-and-swap conflict")

const MaxCredentialMutationAttempts = 3

// AccountCredentialSnapshot couples decoded credentials with an opaque,
// repository-owned comparison token. Callers must only pass Revision back to
// CompareAndSwapCredentials; it is deliberately typed as any so the raw
// database representation cannot be inspected outside the repository package.
type AccountCredentialSnapshot struct {
	Credentials map[string]any
	Revision    any
}

// AtomicAccountCredentialsRepository is a narrow optional extension of
// AccountRepository. It avoids forcing read-only test doubles to implement
// persistence while production repositories provide DB-level CAS semantics.
type AtomicAccountCredentialsRepository interface {
	LoadCredentialSnapshot(ctx context.Context, accountID int64) (*AccountCredentialSnapshot, error)
	CompareAndSwapCredentials(ctx context.Context, accountID int64, expectedRevision any, credentials map[string]any) (*AccountCredentialSnapshot, error)
}

type AccountCredentialMutator func(current map[string]any) (map[string]any, error)

func AtomicCredentialsRepository(repo AccountRepository) (AtomicAccountCredentialsRepository, bool) {
	atomicRepo, ok := any(repo).(AtomicAccountCredentialsRepository)
	return atomicRepo, ok
}

// MutateAccountCredentialsReplaySafe reloads and reapplies a logical patch on
// CAS conflict. It is only suitable for replay-safe user/provider intent.
func MutateAccountCredentialsReplaySafe(ctx context.Context, repo AccountRepository, accountID int64, mutator AccountCredentialMutator) (*AccountCredentialSnapshot, error) {
	atomicRepo, ok := AtomicCredentialsRepository(repo)
	if !ok {
		return nil, errors.New("atomic account credential persistence is unavailable")
	}
	for attempt := 0; attempt < MaxCredentialMutationAttempts; attempt++ {
		snapshot, err := atomicRepo.LoadCredentialSnapshot(ctx, accountID)
		if err != nil {
			return nil, err
		}
		next, err := mutator(cloneCredentialMap(snapshot.Credentials))
		if err != nil {
			return nil, err
		}
		updated, err := atomicRepo.CompareAndSwapCredentials(ctx, accountID, snapshot.Revision, next)
		if err == nil {
			return updated, nil
		}
		if !errors.Is(err, ErrCredentialCASConflict) {
			return nil, err
		}
	}
	return nil, ErrCredentialCASConflict
}

// MutateAccountCredentialsConditional performs exactly one CAS. Session-bound
// results must use this API so a conflict discards the stale result.
func MutateAccountCredentialsConditional(ctx context.Context, repo AccountRepository, accountID int64, expectedRevision any, mutator AccountCredentialMutator) (*AccountCredentialSnapshot, error) {
	atomicRepo, ok := AtomicCredentialsRepository(repo)
	if !ok {
		return nil, errors.New("atomic account credential persistence is unavailable")
	}
	snapshot, err := atomicRepo.LoadCredentialSnapshot(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// The mutation uses current decoded data, but the write remains guarded by
	// the caller's bootstrap revision. A stale revision can never be upgraded.
	next, err := mutator(cloneCredentialMap(snapshot.Credentials))
	if err != nil {
		return nil, err
	}
	return atomicRepo.CompareAndSwapCredentials(ctx, accountID, expectedRevision, next)
}

func mergeCredentialPatch(current, patch map[string]any) map[string]any {
	out := cloneCredentialMap(current)
	for key, value := range patch {
		if key == ConsumerWebCredentialNamespace {
			if nested, ok := value.(map[string]any); ok {
				out = UpdateAccountCredentialNamespace(out, ConsumerWebCredentialNamespace, nested)
				continue
			}
		}
		out[key] = value
	}
	return out
}

type accountCredentialsUpdater interface {
	UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error
}

func persistAccountCredentials(ctx context.Context, repo AccountRepository, account *Account, credentials map[string]any) error {
	if repo == nil || account == nil {
		return nil
	}

	// 安全不变量:spark 影子账号恒不持凭据(凭据透传母账号)。这是凭据写入的唯一汇聚点
	// (token 刷新 / 订阅补全 / 账户导入后刷新等全部经此),在此对影子早返 no-op 是
	// defense-in-depth——即便某条上游路径漏判,也不会把凭据落到影子行(外审第6轮 P1)。
	if account.IsCredentialShadow() {
		slog.Warn("skip persisting credentials to spark shadow account",
			"account_id", account.ID, "parent_id", *account.ParentAccountID)
		return nil
	}

	// Provider refresh results own provider fields, not the Consumer Web
	// namespace. Reapply their patch to the latest DB snapshot so concurrent
	// Consumer Web updates cannot be lost.
	patch := shallowCopyMap(credentials)
	delete(patch, ConsumerWebCredentialNamespace)
	if _, ok := AtomicCredentialsRepository(repo); ok {
		updated, err := MutateAccountCredentialsReplaySafe(ctx, repo, account.ID, func(current map[string]any) (map[string]any, error) {
			return mergeCredentialPatch(current, patch), nil
		})
		if err != nil {
			return err
		}
		account.Credentials = cloneCredentialMap(updated.Credentials)
		return nil
	}
	account.Credentials = shallowCopyMap(credentials)
	if updater, ok := any(repo).(accountCredentialsUpdater); ok {
		return updater.UpdateCredentials(ctx, account.ID, account.Credentials)
	}
	return repo.Update(ctx, account)
}

// sparkShadowAllowedCredentialKeys 是 spark 影子账号唯一可写的凭据键集合(仅模型映射)。
// 校验(isAllowed)与 sanitize 共用此单一来源,避免两处独立硬编码列表漂移。
var sparkShadowAllowedCredentialKeys = map[string]struct{}{
	"model_mapping":         {},
	"compact_model_mapping": {},
}

func isAllowedSparkShadowCredentialsUpdate(credentials map[string]any) bool {
	if credentials == nil {
		return true
	}
	for key := range credentials {
		if _, ok := sparkShadowAllowedCredentialKeys[key]; !ok {
			return false
		}
	}
	return true
}

func sanitizeSparkShadowCredentials(credentials map[string]any) map[string]any {
	if len(credentials) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(sparkShadowAllowedCredentialKeys))
	for key := range sparkShadowAllowedCredentialKeys {
		if value, ok := credentials[key]; ok && value != nil {
			out[key] = value
		}
	}
	return out
}

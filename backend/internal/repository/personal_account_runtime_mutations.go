package repository

import (
	"context"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const personalCodexFingerprintSeedKey = "codex_fingerprint_seed"

// SetTempUnschedulable replaces the upstream NOW()-based conditional update.
// The later deadline wins, matching the upstream monotonic cooldown behaviour.
func (r *personalAccountRepository) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	account, err := r.base.client.Account.Query().Where(dbaccount.IDEQ(id)).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrAccountNotFound
		}
		return err
	}
	if account.TempUnschedulableUntil != nil && !account.TempUnschedulableUntil.Before(until) {
		return nil
	}
	if _, err := r.base.client.Account.UpdateOneID(id).
		SetTempUnschedulableUntil(until).
		SetTempUnschedulableReason(reason).
		Save(ctx); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *personalAccountRepository) ClearTempUnschedulable(ctx context.Context, id int64) error {
	if _, err := r.base.client.Account.UpdateOneID(id).
		ClearTempUnschedulableUntil().
		SetTempUnschedulableReason("").
		Save(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrAccountNotFound
		}
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *personalAccountRepository) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	accounts, err := r.base.client.Account.Query().
		Where(
			dbaccount.SchedulableEQ(true),
			dbaccount.AutoPauseOnExpiredEQ(true),
			dbaccount.ExpiresAtNotNil(),
			dbaccount.ExpiresAtLTE(now),
		).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(accounts) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		if _, err := r.base.client.Account.UpdateOneID(account.ID).SetSchedulable(false).Save(ctx); err != nil {
			return int64(len(ids)), err
		}
		ids = append(ids, account.ID)
	}
	payload := map[string]any{"account_ids": ids}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
		return int64(len(ids)), err
	}
	return int64(len(ids)), nil
}

// UpdateExtra performs a key-level read/merge/write on the single-process
// Personal database. This is intentionally serialized by SQLite's one-connection
// configuration, avoiding the PostgreSQL jsonb operators used upstream.
func (r *personalAccountRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	updates = stripCodexFingerprintSeedFromExtraUpdate(updates)
	if len(updates) == 0 {
		return nil
	}
	account, err := r.base.GetByID(ctx, id)
	if err != nil {
		return err
	}
	extra := copyJSONMap(normalizeJSONMap(account.Extra))
	if extra == nil {
		extra = make(map[string]any, len(updates)+1)
	}
	for key, value := range updates {
		if value == nil {
			delete(extra, key)
			continue
		}
		extra[key] = value
	}

	if service.ShouldEnsureCodexFingerprintSeedForExtraUpdates(updates) {
		seed, _ := extra[personalCodexFingerprintSeedKey].(string)
		parsed, parseErr := uuid.Parse(strings.TrimSpace(seed))
		if parseErr != nil || parsed == uuid.Nil {
			extra[personalCodexFingerprintSeedKey] = uuid.NewString()
		}
	}

	if _, err := r.base.client.Account.UpdateOneID(id).SetExtra(extra).Save(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrAccountNotFound
		}
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *personalAccountRepository) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	account, err := r.base.GetByID(ctx, id)
	if err != nil {
		return err
	}
	extra := copyJSONMap(normalizeJSONMap(account.Extra))
	if extra == nil {
		extra = make(map[string]any)
	}
	limits, _ := extra["model_rate_limits"].(map[string]any)
	limits = copyJSONMap(limits)
	if limits == nil {
		limits = make(map[string]any)
	}
	now := time.Now().UTC()
	entry := map[string]any{
		"rate_limited_at":     now.Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
	if len(reason) > 0 && strings.TrimSpace(reason[0]) != "" {
		entry["reason"] = strings.TrimSpace(reason[0])
	}
	limits[scope] = entry
	extra["model_rate_limits"] = limits
	if _, err := r.base.client.Account.UpdateOneID(id).SetExtra(extra).Save(ctx); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *personalAccountRepository) ClearModelRateLimits(ctx context.Context, id int64) error {
	account, err := r.base.GetByID(ctx, id)
	if err != nil {
		return err
	}
	extra := copyJSONMap(normalizeJSONMap(account.Extra))
	delete(extra, "model_rate_limits")
	if _, err := r.base.client.Account.UpdateOneID(id).SetExtra(extra).Save(ctx); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *personalAccountRepository) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	account, err := r.base.GetByID(ctx, id)
	if err != nil {
		return err
	}
	extra := copyJSONMap(normalizeJSONMap(account.Extra))
	delete(extra, "antigravity_quota_scopes")
	if _, err := r.base.client.Account.UpdateOneID(id).SetExtra(extra).Save(ctx); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

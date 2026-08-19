package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// UpdateCredentials is the Personal Edition credential persistence path used by
// OAuthRefreshAPI / TokenRefreshService after a successful GPT/Gemini refresh.
// The upstream implementation deliberately performs PostgreSQL jsonb cleanup;
// Personal Edition does not run those SaaS billing/Ollama probes, so a direct
// Ent update is both sufficient and dialect-safe.
func (r *personalAccountRepository) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	updated, err := r.base.client.Account.UpdateOneID(id).
		SetCredentials(normalizeJSONMap(credentials)).
		Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrAccountNotFound
		}
		return err
	}

	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	r.base.syncSchedulerAccountSnapshot(ctx, id)
	_ = updated
	return nil
}

// Update replaces the upstream PostgreSQL row-lock/jsonb merge path for normal
// Personal Edition account edits. Personal mode intentionally has no upstream
// billing-probe or Ollama-cloud business state to preserve, so the account's
// explicit domain fields can be written directly through Ent.
func (r *personalAccountRepository) Update(ctx context.Context, account *service.Account) error {
	if account == nil {
		return nil
	}

	schedulable := account.Schedulable
	if account.Status == service.StatusError {
		schedulable = false
	}

	builder := r.base.client.Account.UpdateOneID(account.ID).
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetConcurrency(account.Concurrency).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired).
		SetQuotaDimension(dbaccount.QuotaDimension(account.QuotaDimensionOrDefault())).
		SetNillableParentAccountID(account.ParentAccountID)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	} else {
		builder.ClearLoadFactor()
	}
	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	} else {
		builder.ClearProxyID()
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	} else {
		builder.ClearLastUsedAt()
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	} else {
		builder.ClearRateLimitedAt()
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	} else {
		builder.ClearRateLimitResetAt()
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	} else {
		builder.ClearOverloadUntil()
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	} else {
		builder.ClearSessionWindowStart()
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	} else {
		builder.ClearSessionWindowEnd()
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	} else {
		builder.ClearSessionWindowStatus()
	}
	if account.Notes == nil {
		builder.ClearNotes()
	}

	updated, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrAccountNotFound
		}
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.base.sql, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		return err
	}
	account.UpdatedAt = updated.UpdatedAt
	account.Schedulable = schedulable
	r.base.syncSchedulerAccountSnapshot(ctx, account.ID)
	return nil
}

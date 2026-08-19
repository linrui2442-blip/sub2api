package repository

import (
	"context"
	"database/sql"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// personalAdminAccountRepository keeps the full upstream admin repository
// surface but routes scheduler/OAuth/account-edit operations through the same
// SQLite-safe adapter used by the gateway runtime.
type personalAdminAccountRepository struct {
	service.AdminAccountRepository
	compat *personalAccountRepository
}

func NewPersonalAwareAdminAccountRepository(client *dbent.Client, sqlDB *sql.DB, schedulerCache service.SchedulerCache) service.AdminAccountRepository {
	base := newAccountRepositoryWithSQL(client, sqlDB, schedulerCache)
	if !personal.Enabled() {
		return base
	}
	compat := &personalAccountRepository{AccountRepository: base, base: base}
	return &personalAdminAccountRepository{AdminAccountRepository: base, compat: compat}
}

func (r *personalAdminAccountRepository) Update(ctx context.Context, account *service.Account) error {
	return r.compat.Update(ctx, account)
}

func (r *personalAdminAccountRepository) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	return r.compat.UpdateCredentials(ctx, id, credentials)
}

func (r *personalAdminAccountRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	return r.compat.UpdateExtra(ctx, id, updates)
}

func (r *personalAdminAccountRepository) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	return r.compat.SetTempUnschedulable(ctx, id, until, reason)
}

func (r *personalAdminAccountRepository) ClearTempUnschedulable(ctx context.Context, id int64) error {
	return r.compat.ClearTempUnschedulable(ctx, id)
}

func (r *personalAdminAccountRepository) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	return r.compat.AutoPauseExpiredAccounts(ctx, now)
}

func (r *personalAdminAccountRepository) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	return r.compat.SetModelRateLimit(ctx, id, scope, resetAt, reason...)
}

func (r *personalAdminAccountRepository) ClearModelRateLimits(ctx context.Context, id int64) error {
	return r.compat.ClearModelRateLimits(ctx, id)
}

func (r *personalAdminAccountRepository) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	return r.compat.ClearAntigravityQuotaScopes(ctx, id)
}

func (r *personalAdminAccountRepository) ListSchedulable(ctx context.Context) ([]service.Account, error) {
	return r.compat.ListSchedulable(ctx)
}

func (r *personalAdminAccountRepository) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	return r.compat.ListSchedulableByGroupID(ctx, groupID)
}

func (r *personalAdminAccountRepository) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.compat.ListSchedulableByPlatform(ctx, platform)
}

func (r *personalAdminAccountRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	return r.compat.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
}

func (r *personalAdminAccountRepository) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	return r.compat.ListSchedulableByPlatforms(ctx, platforms)
}

func (r *personalAdminAccountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	return r.compat.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, platforms)
}

func (r *personalAdminAccountRepository) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	return r.compat.ListSchedulableUngroupedByPlatform(ctx, platform)
}

func (r *personalAdminAccountRepository) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	return r.compat.ListSchedulableUngroupedByPlatforms(ctx, platforms)
}

func (r *personalAdminAccountRepository) ListSchedulableAccountLoads(ctx context.Context) ([]service.AccountWithConcurrency, error) {
	return r.compat.ListSchedulableAccountLoads(ctx)
}

func (r *personalAdminAccountRepository) ListSchedulableCapacityByGroupIDs(ctx context.Context, groupIDs []int64) ([]service.GroupAccountCapacityRow, error) {
	return r.compat.ListSchedulableCapacityByGroupIDs(ctx, groupIDs)
}

func (r *personalAdminAccountRepository) ListOAuthRefreshCandidatePage(ctx context.Context, options service.OAuthRefreshPageOptions) (*service.OAuthRefreshCandidatePage, error) {
	return r.compat.ListOAuthRefreshCandidatePage(ctx, options)
}

// Personal Edition disables commercial billing probes. Account editing still
// accepts a manual rate value for upstream UI compatibility, but there is no
// background probe/rate-sync ownership to merge or preserve.
func (r *personalAdminAccountRepository) UpdateWithAccountBillingSettings(
	ctx context.Context,
	account *service.Account,
	_ *bool,
	_ *bool,
	rateMultiplier *float64,
) error {
	if account != nil && rateMultiplier != nil {
		account.RateMultiplier = rateMultiplier
	}
	return r.compat.Update(ctx, account)
}

package repository

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// personalAccountRepository keeps the upstream AccountRepository contract while
// replacing the handful of PostgreSQL-specific read paths that are critical to
// scheduler rebuilds and OAuth refresh in Personal Edition. All normal CRUD and
// provider-specific mutations continue to delegate to the upstream repository.
//
// Personal Edition is intentionally small (one owner + a few private members
// and a small GPT/Gemini account pool), so these compatibility paths favour
// simple, explicit Go filtering over PostgreSQL-only JSON/ANY/NOW expressions.
type personalAccountRepository struct {
	service.AccountRepository
	base *accountRepository
}

// NewPersonalAwareAccountRepository is the Wire provider for the general
// AccountRepository dependency. Upstream Standard/Simple runtimes receive the
// original repository unchanged; Personal Edition receives the compatibility
// adapter.
func NewPersonalAwareAccountRepository(client *dbent.Client, sqlDB *sql.DB, schedulerCache service.SchedulerCache) service.AccountRepository {
	base := newAccountRepositoryWithSQL(client, sqlDB, schedulerCache)
	if !personal.Enabled() {
		return base
	}
	return &personalAccountRepository{AccountRepository: base, base: base}
}

func personalAccountSchedulable(account *service.Account, now time.Time) bool {
	if account == nil || account.Status != service.StatusActive || !account.Schedulable {
		return false
	}
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		return false
	}
	if account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt) {
		return false
	}
	if account.OverloadUntil != nil && now.Before(*account.OverloadUntil) {
		return false
	}
	if account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
		return false
	}
	return true
}

func filterPersonalSchedulable(accounts []service.Account, now time.Time, platformSet map[string]struct{}, requireUngrouped bool) []service.Account {
	out := make([]service.Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if !personalAccountSchedulable(account, now) {
			continue
		}
		if len(platformSet) > 0 {
			if _, ok := platformSet[account.Platform]; !ok {
				continue
			}
		}
		if requireUngrouped && len(account.GroupIDs) != 0 {
			continue
		}
		out = append(out, *account)
	}
	return out
}

func personalPlatformSet(platforms []string) map[string]struct{} {
	if len(platforms) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		if platform = strings.TrimSpace(platform); platform != "" {
			set[platform] = struct{}{}
		}
	}
	return set
}

func (r *personalAccountRepository) ListSchedulable(ctx context.Context) ([]service.Account, error) {
	accounts, err := r.base.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), nil, false), nil
}

func (r *personalAccountRepository) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, err := r.base.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), nil, false), nil
}

func (r *personalAccountRepository) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	accounts, err := r.base.ListByPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), personalPlatformSet([]string{platform}), false), nil
}

func (r *personalAccountRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	accounts, err := r.base.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), personalPlatformSet([]string{platform}), false), nil
}

func (r *personalAccountRepository) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return []service.Account{}, nil
	}
	accounts, err := r.base.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), personalPlatformSet(platforms), false), nil
}

func (r *personalAccountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return []service.Account{}, nil
	}
	accounts, err := r.base.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), personalPlatformSet(platforms), false), nil
}

func (r *personalAccountRepository) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	accounts, err := r.base.ListByPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), personalPlatformSet([]string{platform}), true), nil
}

func (r *personalAccountRepository) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return []service.Account{}, nil
	}
	accounts, err := r.base.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	return filterPersonalSchedulable(accounts, time.Now(), personalPlatformSet(platforms), true), nil
}

// ListSchedulableAccountLoads preserves the narrow projection capability used
// by ops metrics without executing the upstream NOW()-based predicate on
// SQLite. Personal pools are small, so a filtered in-memory projection is
// cheaper than maintaining a second dialect-specific SQL query.
func (r *personalAccountRepository) ListSchedulableAccountLoads(ctx context.Context) ([]service.AccountWithConcurrency, error) {
	accounts, err := r.ListSchedulable(ctx)
	if err != nil {
		return nil, err
	}
	loads := make([]service.AccountWithConcurrency, 0, len(accounts))
	for i := range accounts {
		loads = append(loads, service.AccountWithConcurrency{
			ID:             accounts[i].ID,
			MaxConcurrency: accounts[i].EffectiveLoadFactor(),
		})
	}
	return loads, nil
}

// ListSchedulableCapacityByGroupIDs avoids the PostgreSQL ANY/jsonb projection
// used by the upstream bulk path. The sequential implementation is more than
// sufficient for a Personal Edition with only a handful of groups/accounts.
func (r *personalAccountRepository) ListSchedulableCapacityByGroupIDs(ctx context.Context, groupIDs []int64) ([]service.GroupAccountCapacityRow, error) {
	rows := make([]service.GroupAccountCapacityRow, 0)
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		accounts, err := r.ListSchedulableByGroupID(ctx, groupID)
		if err != nil {
			return nil, err
		}
		for i := range accounts {
			account := &accounts[i]
			rows = append(rows, service.GroupAccountCapacityRow{
				GroupID:             groupID,
				AccountID:           account.ID,
				Concurrency:         account.Concurrency,
				Extra:               account.Extra,
				SessionWindowStart:  account.SessionWindowStart,
				SessionWindowEnd:    account.SessionWindowEnd,
				SessionWindowStatus: account.SessionWindowStatus,
			})
		}
	}
	return rows, nil
}

// ListOAuthRefreshCandidatePage is the Personal SQLite equivalent of the
// upstream PostgreSQL candidate scan (ANY, jsonb operators, btrim, NOW()).
// It deliberately keeps the same eligibility semantics but performs the small
// final filter in Go.
func (r *personalAccountRepository) ListOAuthRefreshCandidatePage(ctx context.Context, options service.OAuthRefreshPageOptions) (*service.OAuthRefreshCandidatePage, error) {
	if len(options.Platforms) == 0 {
		return &service.OAuthRefreshCandidatePage{Accounts: []service.Account{}}, nil
	}
	if options.Limit <= 0 {
		return &service.OAuthRefreshCandidatePage{Accounts: []service.Account{}}, nil
	}

	accounts, err := r.base.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	platforms := personalPlatformSet(options.Platforms)
	now := time.Now()
	eligible := make([]service.Account, 0, options.Limit+1)

	for i := range accounts {
		account := accounts[i]
		if account.ID <= options.AfterID || !account.Schedulable {
			continue
		}
		if _, ok := platforms[account.Platform]; !ok {
			continue
		}
		if options.ActiveOnly && account.Status != service.StatusActive {
			continue
		}
		if options.IncludeSetupToken {
			if account.Type != service.AccountTypeOAuth && account.Type != service.AccountTypeSetupToken {
				continue
			}
		} else if account.Type != service.AccountTypeOAuth {
			continue
		}
		if options.RequireRefreshToken {
			refreshToken, _ := account.Credentials["refresh_token"].(string)
			if strings.TrimSpace(refreshToken) == "" {
				continue
			}
		}
		if options.ExcludeRetryCooldown &&
			account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) &&
			strings.HasPrefix(account.TempUnschedulableReason, "token refresh retry exhausted:") {
			continue
		}
		eligible = append(eligible, account)
		if len(eligible) > options.Limit {
			break
		}
	}

	page := &service.OAuthRefreshCandidatePage{Accounts: []service.Account{}}
	if len(eligible) == 0 {
		return page, nil
	}
	if len(eligible) > options.Limit {
		page.HasMore = true
		eligible = eligible[:options.Limit]
	}
	page.Accounts = eligible
	page.NextAfterID = eligible[len(eligible)-1].ID
	return page, nil
}

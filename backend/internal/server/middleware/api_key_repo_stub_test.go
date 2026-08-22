//go:build unit

package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type stubApiKeyRepo struct {
	getByKey func(context.Context, string) (*service.APIKey, error)
}

func (r *stubApiKeyRepo) Create(context.Context, *service.APIKey) error {
	return errors.New("unexpected Create")
}
func (r *stubApiKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	return nil, errors.New("unexpected GetByID")
}
func (r *stubApiKeyRepo) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	return "", 0, errors.New("unexpected GetKeyAndOwnerID")
}
func (r *stubApiKeyRepo) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
	return r.GetByKeyForAuth(ctx, key)
}
func (r *stubApiKeyRepo) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	if r.getByKey == nil {
		return nil, errors.New("unexpected GetByKeyForAuth")
	}
	return r.getByKey(ctx, key)
}
func (r *stubApiKeyRepo) Update(context.Context, *service.APIKey, service.APIKeyUpdateFields) error {
	return errors.New("unexpected Update")
}
func (r *stubApiKeyRepo) Delete(context.Context, int64) error { return errors.New("unexpected Delete") }
func (r *stubApiKeyRepo) DeleteWithAudit(context.Context, int64) error {
	return errors.New("unexpected DeleteWithAudit")
}
func (r *stubApiKeyRepo) ListByUserID(context.Context, int64, pagination.PaginationParams, service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	return nil, nil, errors.New("unexpected ListByUserID")
}
func (r *stubApiKeyRepo) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	return nil, errors.New("unexpected VerifyOwnership")
}
func (r *stubApiKeyRepo) CountByUserID(context.Context, int64) (int64, error) {
	return 0, errors.New("unexpected CountByUserID")
}
func (r *stubApiKeyRepo) ExistsByKey(context.Context, string) (bool, error) {
	return false, errors.New("unexpected ExistsByKey")
}
func (r *stubApiKeyRepo) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]service.APIKey, *pagination.PaginationResult, error) {
	return nil, nil, errors.New("unexpected ListByGroupID")
}
func (r *stubApiKeyRepo) SearchAPIKeys(context.Context, int64, string, int) ([]service.APIKey, error) {
	return nil, errors.New("unexpected SearchAPIKeys")
}
func (r *stubApiKeyRepo) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	return 0, errors.New("unexpected ClearGroupIDByGroupID")
}
func (r *stubApiKeyRepo) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	return 0, errors.New("unexpected UpdateGroupIDByUserAndGroup")
}
func (r *stubApiKeyRepo) CountByGroupID(context.Context, int64) (int64, error) {
	return 0, errors.New("unexpected CountByGroupID")
}
func (r *stubApiKeyRepo) ListKeysByUserID(context.Context, int64) ([]string, error) {
	return nil, errors.New("unexpected ListKeysByUserID")
}
func (r *stubApiKeyRepo) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	return nil, errors.New("unexpected ListKeysByGroupID")
}
func (r *stubApiKeyRepo) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	return 0, errors.New("unexpected IncrementQuotaUsed")
}
func (r *stubApiKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error        { return nil }
func (r *stubApiKeyRepo) IncrementRateLimitUsage(context.Context, int64, float64) error { return nil }
func (r *stubApiKeyRepo) ResetRateLimitWindows(context.Context, int64) error            { return nil }
func (r *stubApiKeyRepo) GetRateLimitData(context.Context, int64) (*service.APIKeyRateLimitData, error) {
	return nil, nil
}

type fakeAPIKeyRepo = stubApiKeyRepo

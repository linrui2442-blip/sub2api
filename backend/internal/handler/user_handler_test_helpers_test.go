//go:build unit

package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// userHandlerRepoStub embeds the full repository contract so focused handler
// tests need to implement only the local-profile paths they exercise.
type userHandlerRepoStub struct {
	service.UserRepository
	user       *service.User
	identities []service.UserAuthIdentityRecord
	avatar     *service.UserAvatar
}

func (s *userHandlerRepoStub) GetByID(context.Context, int64) (*service.User, error) {
	clone := *s.user
	return &clone, nil
}

func (s *userHandlerRepoStub) GetByEmail(context.Context, string) (*service.User, error) {
	return s.GetByID(context.Background(), 0)
}

func (s *userHandlerRepoStub) GetFirstAdmin(context.Context) (*service.User, error) {
	return s.GetByID(context.Background(), 0)
}

func (s *userHandlerRepoStub) Update(_ context.Context, user *service.User, _ service.UserUpdateFields) error {
	clone := *user
	s.user = &clone
	return nil
}

func (s *userHandlerRepoStub) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	if s.avatar == nil {
		return nil, nil
	}
	clone := *s.avatar
	return &clone, nil
}

func (s *userHandlerRepoStub) UpsertUserAvatar(_ context.Context, _ int64, input service.UpsertUserAvatarInput) (*service.UserAvatar, error) {
	s.avatar = &service.UserAvatar{URL: input.URL, StorageProvider: input.StorageProvider, StorageKey: input.StorageKey}
	return s.avatar, nil
}

func (s *userHandlerRepoStub) ListUserAuthIdentities(context.Context, int64) ([]service.UserAuthIdentityRecord, error) {
	return append([]service.UserAuthIdentityRecord(nil), s.identities...), nil
}

type userHandlerRefreshTokenCacheStub struct {
	service.RefreshTokenCache
	revokedUserIDs []int64
}

func (s *userHandlerRefreshTokenCacheStub) DeleteUserRefreshTokens(_ context.Context, userID int64) error {
	s.revokedUserIDs = append(s.revokedUserIDs, userID)
	return nil
}

func (s *userHandlerRefreshTokenCacheStub) DeleteTokenFamily(context.Context, string) error {
	return nil
}
func (s *userHandlerRefreshTokenCacheStub) StoreRefreshToken(context.Context, string, *service.RefreshTokenData, time.Duration) error {
	return nil
}

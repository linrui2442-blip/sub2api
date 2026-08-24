//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type privacyTokenProviderStub struct {
	token          string
	recoveredToken string
	err            error
	calls          int
	recoverCalls   int
}

func (s *privacyTokenProviderStub) GetAccessToken(context.Context, *Account) (string, error) {
	s.calls++
	return s.token, s.err
}

func (s *privacyTokenProviderStub) RecoverRejectedAccessToken(context.Context, *Account, string) (string, error) {
	s.calls++
	s.recoverCalls++
	if s.recoveredToken != "" {
		return s.recoveredToken, s.err
	}
	return s.token, s.err
}

func TestEnsureAntigravityPrivacyFirst401RecoversAndReplaysOnce(t *testing.T) {
	provider := &privacyTokenProviderStub{token: "rejected-token", recoveredToken: "fresh-token"}
	repo := &privacyAccountRepoStub{}
	var tokens []string
	service := &adminServiceImpl{
		accountRepo: repo, antigravityTokenProvider: provider,
		antigravityPrivacySetter: func(_ context.Context, token, _, _ string) (string, error) {
			tokens = append(tokens, token)
			if len(tokens) == 1 {
				return "", errors.New("HTTP 401 UNAUTHENTICATED")
			}
			return AntigravityPrivacySet, nil
		},
	}
	account := &Account{ID: 104, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": "project-104"}}

	mode := service.EnsureAntigravityPrivacy(context.Background(), account)

	require.Equal(t, AntigravityPrivacySet, mode)
	require.Equal(t, []string{"rejected-token", "fresh-token"}, tokens)
	require.Equal(t, 1, provider.recoverCalls)
}

func TestEnsureAntigravityPrivacySecond401Stops(t *testing.T) {
	provider := &privacyTokenProviderStub{token: "rejected-token", recoveredToken: "fresh-token"}
	privacyCalls := 0
	service := &adminServiceImpl{
		antigravityTokenProvider: provider,
		antigravityPrivacySetter: func(context.Context, string, string, string) (string, error) {
			privacyCalls++
			return "", errors.New("HTTP 401 UNAUTHENTICATED")
		},
	}
	account := &Account{ID: 105, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{}}

	mode := service.EnsureAntigravityPrivacy(context.Background(), account)

	require.Empty(t, mode)
	require.Equal(t, 2, privacyCalls)
	require.Equal(t, 1, provider.recoverCalls)
}

type privacyAccountRepoStub struct {
	AccountRepository
	updates map[string]any
}

func (s *privacyAccountRepoStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	s.updates = updates
	return nil
}

func TestEnsureAntigravityPrivacyUsesTokenProviderResult(t *testing.T) {
	provider := &privacyTokenProviderStub{token: "fresh-provider-token"}
	repo := &privacyAccountRepoStub{}
	var receivedToken string
	service := &adminServiceImpl{
		accountRepo:              repo,
		antigravityTokenProvider: provider,
		antigravityPrivacySetter: func(_ context.Context, token, _, _ string) (string, error) {
			receivedToken = token
			return AntigravityPrivacySet, nil
		},
	}
	account := &Account{
		ID:       101,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "stale-snapshot-token",
			"project_id":   "project-101",
		},
	}

	mode := service.EnsureAntigravityPrivacy(context.Background(), account)

	require.Equal(t, AntigravityPrivacySet, mode)
	require.Equal(t, "fresh-provider-token", receivedToken)
	require.Equal(t, 1, provider.calls)
	require.Equal(t, AntigravityPrivacySet, repo.updates["privacy_mode"])
}

func TestForceAntigravityPrivacyUsesTokenProviderOnce(t *testing.T) {
	provider := &privacyTokenProviderStub{token: "refreshed-token"}
	repo := &privacyAccountRepoStub{}
	var receivedToken string
	service := &adminServiceImpl{
		accountRepo:              repo,
		antigravityTokenProvider: provider,
		antigravityPrivacySetter: func(_ context.Context, token, _, _ string) (string, error) {
			receivedToken = token
			return AntigravityPrivacySet, nil
		},
	}
	account := &Account{
		ID:       102,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "expired-token",
			"project_id":   "project-102",
		},
	}

	mode := service.ForceAntigravityPrivacy(context.Background(), account)

	require.Equal(t, AntigravityPrivacySet, mode)
	require.Equal(t, "refreshed-token", receivedToken)
	require.Equal(t, 1, provider.calls)
	require.Equal(t, AntigravityPrivacySet, repo.updates["privacy_mode"])
}

func TestEnsureAntigravityPrivacyStopsWhenTokenProviderFails(t *testing.T) {
	provider := &privacyTokenProviderStub{err: errors.New("refresh failed")}
	privacyCalls := 0
	service := &adminServiceImpl{
		antigravityTokenProvider: provider,
		antigravityPrivacySetter: func(context.Context, string, string, string) (string, error) {
			privacyCalls++
			return AntigravityPrivacySet, nil
		},
	}
	account := &Account{
		ID:       103,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "stale-token",
		},
	}

	mode := service.EnsureAntigravityPrivacy(context.Background(), account)

	require.Empty(t, mode)
	require.Equal(t, 1, provider.calls)
	require.Zero(t, privacyCalls)
}

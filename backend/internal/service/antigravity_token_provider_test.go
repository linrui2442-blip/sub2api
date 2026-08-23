//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type antigravityProviderCacheStub struct {
	token       string
	setToken    string
	setTTL      time.Duration
	deleteCalls int
}

func (c *antigravityProviderCacheStub) GetAccessToken(context.Context, string) (string, error) {
	return c.token, nil
}
func (c *antigravityProviderCacheStub) SetAccessToken(_ context.Context, _ string, token string, ttl time.Duration) error {
	c.setToken, c.setTTL = token, ttl
	return nil
}
func (c *antigravityProviderCacheStub) DeleteAccessToken(context.Context, string) error {
	c.deleteCalls++
	c.token = ""
	return nil
}
func (c *antigravityProviderCacheStub) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}
func (c *antigravityProviderCacheStub) ReleaseRefreshLock(context.Context, string) error { return nil }

func TestAntigravityTokenProvider_ExpiredDurableTokenBypassesStaleCache(t *testing.T) {
	account := &Account{
		ID: 91, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "expired-db-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(-time.Minute).Unix(), "project_id": "project-91",
		},
	}
	repo := &refreshAPIAccountRepo{account: account}
	cache := &antigravityProviderCacheStub{token: "stale-cache-token"}
	executor := &refreshAPIExecutorStub{
		needsRefresh: true,
		credentials: map[string]any{
			"access_token": "fresh-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(time.Hour).Unix(), "project_id": "project-91",
		},
	}
	provider := NewAntigravityTokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "fresh-token", token)
	require.Equal(t, 1, executor.refreshCalls)
	require.GreaterOrEqual(t, cache.deleteCalls, 1)
	require.Equal(t, "fresh-token", cache.setToken)
	require.Greater(t, cache.setTTL, time.Duration(0))
	require.LessOrEqual(t, cache.setTTL, time.Hour)
}

func TestAntigravityTokenProvider_ValidDurableTokenUsesCacheWithoutRefresh(t *testing.T) {
	account := &Account{
		ID: 92, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "db-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(time.Hour).Unix(), "project_id": "project-92",
		},
	}
	cache := &antigravityProviderCacheStub{token: "valid-cache-token"}
	executor := &refreshAPIExecutorStub{needsRefresh: true}
	provider := NewAntigravityTokenProvider(nil, cache, nil)
	provider.SetRefreshAPI(nil, executor)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "valid-cache-token", token)
	require.Zero(t, executor.refreshCalls)
}

func TestBuildAntigravityDegradedUsage_ReauthSemantics(t *testing.T) {
	tests := []struct {
		name        string
		err         string
		needsReauth bool
	}{
		{name: "invalid grant", err: "refresh failed: invalid_grant", needsReauth: true},
		{name: "transient 401", err: "FetchAvailableModels failed HTTP 401 UNAUTHENTICATED"},
		{name: "rate limited", err: "HTTP 429 RESOURCE_EXHAUSTED"},
		{name: "not found", err: "HTTP 404 NOT_FOUND"},
		{name: "server error", err: "HTTP 503 UNAVAILABLE"},
		{name: "network", err: "dial tcp: i/o timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := buildAntigravityDegradedUsage(stringError(tt.err))
			require.Equal(t, tt.needsReauth, info.NeedsReauth)
		})
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func TestAntigravityTokenProvider_GetAccessToken_Upstream(t *testing.T) {
	provider := &AntigravityTokenProvider{}

	t.Run("upstream account with valid api_key", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
			Credentials: map[string]any{
				"api_key": "sk-test-key-12345",
			},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.NoError(t, err)
		require.Equal(t, "sk-test-key-12345", token)
	})

	t.Run("upstream account missing api_key", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Type:        AccountTypeUpstream,
			Credentials: map[string]any{},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})

	t.Run("upstream account with empty api_key", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
			Credentials: map[string]any{
				"api_key": "",
			},
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})

	t.Run("upstream account with nil credentials", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeUpstream,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "upstream account missing api_key")
		require.Empty(t, token)
	})
}

func TestAntigravityTokenProvider_GetAccessToken_Guards(t *testing.T) {
	provider := &AntigravityTokenProvider{}

	t.Run("nil account", func(t *testing.T) {
		token, err := provider.GetAccessToken(context.Background(), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "account is nil")
		require.Empty(t, token)
	})

	t.Run("non-antigravity platform", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity account")
		require.Empty(t, token)
	})

	t.Run("unsupported account type", func(t *testing.T) {
		account := &Account{
			Platform: PlatformAntigravity,
			Type:     AccountTypeAPIKey,
		}
		token, err := provider.GetAccessToken(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an antigravity oauth account")
		require.Empty(t, token)
	})
}

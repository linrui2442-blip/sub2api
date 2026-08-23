//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type concurrentRejectedTokenRepo struct {
	mockAccountRepoForGemini
	mu      sync.Mutex
	account *Account
}

func (r *concurrentRejectedTokenRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotOAuthRefreshAccount(r.account), nil
}

func (r *concurrentRejectedTokenRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.account.Credentials = shallowCopyMap(credentials)
	return nil
}

type concurrentRejectedTokenExecutor struct {
	mu           sync.Mutex
	refreshCalls int
	credentials  map[string]any
}

func (e *concurrentRejectedTokenExecutor) CanRefresh(*Account) bool { return true }
func (e *concurrentRejectedTokenExecutor) NeedsRefresh(*Account, time.Duration) bool {
	return true
}
func (e *concurrentRejectedTokenExecutor) CacheKey(account *Account) string {
	return AntigravityTokenCacheKey(account)
}
func (e *concurrentRejectedTokenExecutor) Refresh(context.Context, *Account) (map[string]any, error) {
	e.mu.Lock()
	e.refreshCalls++
	e.mu.Unlock()
	time.Sleep(10 * time.Millisecond)
	return shallowCopyMap(e.credentials), nil
}

func (e *concurrentRejectedTokenExecutor) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.refreshCalls
}

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

func TestAntigravityTokenProvider_TransientProactiveRefreshUsesStillSafeToken(t *testing.T) {
	account := &Account{
		ID: 96, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "still-safe-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(2 * time.Minute).Unix(),
		},
	}
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{needsRefresh: true, err: errors.New("dial tcp: i/o timeout")}
	provider := NewAntigravityTokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "still-safe-token", token)
	require.Equal(t, 1, executor.refreshCalls)
}

func TestAntigravityTokenProvider_TransientRefreshDoesNotFallbackInsideSafetyMargin(t *testing.T) {
	account := &Account{
		ID: 97, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "nearly-expired-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(30 * time.Second).Unix(),
		},
	}
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{needsRefresh: true, err: errors.New("HTTP 503 UNAVAILABLE")}
	provider := NewAntigravityTokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)

	_, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
}

func TestOAuthRefreshAPI_ConcurrentRejectedTokenRefreshesOnce(t *testing.T) {
	const callers = 20
	account := &Account{
		ID: 93, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "rejected-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(time.Hour).Unix(), "_token_version": int64(100),
		},
	}
	repo := &concurrentRejectedTokenRepo{account: snapshotOAuthRefreshAccount(account)}
	executor := &concurrentRejectedTokenExecutor{credentials: map[string]any{
		"access_token": "fresh-token", "refresh_token": "valid-refresh",
		"expires_at": time.Now().Add(time.Hour).Unix(),
	}}
	api := NewOAuthRefreshAPI(repo, nil)

	var wg sync.WaitGroup
	errs := make(chan error, callers)
	results := make(chan string, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := api.RecoverRejectedAccessToken(context.Background(), account, executor, "rejected-token", 100)
			if err != nil {
				errs <- err
				return
			}
			results <- result.Account.GetCredential("access_token")
		}()
	}
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		require.NoError(t, err)
	}
	for token := range results {
		require.Equal(t, "fresh-token", token)
	}
	require.Equal(t, 1, executor.calls())
}

func TestBuildAntigravityDegradedUsage_ReauthSemantics(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		needsReauth bool
	}{
		{name: "final invalid grant", err: classifyFinalAntigravityRefreshError(stringError("refresh failed: invalid_grant")), needsReauth: true},
		{name: "raw invalid grant is not authoritative", err: stringError("business API returned invalid_grant")},
		{name: "transient 401", err: stringError("FetchAvailableModels failed HTTP 401 UNAUTHENTICATED")},
		{name: "rate limited", err: stringError("HTTP 429 RESOURCE_EXHAUSTED")},
		{name: "not found", err: stringError("HTTP 404 NOT_FOUND")},
		{name: "server error", err: stringError("HTTP 503 UNAVAILABLE")},
		{name: "network", err: stringError("dial tcp: i/o timeout")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := buildAntigravityDegradedUsage(tt.err)
			require.Equal(t, tt.needsReauth, info.NeedsReauth)
		})
	}
}

func TestClassifyFinalAntigravityRefreshError(t *testing.T) {
	tests := []struct {
		name  string
		err   string
		class antigravityAuthFailureClass
	}{
		{name: "invalid grant", err: "invalid_grant", class: antigravityAuthFailureReauthRequired},
		{name: "invalid rapt", err: "invalid_grant: invalid_rapt", class: antigravityAuthFailureReauthRequired},
		{name: "missing refresh", err: "no refresh token available", class: antigravityAuthFailureReauthRequired},
		{name: "invalid client", err: "invalid_client", class: antigravityAuthFailureProviderConfig},
		{name: "unauthorized client", err: "unauthorized_client", class: antigravityAuthFailureProviderConfig},
		{name: "admin policy", err: "admin_policy_enforced", class: antigravityAuthFailurePolicyBlocked},
		{name: "access token rejected", err: "HTTP 401 UNAUTHENTICATED", class: antigravityAuthFailureAccessTokenRejected},
		{name: "network", err: "dial tcp timeout", class: antigravityAuthFailureTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, ok := antigravityFailureClass(classifyFinalAntigravityRefreshError(stringError(tt.err)))
			require.True(t, ok)
			require.Equal(t, tt.class, class)
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

//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestAntigravityQuotaFetcher_First401RefreshesOnceAndRetries(t *testing.T) {
	var fetchCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:fetchAvailableModels":
			fetchCalls++
			if r.Header.Get("Authorization") != "Bearer fresh-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"status":"UNAUTHENTICATED"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":{}}`))
		case "/v1internal:loadCodeAssist":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	oldURLs := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	defer func() { antigravity.BaseURLs = oldURLs }()

	account := &Account{
		ID: 101, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "stale-token", "refresh_token": "valid-refresh",
			"expires_at": time.Now().Add(time.Hour).Unix(), "project_id": "project-101",
		},
	}
	repo := &refreshAPIAccountRepo{account: account}
	cache := &antigravityProviderCacheStub{token: "stale-token"}
	executor := &refreshAPIExecutorStub{credentials: map[string]any{
		"access_token": "fresh-token", "refresh_token": "valid-refresh",
		"expires_at": time.Now().Add(time.Hour).Unix(), "project_id": "project-101",
	}}
	provider := NewAntigravityTokenProvider(repo, cache, nil)
	refreshAPI := NewOAuthRefreshAPI(repo, cache)
	provider.SetRefreshAPI(refreshAPI, executor)
	usageCache := NewUsageCache()
	usageCache.antigravityCache.Store(account.ID, &antigravityUsageCache{
		usageInfo: &UsageInfo{NeedsReauth: true}, timestamp: time.Now(),
	})
	usageService := &AccountUsageService{cache: usageCache}
	refreshAPI.SetPostRefreshHook(func(refreshed *Account) {
		usageService.InvalidateAntigravityUsageCache(refreshed.ID)
	})
	fetcher := NewAntigravityQuotaFetcher(nil, provider)

	result, err := fetcher.FetchQuota(context.Background(), account, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.UsageInfo.NeedsReauth)
	require.Equal(t, 2, fetchCalls)
	require.Equal(t, 1, executor.refreshCalls)
	_, staleUsageStillCached := usageCache.antigravityCache.Load(account.ID)
	require.False(t, staleUsageStillCached)
}

func TestAntigravityQuotaFetcher_InvalidGrantRequiresReauth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"status":"UNAUTHENTICATED"}}`))
	}))
	defer server.Close()
	oldURLs := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	defer func() { antigravity.BaseURLs = oldURLs }()

	account := &Account{
		ID: 102, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{
			"access_token": "stale-token", "refresh_token": "revoked-refresh",
			"expires_at": time.Now().Add(time.Hour).Unix(), "project_id": "project-102",
		},
	}
	repo := &refreshAPIAccountRepo{account: account}
	cache := &antigravityProviderCacheStub{token: "stale-token"}
	executor := &refreshAPIExecutorStub{err: stringError("invalid_grant: refresh token revoked")}
	provider := NewAntigravityTokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

	_, err := NewAntigravityQuotaFetcher(nil, provider).FetchQuota(context.Background(), account, "")
	require.Error(t, err)
	require.True(t, buildAntigravityDegradedUsage(err).NeedsReauth)
	require.Equal(t, 1, executor.refreshCalls)
}

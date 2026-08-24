//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type authRecoveryUpstream struct {
	mu             sync.Mutex
	statuses       []int
	authorizations []string
}

func (u *authRecoveryUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authorizations = append(u.authorizations, req.Header.Get("Authorization"))
	status := http.StatusOK
	if len(u.statuses) > 0 {
		status = u.statuses[0]
		u.statuses = u.statuses[1:]
	}
	body := "ok"
	if status == http.StatusUnauthorized {
		body = `{"error":{"status":"UNAUTHENTICATED"}}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *authRecoveryUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func newGatewayAuthRecoveryHarness() (*AntigravityGatewayService, *Account, *concurrentRejectedTokenExecutor) {
	account := &Account{
		ID: 94, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
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
	provider := NewAntigravityTokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
	return &AntigravityGatewayService{tokenProvider: provider}, account, executor
}

func TestAntigravityGatewayProtocolsRecoverFirst401ExactlyOnce(t *testing.T) {
	protocols := []string{"gemini-native", "claude-compatible", "openai-chat", "openai-responses"}
	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			svc, account, executor := newGatewayAuthRecoveryHarness()
			upstream := &authRecoveryUpstream{statuses: []int{http.StatusUnauthorized, http.StatusOK}}
			result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
				prefix: "[" + protocol + "]", ctx: context.Background(), account: account,
				accessToken: "rejected-token", action: "streamGenerateContent", body: []byte(`{}`),
				httpUpstream: upstream, requestedModel: "gemini-3.1-pro-high",
			})
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.resp.StatusCode)
			require.Equal(t, 1, executor.calls())
			require.Equal(t, []string{"Bearer rejected-token", "Bearer fresh-token"}, upstream.authorizations)
		})
	}
}

func TestAntigravityGatewaySecond401StopsWithoutRefreshLoop(t *testing.T) {
	svc, account, executor := newGatewayAuthRecoveryHarness()
	upstream := &authRecoveryUpstream{statuses: []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusOK}}
	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		prefix: "[second-401]", ctx: context.Background(), account: account,
		accessToken: "rejected-token", action: "streamGenerateContent", body: []byte(`{}`),
		httpUpstream: upstream, requestedModel: "gemini-3.1-pro-high",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, result.resp.StatusCode)
	require.Equal(t, 1, executor.calls())
	require.Len(t, upstream.authorizations, 2)
}

func TestAntigravityGatewaySuccessfulStreamingResponseIsNeverReplayed(t *testing.T) {
	svc, account, executor := newGatewayAuthRecoveryHarness()
	upstream := &authRecoveryUpstream{statuses: []int{http.StatusOK, http.StatusUnauthorized}}
	result, err := svc.antigravityRetryLoop(antigravityRetryLoopParams{
		prefix: "[stream]", ctx: context.Background(), account: account,
		accessToken: "rejected-token", action: "streamGenerateContent", body: []byte(`{}`),
		httpUpstream: upstream, requestedModel: "gemini-3.1-pro-high",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Zero(t, executor.calls())
	require.Len(t, upstream.authorizations, 1)
}

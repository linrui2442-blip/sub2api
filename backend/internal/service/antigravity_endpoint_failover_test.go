//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type antigravityEndpointAttempt struct {
	host          string
	body          []byte
	accountID     int64
	proxyURL      string
	authorization string
}

type antigravityEndpointOutcome struct {
	resp *http.Response
	err  error
}

type antigravityEndpointUpstream struct {
	outcomes []antigravityEndpointOutcome
	attempts []antigravityEndpointAttempt
}

func (u *antigravityEndpointUpstream) Do(req *http.Request, proxyURL string, accountID int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.attempts = append(u.attempts, antigravityEndpointAttempt{
		host:          req.URL.Host,
		body:          append([]byte(nil), body...),
		accountID:     accountID,
		proxyURL:      proxyURL,
		authorization: req.Header.Get("Authorization"),
	})
	idx := len(u.attempts) - 1
	if idx >= len(u.outcomes) {
		return nil, errors.New("unexpected extra endpoint attempt")
	}
	return u.outcomes[idx].resp, u.outcomes[idx].err
}

func (u *antigravityEndpointUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func antigravityEndpointResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func antigravityPaidEndpointAccount() *Account {
	return &Account{
		ID:          42,
		Name:        "paid-antigravity",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 3,
		Credentials: map[string]any{"paid_tier": "g1-pro-tier"},
	}
}

func runAntigravityEndpointLoop(t *testing.T, upstream HTTPUpstream) (*antigravityRetryLoopResult, error) {
	t.Helper()
	t.Setenv(antigravityForwardBaseURLEnv, "")
	account := antigravityPaidEndpointAccount()
	return (&AntigravityGatewayService{}).antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:            context.Background(),
		prefix:         "[endpoint-failover-test]",
		account:        account,
		proxyURL:       "http://proxy.test:7897",
		accessToken:    "test-access-token",
		action:         "streamGenerateContent",
		body:           []byte(`{"project":"project-1","model":"gemini-pro-agent","request":{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}}`),
		httpUpstream:   upstream,
		requestedModel: "gemini-3.1-pro-high",
		handleError: func(context.Context, string, *Account, int, http.Header, []byte, string, int64, string, bool) *handleModelRateLimitResult {
			return nil
		},
	})
}

func TestAntigravityEndpointFailover_EOFOrderBodyAndIdentity(t *testing.T) {
	upstream := &antigravityEndpointUpstream{outcomes: []antigravityEndpointOutcome{
		{err: io.EOF},
		{err: io.ErrUnexpectedEOF},
		{resp: antigravityEndpointResponse(http.StatusOK, "ok")},
	}}

	result, err := runAntigravityEndpointLoop(t, upstream)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Equal(t, []string{
		"daily-cloudcode-pa.googleapis.com",
		"daily-cloudcode-pa.sandbox.googleapis.com",
		"cloudcode-pa.googleapis.com",
	}, []string{upstream.attempts[0].host, upstream.attempts[1].host, upstream.attempts[2].host})
	require.Len(t, upstream.attempts, 3)
	for _, attempt := range upstream.attempts {
		require.NotEmpty(t, attempt.body)
		require.True(t, bytes.Equal(upstream.attempts[0].body, attempt.body))
		require.Equal(t, int64(42), attempt.accountID)
		require.Equal(t, "http://proxy.test:7897", attempt.proxyURL)
		require.Equal(t, "Bearer test-access-token", attempt.authorization)
		require.Contains(t, string(attempt.body), `"model":"gemini-pro-agent"`)
		require.Contains(t, string(attempt.body), `"project":"project-1"`)
	}
}

func TestAntigravityEndpointFailover_FirstEndpointSuccess(t *testing.T) {
	upstream := &antigravityEndpointUpstream{outcomes: []antigravityEndpointOutcome{{resp: antigravityEndpointResponse(http.StatusOK, "ok")}}}
	result, err := runAntigravityEndpointLoop(t, upstream)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Len(t, upstream.attempts, 1)
	require.Equal(t, "daily-cloudcode-pa.googleapis.com", upstream.attempts[0].host)
}

func TestAntigravityEndpointFailover_SecondEndpointSuccess(t *testing.T) {
	upstream := &antigravityEndpointUpstream{outcomes: []antigravityEndpointOutcome{
		{err: io.EOF},
		{resp: antigravityEndpointResponse(http.StatusOK, "ok")},
	}}
	result, err := runAntigravityEndpointLoop(t, upstream)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.Equal(t, []string{"daily-cloudcode-pa.googleapis.com", "daily-cloudcode-pa.sandbox.googleapis.com"}, []string{upstream.attempts[0].host, upstream.attempts[1].host})
}

func TestAntigravityEndpointFailover_HTTP400DoesNotFailOver(t *testing.T) {
	upstream := &antigravityEndpointUpstream{outcomes: []antigravityEndpointOutcome{{resp: antigravityEndpointResponse(http.StatusBadRequest, `{"error":"invalid"}`)}}}
	result, err := runAntigravityEndpointLoop(t, upstream)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, result.resp.StatusCode)
	require.Len(t, upstream.attempts, 1)
	require.Equal(t, "daily-cloudcode-pa.googleapis.com", upstream.attempts[0].host)
}

func TestAntigravityEndpointFailover_HTTP429StaysOnCurrentEndpoint(t *testing.T) {
	outcomes := make([]antigravityEndpointOutcome, antigravityMaxRetries)
	for i := range outcomes {
		outcomes[i].resp = antigravityEndpointResponse(http.StatusTooManyRequests, `{"error":{"message":"Resource has been exhausted"}}`)
	}
	upstream := &antigravityEndpointUpstream{outcomes: outcomes}
	result, err := runAntigravityEndpointLoop(t, upstream)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, result.resp.StatusCode)
	require.Len(t, upstream.attempts, antigravityMaxRetries)
	for _, attempt := range upstream.attempts {
		require.Equal(t, "daily-cloudcode-pa.googleapis.com", attempt.host)
	}
}

func TestAntigravityEndpointFailover_EstablishedResponseStreamEOFDoesNotFailOver(t *testing.T) {
	upstream := &antigravityEndpointUpstream{outcomes: []antigravityEndpointOutcome{{
		resp: antigravityEndpointResponse(http.StatusOK, "data: {\"text\":\"started\"}\n\n"),
	}}}
	result, err := runAntigravityEndpointLoop(t, upstream)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	body, readErr := io.ReadAll(result.resp.Body)
	require.NoError(t, readErr)
	require.Contains(t, string(body), "started")
	require.Len(t, upstream.attempts, 1)
}

func TestAntigravityEndpointFailover_AllEndpointsEOFMaxThree(t *testing.T) {
	upstream := &antigravityEndpointUpstream{outcomes: []antigravityEndpointOutcome{{err: io.EOF}, {err: io.EOF}, {err: io.EOF}}}
	result, err := runAntigravityEndpointLoop(t, upstream)
	require.Error(t, err)
	require.Nil(t, result)
	require.ErrorIs(t, err, io.EOF)
	require.Len(t, upstream.attempts, 3)
}

func TestResolveAntigravityGenerationEndpoints(t *testing.T) {
	t.Run("paid tier", func(t *testing.T) {
		t.Setenv(antigravityForwardBaseURLEnv, "")
		require.Equal(t, []string{antigravityDailyBaseURL, antigravitySandboxBaseURL, antigravityProdBaseURL}, resolveAntigravityGenerationEndpoints(antigravityPaidEndpointAccount()))
	})
	t.Run("free tier remains production only", func(t *testing.T) {
		t.Setenv(antigravityForwardBaseURLEnv, "")
		account := &Account{Credentials: map[string]any{"paid_tier": "free-tier"}}
		require.Equal(t, []string{antigravityProdBaseURL}, resolveAntigravityGenerationEndpoints(account))
	})
	t.Run("explicit sandbox remains single endpoint", func(t *testing.T) {
		t.Setenv(antigravityForwardBaseURLEnv, "sandbox")
		require.Equal(t, []string{antigravitySandboxBaseURL}, resolveAntigravityGenerationEndpoints(antigravityPaidEndpointAccount()))
	})
}

func TestIsAntigravityPreResponseEndpointFailoverError(t *testing.T) {
	require.True(t, isAntigravityPreResponseEndpointFailoverError(io.EOF))
	require.True(t, isAntigravityPreResponseEndpointFailoverError(io.ErrUnexpectedEOF))
	require.True(t, isAntigravityPreResponseEndpointFailoverError(errAntigravityNilResponse))
	require.False(t, isAntigravityPreResponseEndpointFailoverError(context.Canceled))
	require.False(t, isAntigravityPreResponseEndpointFailoverError(context.DeadlineExceeded))
	require.False(t, isAntigravityPreResponseEndpointFailoverError(errors.New("provider rejected schema")))
}

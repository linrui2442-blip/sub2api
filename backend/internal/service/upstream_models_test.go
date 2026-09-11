package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func upstreamModelSyncTestConfig() *config.Config {
	return &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		},
	}
}

func openAIOAuthMembershipAccount(mapping map[string]any) *Account {
	return &Account{ID: 77, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "redacted", "model_mapping": mapping}}
}

func TestSyncUpstreamModelCatalog_OpenAIOAuthDiscoveryEvidence(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	svc := &AccountTestService{accountRepo: repo}
	account := openAIOAuthMembershipAccount(map[string]any{"gpt-5.4-mini": "gpt-5.4-mini"})
	repo.accounts = map[int64]*Account{account.ID: account}
	svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
		return &CodexModelsManifest{Body: []byte(`{"models":[{"slug":"gpt-reserve"}]}`)}, nil
	})
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Equal(t, []string{"gpt-reserve"}, live)
	require.True(t, account.IsModelSupported("gpt-5.4-mini"))
	require.False(t, account.GetUpstreamModelMetadataSnapshot().LiveModelsAuthoritative)
	account.Credentials["model_mapping"] = map[string]any{"alias-x": "gpt-reserve"}
	require.True(t, account.IsModelSupported("alias-x"))
}

func TestSyncUpstreamModelCatalog_PersistsMembershipWithIncompleteMetadata(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	svc := &AccountTestService{accountRepo: repo}
	account := openAIOAuthMembershipAccount(nil)
	repo.accounts = map[int64]*Account{account.ID: account}
	svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
		return &CodexModelsManifest{Body: []byte(`{"models":[{"slug":"gpt-reserve"}]}`)}, nil
	})
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	_, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.NotEmpty(t, repo.updates)
}

func TestAccountIsModelSupported_OpenAIOAuthLegacyFallbackWithoutSnapshot(t *testing.T) {
	account := openAIOAuthMembershipAccount(nil)
	require.True(t, account.IsModelSupported("gpt-5.4-mini"))
}

func TestAccountIsModelSupported_ProviderDiscoverySemantics(t *testing.T) {
	for _, tc := range []struct {
		name, platform, accountType, requested string
		live                                   []string
		mapping                                map[string]any
		passthrough, want                      bool
	}{
		{name: "openai listed", platform: PlatformOpenAI, accountType: AccountTypeOAuth, live: []string{"a"}, requested: "a", want: true},
		{name: "openai absent unknown", platform: PlatformOpenAI, accountType: AccountTypeOAuth, live: []string{"a"}, requested: "b", want: true},
		{name: "openai empty unknown", platform: PlatformOpenAI, accountType: AccountTypeOAuth, requested: "b", want: true},
		{name: "openai listed alias", platform: PlatformOpenAI, accountType: AccountTypeOAuth, live: []string{"a"}, mapping: map[string]any{"alias": "a"}, requested: "alias", want: true},
		{name: "openai absent alias uses mapping", platform: PlatformOpenAI, accountType: AccountTypeOAuth, live: []string{"a"}, mapping: map[string]any{"alias": "b"}, requested: "alias", want: true},
		{name: "openai absent unconfigured retains restriction", platform: PlatformOpenAI, accountType: AccountTypeOAuth, live: []string{"a"}, mapping: map[string]any{"alias": "b"}, requested: "c", want: false},
		{name: "openai foreign family remains excluded", platform: PlatformOpenAI, accountType: AccountTypeOAuth, live: []string{"a"}, requested: "deepseek-chat", want: false},
		{name: "antigravity absent denied", platform: PlatformAntigravity, accountType: AccountTypeOAuth, live: []string{"a"}, requested: "b", want: false},
		{name: "antigravity empty denied", platform: PlatformAntigravity, accountType: AccountTypeOAuth, requested: "a", want: false},
		{name: "gemini oauth absent denied", platform: PlatformGemini, accountType: AccountTypeOAuth, live: []string{"a"}, requested: "b", want: false},
		{name: "gemini apikey absent denied", platform: PlatformGemini, accountType: AccountTypeAPIKey, live: []string{"a"}, requested: "b", want: false},
		{name: "openai apikey legacy unchanged", platform: PlatformOpenAI, accountType: AccountTypeAPIKey, live: []string{"a"}, requested: "b", want: true},
		{name: "passthrough overrides evidence and mapping", platform: PlatformOpenAI, accountType: AccountTypeOAuth, mapping: map[string]any{"a": "a"}, requested: "b", passthrough: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, legacy := range []bool{false, true} {
				account := &Account{Platform: tc.platform, Type: tc.accountType, Credentials: map[string]any{"model_mapping": tc.mapping}, Extra: map[string]any{"openai_passthrough": tc.passthrough}}
				// Both persisted legacy JSON and the new representation must agree.
				snapshot := map[string]any{"live_models": tc.live, "live_models_authoritative": true}
				if !legacy {
					snapshot["live_models_discovery_known"] = true
					snapshot["live_models_authoritative"] = supportsAuthoritativeMembership(account)
				}
				account.Extra[upstreamModelMetadataExtraKey] = snapshot
				require.Equal(t, tc.want, account.IsModelSupported(tc.requested), "legacy=%v", legacy)
			}
		})
	}
}

func TestOpenAIDiscovery_TemporaryFailurePreservesEvidence(t *testing.T) {
	for _, status := range []int{400, 404, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := openAIOAuthMembershipAccount(nil)
			account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{LiveModelsDiscoveryKnown: true, LiveModels: []string{"a"}})
			before := account.GetUpstreamModelMetadataSnapshot()
			reset := time.Now().Add(time.Hour)
			account.RateLimitResetAt = &reset
			account.TempUnschedulableUntil = &reset
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
			svc := &AccountTestService{accountRepo: repo}
			svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
				return nil, &codexModelsManifestUpstreamError{statusCode: status, err: errors.New(http.StatusText(status))}
			})
			_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
			require.Error(t, err)
			require.Equal(t, before, account.GetUpstreamModelMetadataSnapshot())
			require.Empty(t, repo.updates)
			require.True(t, account.IsModelSupported("b"))
		})
	}
}

func TestCurrentSupportedModelIDs_AuthoritativeMembershipFiltersStaticAndPreservesLiveAlias(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"live-alias":  "live-target",
			"stale-alias": "stale-target",
		}},
	}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		LiveModelsAuthoritative: true,
		LiveModels:              []string{"live-a", "live-target"},
	})

	require.Equal(t,
		[]string{"live-a", "live-alias", "live-target"},
		CurrentSupportedModelIDs(account, []string{"live-a", "old-static"}),
	)
}

func TestCurrentSupportedModelIDs_UnknownKeepsLegacyCandidates(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	require.Equal(t, []string{"legacy-b", "legacy-a"}, CurrentSupportedModelIDs(account, []string{"legacy-b", "legacy-a"}))
}

func TestSyncUpstreamModelCatalog_FailurePreservesPreviousMembership(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	account := openAIOAuthMembershipAccount(nil)
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Source: "upstream", SyncedAt: "before", Models: map[string]UpstreamModelMetadata{"gpt-reserve": {ID: "gpt-reserve"}},
		LiveModels: []string{"gpt-reserve"}, LiveModelsAuthoritative: true,
	})
	repo.accounts = map[int64]*Account{account.ID: account}
	svc := &AccountTestService{accountRepo: repo}
	svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
		return nil, errors.New("upstream unavailable")
	})
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.Error(t, err)
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Equal(t, []string{"gpt-reserve"}, live)
	require.Empty(t, repo.updates)
}

func TestSyncUpstreamModelCatalog_DoesNotTreatMappingAsLiveMembership(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	account := openAIOAuthMembershipAccount(map[string]any{"gpt-5.4-mini": "gpt-5.4-mini"})
	repo.accounts = map[int64]*Account{account.ID: account}
	svc := &AccountTestService{accountRepo: repo}
	svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
		return &CodexModelsManifest{Body: []byte(`{"models":[{"slug":"gpt-reserve"}]}`)}, nil
	})
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	live, _ := account.GetUpstreamLiveModels()
	require.Equal(t, []string{"gpt-reserve"}, live)
	// Mapping remains untouched as routing configuration; it must not enlarge
	// the authoritative live membership set.
	require.Equal(t, "gpt-5.4-mini", account.GetModelMapping()["gpt-5.4-mini"])
}

func TestAccountIsModelSupported_OpenAIOAuthEmptyDiscoveryPreservesLegacySupport(t *testing.T) {
	account := openAIOAuthMembershipAccount(map[string]any{"gpt-5.4-mini": "gpt-5.4-mini"})
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Source: "upstream", Models: map[string]UpstreamModelMetadata{}, LiveModels: []string{}, LiveModelsAuthoritative: true,
	})
	require.True(t, account.IsModelSupported("gpt-5.4-mini"))
	require.True(t, account.GetUpstreamModelMetadataSnapshot().LiveModelsDiscoveryKnown)
	require.False(t, account.GetUpstreamModelMetadataSnapshot().LiveModelsAuthoritative)
}

func TestSyncUpstreamModelCatalog_PersistsOpenAIEmptyDiscovery(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	account := openAIOAuthMembershipAccount(nil)
	repo.accounts = map[int64]*Account{account.ID: account}
	svc := &AccountTestService{accountRepo: repo}
	svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
		return &CodexModelsManifest{Body: []byte(`{"models":[]}`)}, nil
	})
	catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, catalog.Models)
	require.False(t, catalog.AbsenceAuthoritative)
	require.True(t, account.IsModelSupported("unknown-openai-model"))
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Empty(t, live)
	require.NotEmpty(t, repo.updates)
}

func TestAccountGetUpstreamModelMetadataSnapshot_OldSnapshotIsUnknown(t *testing.T) {
	account := openAIOAuthMembershipAccount(nil)
	account.Extra = map[string]any{upstreamModelMetadataExtraKey: map[string]any{
		"source": "old", "synced_at": "before", "models": map[string]any{"gpt-reserve": map[string]any{"id": "gpt-reserve"}},
	}}
	snapshot := account.GetUpstreamModelMetadataSnapshot()
	require.NotNil(t, snapshot)
	_, authoritative := account.GetUpstreamLiveModels()
	require.False(t, authoritative)
	require.Contains(t, snapshot.Models, "gpt-reserve")
}

func TestAccountIsModelSupported_OpenAIOAuthPassthroughIgnoresMembership(t *testing.T) {
	account := openAIOAuthMembershipAccount(nil)
	account.Extra = map[string]any{"openai_passthrough": true}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{LiveModelsAuthoritative: true})
	require.True(t, account.IsModelSupported("gpt-5.4-mini"))
}

func TestSyncUpstreamModelCatalog_GeminiAPIKeyAuthoritativeMembership(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	cfg := upstreamModelSyncTestConfig()
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	account := &Account{ID: 88, Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "gemini-test", "base_url": "http://gemini.test"},
		Extra:       map[string]any{}}
	repo.accounts = map[int64]*Account{account.ID: account}
	svc := &AccountTestService{accountRepo: repo, cfg: cfg, httpUpstream: &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, "/v1beta/models", req.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"models":[{"name":"models/gemini-current"}]}`)), Header: make(http.Header)}, nil
	}}}
	account.Credentials["model_mapping"] = map[string]any{"stale-alias": "gemini-retired", "current-alias": "gemini-current"}
	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.True(t, account.IsModelSupported("current-alias"))
	require.False(t, account.IsModelSupported("stale-alias"))
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Equal(t, []string{"gemini-current"}, live)
}

func TestGeminiCodeAssistOAuthMembershipRemainsUnknown(t *testing.T) {
	account := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": "code-assist-project"}}
	require.False(t, supportsAuthoritativeMembership(account))
	require.True(t, account.IsModelSupported("gemini-2.5-pro"))
}

func antigravityOAuthMembershipAccount(mapping map[string]any) *Account {
	return &Account{
		ID:       99,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "antigravity-test-token",
			"expires_at":    time.Now().Add(time.Hour).Unix(),
			"project_id":    "antigravity-test-project",
			"model_mapping": mapping,
		},
		Extra: map[string]any{},
	}
}

func antigravityOAuthMembershipService(t *testing.T, handler http.HandlerFunc, account *Account) (*AccountTestService, *upstreamBillingProbeAccountRepo) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	oldURLs := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	t.Cleanup(func() { antigravity.BaseURLs = oldURLs })

	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	tokenProvider := NewAntigravityTokenProvider(repo, nil, nil)
	gateway := NewAntigravityGatewayService(repo, nil, nil, tokenProvider, nil, nil, nil, nil)
	return &AccountTestService{accountRepo: repo, antigravityGatewayService: gateway}, repo
}

func TestSyncUpstreamModelCatalog_AntigravityOAuthAuthoritativeMembership(t *testing.T) {
	account := antigravityOAuthMembershipAccount(map[string]any{
		"old-alias":     "retired-model",
		"current-alias": "current-model",
	})
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Source:                  "upstream",
		Models:                  map[string]UpstreamModelMetadata{"previous-model": {ID: "previous-model"}},
		LiveModels:              []string{"previous-model"},
		LiveModelsAuthoritative: true,
	})
	var calls int
	svc, _ := antigravityOAuthMembershipService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "Bearer antigravity-test-token", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"project":"antigravity-test-project"}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{"current-model":{"quotaInfo":{"remainingFraction":0}}}}`))
	}, account)

	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Equal(t, []string{"current-model"}, live)
	require.True(t, account.IsModelSupported("current-alias"))
	require.False(t, account.IsModelSupported("old-alias"))
	require.False(t, account.IsModelSupported("previous-model"))
	require.NotContains(t, live, "retired-model")
}

func TestSyncUpstreamModelCatalog_AntigravityOAuthEmptyIsAuthoritative(t *testing.T) {
	account := antigravityOAuthMembershipAccount(map[string]any{"old-alias": "retired-model"})
	svc, _ := antigravityOAuthMembershipService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":{}}`))
	}, account)

	catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, catalog.Models)
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Empty(t, live)
	require.False(t, account.IsModelSupported("old-alias"))
}

func TestSyncUpstreamModelCatalog_AntigravityOAuthFailurePreservesMembership(t *testing.T) {
	account := antigravityOAuthMembershipAccount(nil)
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Source:                  "upstream",
		Models:                  map[string]UpstreamModelMetadata{"current-model": {ID: "current-model"}},
		LiveModels:              []string{"current-model"},
		LiveModelsAuthoritative: true,
	})
	svc, repo := antigravityOAuthMembershipService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, account)

	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.Error(t, err)
	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Equal(t, []string{"current-model"}, live)
	require.Empty(t, repo.updates)
}

func TestSyncUpstreamModelCatalog_AntigravityOAuthFirstFailureKeepsLegacyFallback(t *testing.T) {
	account := antigravityOAuthMembershipAccount(map[string]any{"legacy-alias": "legacy-model"})
	svc, _ := antigravityOAuthMembershipService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}, account)

	_, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.Error(t, err)
	_, authoritative := account.GetUpstreamLiveModels()
	require.False(t, authoritative)
	require.True(t, account.IsModelSupported("legacy-alias"))
}

func TestAntigravityOAuthTemporaryQuotaStateDoesNotChangeMembership(t *testing.T) {
	account := antigravityOAuthMembershipAccount(nil)
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Source:                  "upstream",
		Models:                  map[string]UpstreamModelMetadata{"current-model": {ID: "current-model"}},
		LiveModels:              []string{"current-model"},
		LiveModelsAuthoritative: true,
	})
	reset := time.Now().Add(time.Hour)
	account.RateLimitResetAt = &reset
	account.TempUnschedulableUntil = &reset

	live, authoritative := account.GetUpstreamLiveModels()
	require.True(t, authoritative)
	require.Equal(t, []string{"current-model"}, live)
	require.True(t, account.IsModelSupported("current-model"))
}

func grokOAuthModelSyncTestAccount(baseURL string) *Account {
	credentials := map[string]any{
		"access_token":  "oauth-access-token",
		"refresh_token": "oauth-refresh-token",
		"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"sub":           "grok-user-id",
		"email":         "grok-user@example.com",
	}
	if strings.TrimSpace(baseURL) != "" {
		credentials["base_url"] = baseURL
	}
	return &Account{
		ID:          10,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Credentials: credentials,
	}
}

func TestBuildV1ModelsURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://api.anthropic.com/v1/models", buildV1ModelsURL("https://api.anthropic.com"))
	require.Equal(t, "https://api.anthropic.com/v1/models", buildV1ModelsURL("https://api.anthropic.com/v1"))
	require.Equal(t, "https://api.anthropic.com/v1/models", buildV1ModelsURL("https://api.anthropic.com/v1/models"))
	require.Equal(t, "https://gateway.example.com/antigravity/v1/models", buildV1ModelsURL("https://gateway.example.com/antigravity/"))
}

func TestBuildOpenAIModelsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "zhipu v4 coding base url",
			base: "https://open.bigmodel.cn/api/coding/paas/v4",
			want: "https://open.bigmodel.cn/api/coding/paas/v4/models",
		},
		{
			name: "openai v1 base url",
			base: "https://api.openai.com/v1",
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "models url unchanged",
			base: "https://api.openai.com/v1/models",
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "host fallback uses v1",
			base: "https://api.openai.com",
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "trailing slash on v4",
			base: "https://open.bigmodel.cn/api/coding/paas/v4/",
			want: "https://open.bigmodel.cn/api/coding/paas/v4/models",
		},
		{
			name: "v2 base url",
			base: "https://gateway.example.com/openai/v2",
			want: "https://gateway.example.com/openai/v2/models",
		},
		{
			name: "v3 base url",
			base: "https://gateway.example.com/openai/v3",
			want: "https://gateway.example.com/openai/v3/models",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, buildOpenAIModelsURL(tt.base))
		})
	}
}

func TestBuildGeminiModelsURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", buildGeminiModelsURL("https://generativelanguage.googleapis.com"))
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", buildGeminiModelsURL("https://generativelanguage.googleapis.com/v1beta"))
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", buildGeminiModelsURL("https://generativelanguage.googleapis.com/v1beta/models"))
}

func TestExtractUpstreamModelIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "openai and anthropic data array",
			body: `{"data":[{"id":"claude-sonnet-4-5"},{"id":"gpt-5"},{"id":"gpt-5"},{"id":""}]}`,
			want: []string{"claude-sonnet-4-5", "gpt-5"},
		},
		{
			name: "gemini models array strips prefix",
			body: `{"models":[{"name":"models/gemini-2.5-pro"},{"name":"gemini-2.5-flash"}]}`,
			want: []string{"gemini-2.5-flash", "gemini-2.5-pro"},
		},
		{
			name: "top level array",
			body: `[{"id":"z-model"},{"name":"models/a-model"}]`,
			want: []string{"a-model", "z-model"},
		},
		{
			name: "standard id wins over provider-specific model field",
			body: `{"data":[{"id":"canonical-id","model":"display-model"}]}`,
			want: []string{"canonical-id"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractUpstreamModelIDs([]byte(tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtractGrokUpstreamModelIDs(t *testing.T) {
	t.Parallel()

	models, err := extractGrokUpstreamModelIDs([]byte(`{"data":[{"id":"display-id","model":"grok-4.5"},{"modelId":"grok-build-0.1"},{"model_id":"grok-composer-2.5-fast"},{"name":"Grok Meta Display Name","_meta":{"model":"grok-meta"}},{"name":"grok-name"},{"id":"grok-safe","_meta":"not-an-object"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"grok-4.5", "grok-build-0.1", "grok-composer-2.5-fast", "grok-meta", "grok-name", "grok-safe"}, models)
}

func TestBuildUpstreamModelsRequestsForAPIKeyAccounts(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	ctx := context.Background()

	anthropicReq, err := svc.buildAnthropicUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "anthropic-key",
			"base_url": "https://anthropic.example.com/v1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://anthropic.example.com/v1/models", anthropicReq.URL.String())
	require.Equal(t, "anthropic-key", anthropicReq.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", anthropicReq.Header.Get("anthropic-version"))

	anthropicBearerReq, err := svc.buildAnthropicUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "ollama-key",
			"base_url": "https://ollama.com",
		},
		Extra: map[string]any{
			"anthropic_apikey_auth_scheme": AnthropicAPIKeyAuthSchemeAuthorizationBearer,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://ollama.com/v1/models", anthropicBearerReq.URL.String())
	require.Equal(t, "Bearer ollama-key", anthropicBearerReq.Header.Get("Authorization"))
	require.Empty(t, anthropicBearerReq.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", anthropicBearerReq.Header.Get("anthropic-version"))

	openAIReq, err := svc.buildOpenAIUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "openai-key",
			"base_url": "https://openai.example.com",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://openai.example.com/v1/models", openAIReq.URL.String())
	require.Equal(t, "Bearer openai-key", openAIReq.Header.Get("Authorization"))

	grokReq, err := svc.buildUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "xai-key",
			"base_url": "https://xai.example.com/v1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://xai.example.com/v1/models", grokReq.URL.String())
	require.Equal(t, "Bearer xai-key", grokReq.Header.Get("Authorization"))

	geminiReq, err := svc.buildGeminiUpstreamModelsRequest(ctx, &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "gemini-key",
			"base_url": "https://generativelanguage.googleapis.com/v1beta",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", geminiReq.URL.String())
	require.Equal(t, "gemini-key", geminiReq.Header.Get("x-goog-api-key"))

	antigravityReq, err := svc.buildAntigravityAPIKeyModelsRequest(ctx, &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "antigravity-key",
			"base_url": "https://gateway.example.com/antigravity",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example.com/antigravity/v1/models", antigravityReq.URL.String())
	require.Equal(t, "antigravity-key", antigravityReq.Header.Get("x-api-key"))
}

func TestBuildUpstreamModelsRequestSupportsGrokOAuth(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{
		cfg:               upstreamModelSyncTestConfig(),
		grokTokenProvider: NewGrokTokenProvider(nil, nil),
	}
	req, err := svc.buildUpstreamModelsRequest(context.Background(), grokOAuthModelSyncTestAccount(""))
	require.NoError(t, err)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/models", req.URL.String())
	require.Equal(t, "Bearer oauth-access-token", req.Header.Get("Authorization"))
	require.Equal(t, grokCLIVersion, req.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, "interactive", req.Header.Get("X-Grok-Client-Mode"))
	require.Equal(t, defaultGrokUpstreamUserAgent(), req.Header.Get("User-Agent"))
	require.Equal(t, "grok-user-id", req.Header.Get("X-UserID"))
	require.Equal(t, "grok-user@example.com", req.Header.Get("X-Email"))
	require.NotContains(t, req.Header.Get("Authorization"), "oauth-refresh-token")
}

func TestBuildUpstreamModelsRequestGrokOAuthRequiresTokenProvider(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	_, err := svc.buildUpstreamModelsRequest(context.Background(), grokOAuthModelSyncTestAccount(""))
	require.Error(t, err)

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorConfiguration, syncErr.Kind)
	require.Contains(t, syncErr.SafeMessage(), "token provider")
}

func TestBuildAntigravityAPIKeyModelsRequestRejectsOfficialCloudCodeBase(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	_, err := svc.buildAntigravityAPIKeyModelsRequest(context.Background(), &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "antigravity-key",
			"base_url": "https://cloudcode-pa.googleapis.com",
		},
	})
	require.Error(t, err)

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUnsupported, syncErr.Kind)
	require.Contains(t, syncErr.SafeMessage(), "compatible gateway")
}

func TestBuildAnthropicUpstreamModelsRequestRejectsBedrock(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	_, err := svc.buildAnthropicUpstreamModelsRequest(context.Background(), &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeBedrock,
	})
	require.Error(t, err)

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUnsupported, syncErr.Kind)
}

func TestFetchUpstreamSupportedModelsParsesOpenAIResponse(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"gpt-5"},{"id":"gpt-5"},{"name":"o3"}]}`)),
	}}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          upstreamModelSyncTestConfig(),
	}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{
		ID:       7,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "openai-key",
			"base_url": "https://openai.example.com/v1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5", "o3"}, models)
	require.Equal(t, "https://openai.example.com/v1/models", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer openai-key", upstream.lastReq.Header.Get("Authorization"))
}

func TestFetchUpstreamSupportedModelsOpenAIOAuthUsesCodexManifest(t *testing.T) {
	t.Parallel()
	called := false
	svc := &AccountTestService{}
	svc.SetCodexManifestFetcher(func(ctx context.Context, account *Account) (*CodexModelsManifest, error) {
		called = true
		return &CodexModelsManifest{Body: []byte(`{"models":[{"slug":"gpt-5.6-sol","id":"legacy"},{"slug":"gpt-5.6-luna","name":"legacy-name"}]}`)}, nil
	})
	models, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, []string{"gpt-5.6-luna", "gpt-5.6-sol"}, models)
}

func TestExtractCodexManifestModelIDsFallbackAndMalformed(t *testing.T) {
	models, err := extractCodexManifestModelIDs([]byte(`{"models":[{"id":"legacy-id"},{"name":"legacy-name"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"legacy-id", "legacy-name"}, models)
	_, err = extractCodexManifestModelIDs([]byte(`{"models":`))
	require.Error(t, err)
}

func TestFetchUpstreamSupportedModelsOpenAIOAuthRedactsFetcherError(t *testing.T) {
	t.Parallel()
	const secret = "Bearer oauth-secret-token"
	svc := &AccountTestService{}
	svc.SetCodexManifestFetcher(func(context.Context, *Account) (*CodexModelsManifest, error) {
		return nil, errors.New(secret)
	})
	_, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.Error(t, err)
	var syncErr *UpstreamModelSyncError
	require.ErrorAs(t, err, &syncErr)
	require.NotContains(t, syncErr.SafeMessage(), secret)
}

func TestFetchUpstreamSupportedModelsParsesGrokAPIKeyResponse(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"grok-4.5"},{"id":"grok-4.5"},{"id":"grok-imagine"}]}`)),
	}}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          upstreamModelSyncTestConfig(),
	}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{
		ID:       9,
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "xai-key",
			"base_url": "https://xai.example.com/v1",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"grok-4.5", "grok-imagine"}, models)
	require.Equal(t, "https://xai.example.com/v1/models", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer xai-key", upstream.lastReq.Header.Get("Authorization"))
}

func TestFetchUpstreamSupportedModelsParsesGrokOAuthResponse(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"model":"grok-4.5"},{"model":"grok-4.5"},{"modelId":"grok-build-0.1"}]}`)),
	}}
	svc := &AccountTestService{
		httpUpstream:      upstream,
		cfg:               upstreamModelSyncTestConfig(),
		grokTokenProvider: NewGrokTokenProvider(nil, nil),
	}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), grokOAuthModelSyncTestAccount(""))
	require.NoError(t, err)
	require.Equal(t, []string{"grok-4.5", "grok-build-0.1"}, models)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/models", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer oauth-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, grokCLIVersion, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, "interactive", upstream.lastReq.Header.Get("X-Grok-Client-Mode"))
	require.Equal(t, "grok-user-id", upstream.lastReq.Header.Get("X-UserID"))
	require.Equal(t, "grok-user@example.com", upstream.lastReq.Header.Get("X-Email"))
}

func TestBuildUpstreamModelsRequestGrokOAuthDoesNotSendIdentityToCustomBase(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{
		cfg:               upstreamModelSyncTestConfig(),
		grokTokenProvider: NewGrokTokenProvider(nil, nil),
	}
	req, err := svc.buildUpstreamModelsRequest(context.Background(), grokOAuthModelSyncTestAccount("https://relay.example/v1"))
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/v1/models", req.URL.String())
	require.Empty(t, req.Header.Get("X-UserID"))
	require.Empty(t, req.Header.Get("X-Email"))
}

func TestFetchUpstreamSupportedModelsDoesNotExposeUpstreamBody(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          upstreamModelSyncTestConfig(),
	}

	_, err := svc.FetchUpstreamSupportedModels(context.Background(), &Account{
		ID:       8,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "openai-key",
			"base_url": "https://openai.example.com/v1",
		},
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET_TOKEN")

	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUpstream, syncErr.Kind)
	require.NotContains(t, syncErr.SafeMessage(), "SECRET_TOKEN")
	require.Contains(t, syncErr.SafeMessage(), "HTTP 502")
}

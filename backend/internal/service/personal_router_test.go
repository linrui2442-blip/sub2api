package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolvePersonalProvider(t *testing.T) {
	tests := []struct {
		name, model, endpoint, platform, upstream string
		err                                       error
	}{
		{"openai namespace", "openai/gpt-5.4", "", PlatformOpenAI, "gpt-5.4", nil},
		{"anthropic namespace", "anthropic/claude-sonnet-4-6", "", PlatformAnthropic, "claude-sonnet-4-6", nil},
		{"gemini namespace", "gemini/gemini-2.5-flash", "", PlatformGemini, "gemini-2.5-flash", nil},
		{"antigravity gemini", "antigravity/gemini-3.1-pro-high", "", PlatformAntigravity, "gemini-3.1-pro-high", nil},
		{"antigravity claude", "antigravity/claude-sonnet-4-6", "", PlatformAntigravity, "claude-sonnet-4-6", nil},
		{"native gemini endpoint", "gemini-2.5-flash", PlatformGemini, PlatformGemini, "gemini-2.5-flash", nil},
		{"bare model needs catalog", "gpt-5.4", "", "", "", ErrPersonalRouteUnsupported},
		{"unknown namespace", "future/model", "", "", "", ErrPersonalRouteUnsupported},
		{"unsupported model", "mystery-model", "", "", "", ErrPersonalRouteUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform, upstream, err := ResolvePersonalProvider(tt.model, tt.endpoint)
			require.Equal(t, tt.platform, platform)
			require.Equal(t, tt.upstream, upstream)
			require.True(t, errors.Is(err, tt.err))
		})
	}
}

type personalRouterGroupRepo struct {
	GroupRepository
	groups []Group
}

func (r *personalRouterGroupRepo) ListActive(context.Context) ([]Group, error) {
	return append([]Group(nil), r.groups...), nil
}

type personalRouterAccountRepo struct {
	AccountRepository
	byGroup map[int64][]Account
}

func (r *personalRouterAccountRepo) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, _ []string, _ bool) ([]Account, error) {
	if groupID == nil {
		return nil, nil
	}
	return append([]Account(nil), r.byGroup[*groupID]...), nil
}

func personalMappedAccount(platform string, models ...string) Account {
	mapping := make(map[string]any, len(models))
	for _, model := range models {
		mapping[model] = "wire-" + model
	}
	return Account{
		Platform:    platform,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"model_mapping": mapping},
	}
}

func newPersonalRouterService(groups []Group, accounts map[int64][]Account) *GatewayService {
	return &GatewayService{
		groupRepo:   &personalRouterGroupRepo{groups: groups},
		accountRepo: &personalRouterAccountRepo{byGroup: accounts},
	}
}

func TestGatewayServiceResolvePersonalProviderFromConfiguredCandidates(t *testing.T) {
	groups := []Group{
		{ID: 1, Platform: PlatformAnthropic, Status: StatusActive},
		{ID: 2, Platform: PlatformOpenAI, Status: StatusActive},
		{ID: 3, Platform: PlatformAntigravity, Status: StatusActive},
	}
	svc := newPersonalRouterService(groups, map[int64][]Account{
		1: {personalMappedAccount(PlatformAnthropic, "model-alpha", "shared-model")},
		2: {personalMappedAccount(PlatformOpenAI, "model-beta", "shared-model")},
		3: {personalMappedAccount(PlatformAntigravity, "gemini-3.1-pro")},
	})

	for _, tt := range []struct {
		model, wantPlatform string
		err                 error
	}{
		{"model-alpha", PlatformAnthropic, nil},
		{"model-beta", PlatformOpenAI, nil},
		{"gemini-3.1-pro", PlatformAntigravity, nil},
		{"missing-model", "", ErrPersonalRouteUnsupported},
		{"shared-model", "", ErrPersonalRouteAmbiguous},
	} {
		platform, upstream, err := svc.ResolvePersonalProvider(context.Background(), tt.model, "")
		require.Equal(t, tt.wantPlatform, platform)
		if tt.err == nil {
			require.Equal(t, tt.model, upstream)
		} else {
			require.Empty(t, upstream)
		}
		require.ErrorIs(t, err, tt.err)
	}
}

func TestGatewayServiceResolvePersonalProviderDeduplicatesSamePlatformGroups(t *testing.T) {
	groups := []Group{
		{ID: 10, Platform: PlatformAntigravity, Status: StatusActive},
		{ID: 11, Platform: PlatformAntigravity, Status: StatusActive},
	}
	svc := newPersonalRouterService(groups, map[int64][]Account{
		10: {personalMappedAccount(PlatformAntigravity, "model-alpha")},
		11: {personalMappedAccount(PlatformAntigravity, "model-alpha")},
	})

	platform, upstream, err := svc.ResolvePersonalProvider(context.Background(), "model-alpha", "")
	require.NoError(t, err)
	require.Equal(t, PlatformAntigravity, platform)
	require.Equal(t, "model-alpha", upstream)
}

func TestGatewayServiceResolvePersonalProviderLeavesTransientAvailabilityToScheduler(t *testing.T) {
	account := personalMappedAccount(PlatformAnthropic, "model-alpha")
	resetAt := time.Now().Add(time.Hour)
	account.RateLimitResetAt = &resetAt
	svc := newPersonalRouterService(
		[]Group{{ID: 20, Platform: PlatformAnthropic, Status: StatusActive}},
		map[int64][]Account{20: {account}},
	)

	platform, _, err := svc.ResolvePersonalProvider(context.Background(), "model-alpha", "")
	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, platform)
}

func TestGatewayServiceResolvePersonalProviderUsesConfiguredGroupModelList(t *testing.T) {
	svc := newPersonalRouterService([]Group{{
		ID:       30,
		Platform: PlatformGemini,
		Status:   StatusActive,
		ModelsListConfig: GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"model-alpha"},
		},
	}}, nil)

	platform, _, err := svc.ResolvePersonalProvider(context.Background(), "model-alpha", "")
	require.NoError(t, err)
	require.Equal(t, PlatformGemini, platform)
}

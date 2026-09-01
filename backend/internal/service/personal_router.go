package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrPersonalRouteAmbiguous   = errors.New("personal unified route is ambiguous")
	ErrPersonalRouteUnsupported = errors.New("personal unified route is unsupported")
)

// PersonalProviderResolver resolves the provider namespace used by an
// ungrouped Personal API key.
type PersonalProviderResolver interface {
	ResolvePersonalProvider(ctx context.Context, model, endpointPlatform string) (platform, upstreamModel string, err error)
}

// ResolvePersonalProvider resolves explicit namespaces and endpoint-owned
// protocols. Bare models require configured provider candidates and are
// resolved by GatewayService.ResolvePersonalProvider.
func ResolvePersonalProvider(model, endpointPlatform string) (platform, upstreamModel string, err error) {
	return resolvePersonalProviderFromCandidates(model, endpointPlatform, nil)
}

// ResolvePersonalProvider resolves a bare logical model from persistently
// configured model capabilities. Transient account availability is
// deliberately left to the existing scheduler.
func (s *GatewayService) ResolvePersonalProvider(
	ctx context.Context,
	model, endpointPlatform string,
) (platform, upstreamModel string, err error) {
	model = strings.TrimSpace(model)
	if platform, upstreamModel, resolved, err := resolveExplicitPersonalProvider(model, endpointPlatform); resolved || err != nil {
		return platform, upstreamModel, err
	}
	if s == nil || s.groupRepo == nil || s.accountRepo == nil {
		return "", "", ErrPersonalRouteUnsupported
	}

	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list active personal provider groups: %w", err)
	}

	platformSet := make(map[string]struct{})
	for i := range groups {
		group := &groups[i]
		platform := NormalizeGroupPlatform(group.Platform)
		if !isConcreteRequestPlatform(platform) {
			continue
		}

		matched := groupModelsListSupports(group, model)
		accounts, listErr := s.accountRepo.ListModelAvailabilityCandidates(ctx, &group.ID, []string{platform}, false)
		if listErr != nil {
			return "", "", fmt.Errorf("list personal provider model candidates for group %d: %w", group.ID, listErr)
		}
		for j := range accounts {
			if accounts[j].IsModelSupported(model) {
				matched = true
				break
			}
		}
		if matched {
			platformSet[platform] = struct{}{}
		}
	}

	platforms := make([]string, 0, len(platformSet))
	for platform := range platformSet {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	return resolvePersonalProviderFromCandidates(model, endpointPlatform, platforms)
}

func resolvePersonalProviderFromCandidates(
	model, endpointPlatform string,
	candidatePlatforms []string,
) (platform, upstreamModel string, err error) {
	model = strings.TrimSpace(model)
	if platform, upstreamModel, resolved, err := resolveExplicitPersonalProvider(model, endpointPlatform); resolved || err != nil {
		return platform, upstreamModel, err
	}

	unique := make(map[string]struct{}, len(candidatePlatforms))
	for _, candidate := range candidatePlatforms {
		candidate = NormalizeGroupPlatform(candidate)
		if isConcreteRequestPlatform(candidate) {
			unique[candidate] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return "", "", ErrPersonalRouteUnsupported
	}
	if len(unique) > 1 {
		return "", "", ErrPersonalRouteAmbiguous
	}
	for candidate := range unique {
		return candidate, model, nil
	}
	return "", "", ErrPersonalRouteUnsupported
}

func resolveExplicitPersonalProvider(model, endpointPlatform string) (platform, upstreamModel string, resolved bool, err error) {
	if model == "" {
		return "", "", true, ErrPersonalRouteUnsupported
	}
	if slash := strings.IndexByte(model, '/'); slash > 0 {
		namespace := strings.ToLower(strings.TrimSpace(model[:slash]))
		upstream := strings.TrimSpace(model[slash+1:])
		if upstream == "" {
			return "", "", true, ErrPersonalRouteUnsupported
		}
		switch namespace {
		case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity:
			return namespace, upstream, true, nil
		default:
			return "", "", true, ErrPersonalRouteUnsupported
		}
	}
	if endpointPlatform != "" {
		return endpointPlatform, model, true, nil
	}
	return "", "", false, nil
}

func groupModelsListSupports(group *Group, model string) bool {
	if group == nil || !group.CustomModelsListEnabled() {
		return false
	}
	model = strings.TrimSpace(model)
	for _, candidate := range group.ModelsListConfig.Models {
		if strings.TrimSpace(candidate) == model {
			return true
		}
	}
	return false
}

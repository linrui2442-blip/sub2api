package service

import (
	"errors"
	"strings"
)

var (
	ErrPersonalRouteAmbiguous   = errors.New("personal unified route is ambiguous")
	ErrPersonalRouteUnsupported = errors.New("personal unified route is unsupported")
)

// ResolvePersonalProvider resolves the provider namespace used only by an
// ungrouped Personal API key. The returned model is always safe to send to the
// upstream provider (the Personal namespace has been removed).
//
// endpointPlatform is set for provider-specific protocols such as /v1beta.
// General OpenAI/Anthropic-compatible endpoints pass an empty value because
// their wire format does not prove provider ownership.
func ResolvePersonalProvider(model, endpointPlatform string) (platform, upstreamModel string, err error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", "", ErrPersonalRouteUnsupported
	}
	if slash := strings.IndexByte(model, '/'); slash > 0 {
		namespace := strings.ToLower(strings.TrimSpace(model[:slash]))
		upstream := strings.TrimSpace(model[slash+1:])
		if upstream == "" {
			return "", "", ErrPersonalRouteUnsupported
		}
		switch namespace {
		case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity:
			return namespace, upstream, nil
		default:
			return "", "", ErrPersonalRouteUnsupported
		}
	}

	if endpointPlatform != "" {
		return endpointPlatform, model, nil
	}
	lower := strings.ToLower(model)
	if strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "chatgpt-") ||
		strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") ||
		strings.HasPrefix(lower, "o4") || strings.HasPrefix(lower, "text-embedding-") {
		return PlatformOpenAI, model, nil
	}
	if strings.HasPrefix(lower, "gemini-") || strings.HasPrefix(lower, "claude-") {
		return "", "", ErrPersonalRouteAmbiguous
	}
	return "", "", ErrPersonalRouteUnsupported
}

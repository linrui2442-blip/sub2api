package service

import (
	"errors"
	"testing"

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
		{"unnamespaced openai", "gpt-5.4", "", PlatformOpenAI, "gpt-5.4", nil},
		{"native gemini endpoint", "gemini-2.5-flash", PlatformGemini, PlatformGemini, "gemini-2.5-flash", nil},
		{"ambiguous gemini", "gemini-2.5-flash", "", "", "", ErrPersonalRouteAmbiguous},
		{"ambiguous claude", "claude-sonnet-4-6", "", "", "", ErrPersonalRouteAmbiguous},
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

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNormalizeInboundEndpointPersonalCore(t *testing.T) {
	tests := map[string]string{
		"/v1/messages":         EndpointMessages,
		"/v1/chat/completions": EndpointChatCompletions,
		"/v1/embeddings":       EndpointEmbeddings,
		"/v1/responses":        EndpointResponses,
		"/responses/compact":   EndpointResponsesCompact,
		"/backend-api/codex/responses/input_tokens":     EndpointResponsesInputTokens,
		"/v1beta/models/gemini-2.5-pro:generateContent": EndpointGeminiModels,
	}
	for path, want := range tests {
		require.Equal(t, want, NormalizeInboundEndpoint(path), "path=%s", path)
	}
}

func TestNormalizeInboundEndpointDoesNotCanonicalizeRemovedSurface(t *testing.T) {
	for _, path := range []string{
		"/v1/alpha/search", "/v1/images/generations", "/v1/images/tasks/id",
		"/v1/videos/generations", "/v1/live", "/v1/tts",
	} {
		require.Equal(t, path, NormalizeInboundEndpoint(path))
	}
}

func TestDeriveUpstreamEndpointPersonalCore(t *testing.T) {
	require.Equal(t, EndpointResponses, DeriveUpstreamEndpoint(EndpointChatCompletions, "/v1/chat/completions", service.PlatformOpenAI))
	require.Equal(t, EndpointResponsesCompact, DeriveUpstreamEndpoint(EndpointResponsesCompact, "/responses/compact", service.PlatformOpenAI))
	require.Equal(t, EndpointMessages, DeriveUpstreamEndpoint(EndpointMessages, "/v1/messages", service.PlatformAnthropic))
	require.Equal(t, EndpointGeminiModels, DeriveUpstreamEndpoint(EndpointGeminiModels, "/v1beta/models/gemini:generateContent", service.PlatformGemini))
	require.Equal(t, EndpointEmbeddings, DeriveUpstreamEndpoint(EndpointEmbeddings, "/v1/embeddings", service.PlatformOpenAI))
}

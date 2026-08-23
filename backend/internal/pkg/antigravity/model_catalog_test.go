package antigravity

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPersonalModelCatalogSeparatesAvailabilityFromCompatibility(t *testing.T) {
	recommended := RecommendedModelIDs()
	verified := VerifiedModelIDs()
	unavailable := CurrentlyUnavailableModelIDs()

	require.Len(t, recommended, 6)
	require.Len(t, verified, 20)
	require.Len(t, unavailable, 8)
	require.Subset(t, verified, recommended)
	require.NotEqual(t, len(verified), len(domain.DefaultAntigravityModelMapping))

	for _, id := range unavailable {
		require.NotContains(t, verified, id)
	}
	require.Contains(t, unavailable, "gemini-3.6-flash")
	require.Contains(t, verified, "gemini-3.6-flash-high")
	require.Contains(t, verified, "gemini-3.6-flash-medium")
	require.Contains(t, verified, "gemini-3.6-flash-low")
	require.Contains(t, verified, "gemini-3.6-flash-tiered")
}

func TestVerifiedModelsForAccountTestRecommendedFirstAndExcludesKnown404(t *testing.T) {
	models := VerifiedModelsForAccountTest()
	require.Len(t, models, 20)

	for index, expected := range RecommendedModelIDs() {
		require.Equal(t, expected, models[index].ID)
		require.True(t, models[index].Recommended)
	}

	for _, model := range models {
		require.NotContains(t, CurrentlyUnavailableModelIDs(), model.ID)
	}
}

func TestCompatibilityAliasesRemainIndependent(t *testing.T) {
	require.Equal(t, "gemini-pro-agent", domain.DefaultAntigravityModelMapping["gemini-3.1-pro-high"])
	require.Equal(t, "gemini-3.1-flash-image", domain.DefaultAntigravityModelMapping["gemini-3-pro-image"])
	require.Equal(t, "claude-opus-4-6-thinking", domain.DefaultAntigravityModelMapping["claude-opus-4-6"])

	// A currently unavailable semantic version must not be silently upgraded.
	require.NotEqual(t, "claude-sonnet-4-6", domain.DefaultAntigravityModelMapping["claude-sonnet-4-5"])
}

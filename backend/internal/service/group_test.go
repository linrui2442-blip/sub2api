//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupGetRoutingAccountIDsUsesExactAndWildcardModelRoutes(t *testing.T) {
	group := &Group{
		ModelRoutingEnabled: true,
		ModelRouting: map[string][]int64{
			"gpt-5":    {11},
			"gemini-*": {21, 22},
		},
	}

	require.Equal(t, []int64{11}, group.GetRoutingAccountIDs("GPT-5"))
	require.Equal(t, []int64{21, 22}, group.GetRoutingAccountIDs("gemini-2.5-pro"))
	require.Nil(t, group.GetRoutingAccountIDs("claude-sonnet"))
}

func TestGroupGetRoutingAccountIDsReturnsCopyAndHonorsRoutingFlag(t *testing.T) {
	group := &Group{ModelRoutingEnabled: true, ModelRouting: map[string][]int64{"*": {41, 42}}}
	accountIDs := group.GetRoutingAccountIDs("gpt-5")
	accountIDs[0] = 99

	require.Equal(t, []int64{41, 42}, group.ModelRouting["*"])
	group.ModelRoutingEnabled = false
	require.Nil(t, group.GetRoutingAccountIDs("gpt-5"))
}

func TestGroupContextValidityAndPlatformNormalization(t *testing.T) {
	require.False(t, IsGroupContextValid(nil))
	require.False(t, IsGroupContextValid(&Group{ID: 1}))
	require.False(t, IsGroupContextValid(&Group{Hydrated: true}))
	require.True(t, IsGroupContextValid(&Group{ID: 1, Hydrated: true}))
	require.Equal(t, PlatformOpenAI, NormalizeGroupPlatform(" OpenAI "))
}

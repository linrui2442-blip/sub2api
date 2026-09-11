//go:build unit

package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreserveReauthModelMapping(t *testing.T) {
	existing := map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}, "refresh_token": "old"}
	incoming := map[string]any{"access_token": "new", "refresh_token": "new-refresh"}
	out := preserveReauthModelMapping(existing, incoming)
	require.Equal(t, existing["model_mapping"], out["model_mapping"])
	require.Equal(t, "new", out["access_token"])
	require.Equal(t, "new-refresh", out["refresh_token"])
}

func TestPreserveReauthModelMappingExplicitEmptyClears(t *testing.T) {
	existing := map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}}
	incoming := map[string]any{"model_mapping": map[string]any{}}
	out := preserveReauthModelMapping(existing, incoming)
	require.Empty(t, out["model_mapping"])
}

func TestPreserveReauthPlanType(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]any
		incoming map[string]any
		want     string
	}{
		{name: "authoritative free replaces plus", existing: map[string]any{"plan_type": "plus"}, incoming: map[string]any{"plan_type": "free"}, want: "free"},
		{name: "authoritative plus replaces free", existing: map[string]any{"plan_type": "free"}, incoming: map[string]any{"plan_type": "plus"}, want: "plus"},
		{name: "missing preserves plus", existing: map[string]any{"plan_type": "plus"}, incoming: map[string]any{"access_token": "new"}, want: "plus"},
		{name: "blank preserves plus", existing: map[string]any{"plan_type": "plus"}, incoming: map[string]any{"plan_type": "  "}, want: "plus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := preserveReauthPlanType(tt.existing, tt.incoming)
			require.Equal(t, tt.want, out["plan_type"])
		})
	}
}

func TestReauthPlanTypeAndModelMappingSemanticsCoexist(t *testing.T) {
	existing := map[string]any{
		"plan_type":    "plus",
		"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"},
	}

	preserved := preserveReauthPlanType(existing, preserveReauthModelMapping(existing, map[string]any{"access_token": "new"}))
	require.Equal(t, "plus", preserved["plan_type"])
	require.Equal(t, existing["model_mapping"], preserved["model_mapping"])

	cleared := preserveReauthPlanType(existing, preserveReauthModelMapping(existing, map[string]any{
		"plan_type":    "free",
		"model_mapping": map[string]any{},
	}))
	require.Equal(t, "free", cleared["plan_type"])
	require.Empty(t, cleared["model_mapping"])
}

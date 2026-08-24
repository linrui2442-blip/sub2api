//go:build unit

package handler

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateAPIKeyRequestGroupIDTriState(t *testing.T) {
	var omitted UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"unchanged"}`), &omitted))
	require.False(t, omitted.GroupID.Set)
	require.Nil(t, omitted.GroupID.Value)

	var cleared UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"group_id":null}`), &cleared))
	require.True(t, cleared.GroupID.Set)
	require.Nil(t, cleared.GroupID.Value)

	var grouped UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"group_id":42}`), &grouped))
	require.True(t, grouped.GroupID.Set)
	require.NotNil(t, grouped.GroupID.Value)
	require.Equal(t, int64(42), *grouped.GroupID.Value)
}

func TestValidateAPIKeyCreateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(1)
	positiveDays, zeroDays, negativeDays := 1, 0, -1
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Quota: &zero, RateLimit5h: &large, ExpiresInDays: &positiveDays}))

	for _, req := range []CreateAPIKeyRequest{
		{Quota: &negative},
		{Quota: &nan},
		{RateLimit5h: &inf},
		{RateLimit1d: &negative},
		{RateLimit7d: &negative},
		{ExpiresInDays: &zeroDays},
		{ExpiresInDays: &negativeDays},
	} {
		require.Error(t, validateAPIKeyCreateRequest(req))
	}
}

func TestValidateAPIKeyUpdateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(-1)
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Quota: &zero, RateLimit7d: &large}))

	for _, req := range []UpdateAPIKeyRequest{
		{Quota: &negative},
		{RateLimit5h: &nan},
		{RateLimit1d: &inf},
		{RateLimit7d: &negative},
	} {
		require.Error(t, validateAPIKeyUpdateRequest(req))
	}
}

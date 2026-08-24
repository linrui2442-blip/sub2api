package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseUsageRangeBoundaryDateUsesUserTimezoneAndExclusiveNextDay(t *testing.T) {
	start, err := parseUsageRangeBoundary("2026-08-23", "Asia/Shanghai", false)
	require.NoError(t, err)
	end, err := parseUsageRangeBoundary("2026-08-23", "Asia/Shanghai", true)
	require.NoError(t, err)
	require.Equal(t, "2026-08-23T00:00:00+08:00", start.Format(time.RFC3339))
	require.Equal(t, "2026-08-24T00:00:00+08:00", end.Format(time.RFC3339))
}

func TestParseUsageRangeBoundaryRFC3339RemainsExact(t *testing.T) {
	value := "2026-08-24T05:03:04.123456789Z"
	parsed, err := parseUsageRangeBoundary(value, "Asia/Shanghai", true)
	require.NoError(t, err)
	require.Equal(t, value, parsed.UTC().Format(time.RFC3339Nano))
	require.Equal(t, "2026-08-24T13:03:04.123456789+08:00", parsed.Format(time.RFC3339Nano))
}

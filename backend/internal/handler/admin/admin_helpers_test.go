package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseTimeRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/?start_date=2024-01-01&end_date=2024-01-02&timezone=UTC", nil)
	c.Request = req

	start, end := parseTimeRange(c)
	require.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), start)
	require.Equal(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), end)

	req = httptest.NewRequest(http.MethodGet, "/?start_date=bad&timezone=UTC", nil)
	c.Request = req
	start, end = parseTimeRange(c)
	require.False(t, start.IsZero())
	require.False(t, end.IsZero())
}

// TestOpenAIFastPolicySettingsFromDTO_NormalizesServiceTier 验证 admin
// 写入路径会把 ServiceTier 的空字符串/空白/大小写归一化为
// service.OpenAIFastTierAny ("all")，避免落盘时 "" 与 "all" 双语义。
func TestOpenAIFastPolicySettingsFromDTO_NormalizesServiceTier(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		require.Nil(t, openaiFastPolicySettingsFromDTO(nil))
	})

	t.Run("empty service_tier becomes 'all'", func(t *testing.T) {
		in := &dto.OpenAIFastPolicySettings{
			Rules: []dto.OpenAIFastPolicyRule{{
				ServiceTier: "",
				Action:      "filter",
				Scope:       "all",
			}},
		}
		out := openaiFastPolicySettingsFromDTO(in)
		require.NotNil(t, out)
		require.Len(t, out.Rules, 1)
		require.Equal(t, service.OpenAIFastTierAny, out.Rules[0].ServiceTier)
		require.Equal(t, "all", out.Rules[0].ServiceTier)
	})

	t.Run("whitespace-only service_tier becomes 'all'", func(t *testing.T) {
		in := &dto.OpenAIFastPolicySettings{
			Rules: []dto.OpenAIFastPolicyRule{{
				ServiceTier: "   ",
				Action:      "pass",
				Scope:       "all",
			}},
		}
		out := openaiFastPolicySettingsFromDTO(in)
		require.Equal(t, service.OpenAIFastTierAny, out.Rules[0].ServiceTier)
	})

	t.Run("uppercase service_tier is lowercased", func(t *testing.T) {
		in := &dto.OpenAIFastPolicySettings{
			Rules: []dto.OpenAIFastPolicyRule{{
				ServiceTier: "PRIORITY",
				Action:      "filter",
				Scope:       "all",
				UserIDs:     []int64{42},
			}},
		}
		out := openaiFastPolicySettingsFromDTO(in)
		require.Equal(t, service.OpenAIFastTierPriority, out.Rules[0].ServiceTier)
		require.Equal(t, []int64{42}, out.Rules[0].UserIDs)
	})

	t.Run("non-empty values pass through (lowercased)", func(t *testing.T) {
		in := &dto.OpenAIFastPolicySettings{
			Rules: []dto.OpenAIFastPolicyRule{
				{ServiceTier: "priority", Action: "filter", Scope: "all"},
				{ServiceTier: "flex", Action: "block", Scope: "oauth"},
				{ServiceTier: "all", Action: "pass", Scope: "apikey"},
			},
		}
		out := openaiFastPolicySettingsFromDTO(in)
		require.Len(t, out.Rules, 3)
		require.Equal(t, service.OpenAIFastTierPriority, out.Rules[0].ServiceTier)
		require.Equal(t, service.OpenAIFastTierFlex, out.Rules[1].ServiceTier)
		require.Equal(t, service.OpenAIFastTierAny, out.Rules[2].ServiceTier)
	})
}

package service

import "time"

// APIKeyAuthSnapshot API Key 认证缓存快照（仅包含认证所需字段）
type APIKeyAuthSnapshot struct {
	Version     int                      `json:"version"`
	APIKeyID    int64                    `json:"api_key_id"`
	UserID      int64                    `json:"user_id"`
	GroupID     *int64                   `json:"group_id,omitempty"`
	Name        string                   `json:"name"`
	Status      string                   `json:"status"`
	IPWhitelist []string                 `json:"ip_whitelist,omitempty"`
	IPBlacklist []string                 `json:"ip_blacklist,omitempty"`
	User        APIKeyAuthUserSnapshot   `json:"user"`
	Group       *APIKeyAuthGroupSnapshot `json:"group,omitempty"`

	// Quota fields for API Key independent quota feature
	Quota     float64 `json:"quota"`      // Quota limit in USD (0 = unlimited)
	QuotaUsed float64 `json:"quota_used"` // Used quota amount

	// Expiration field for API Key expiration feature
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Expiration time (nil = never expires)

	// Rate limit configuration (only limits, not usage - usage read from Redis at check time)
	RateLimit5h float64 `json:"rate_limit_5h"`
	RateLimit1d float64 `json:"rate_limit_1d"`
	RateLimit7d float64 `json:"rate_limit_7d"`
}

// APIKeyAuthUserSnapshot 用户快照
type APIKeyAuthUserSnapshot struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	Role          string  `json:"role"`
	Concurrency   int     `json:"concurrency"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`

	Email    string `json:"email"`
	Username string `json:"username"`

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）；用于 billing_cache_service.checkRPM 兜底判断。
	RPMLimit int `json:"rpm_limit"`
}

// APIKeyAuthGroupSnapshot 分组快照
type APIKeyAuthGroupSnapshot struct {
	ID                              int64                             `json:"id"`
	Name                            string                            `json:"name"`
	Platform                        string                            `json:"platform"`
	IsExclusive                     bool                              `json:"is_exclusive"`
	Status                          string                            `json:"status"`
	ClaudeCodeOnly                  bool                              `json:"claude_code_only"`
	FallbackGroupID                 *int64                            `json:"fallback_group_id,omitempty"`
	FallbackGroupIDOnInvalidRequest *int64                            `json:"fallback_group_id_on_invalid_request,omitempty"`
	ModelRouting                    map[string][]int64                `json:"model_routing,omitempty"`
	ModelRoutingEnabled             bool                              `json:"model_routing_enabled"`
	MCPXMLInject                    bool                              `json:"mcp_xml_inject"`
	SupportedModelScopes            []string                          `json:"supported_model_scopes,omitempty"`
	AllowMessagesDispatch           bool                              `json:"allow_messages_dispatch"`
	AllowLive                       bool                              `json:"allow_live"`
	DefaultMappedModel              string                            `json:"default_mapped_model,omitempty"`
	MessagesDispatchModelConfig     OpenAIMessagesDispatchModelConfig `json:"messages_dispatch_model_config,omitempty"`
	ModelsListConfig                GroupModelsListConfig             `json:"models_list_config,omitempty"`
	RPMLimit                        int                               `json:"rpm_limit"`
	MaxReasoningEffort              string                            `json:"max_reasoning_effort,omitempty"`
	ReasoningEffortMappings         []ReasoningEffortMapping          `json:"reasoning_effort_mappings"`
}

// APIKeyAuthCacheEntry 缓存条目，支持负缓存
type APIKeyAuthCacheEntry struct {
	NotFound bool                `json:"not_found"`
	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
}

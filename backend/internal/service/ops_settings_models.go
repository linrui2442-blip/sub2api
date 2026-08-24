package service

// Local observability settings stored in the Personal Edition settings table.

type OpsRuntimeLogConfig struct {
	Level           string         `json:"level"`
	EnableSampling  bool           `json:"enable_sampling"`
	SamplingInitial int            `json:"sampling_initial"`
	SamplingNext    int            `json:"sampling_thereafter"`
	Caller          bool           `json:"caller"`
	StacktraceLevel string         `json:"stacktrace_level"`
	RetentionDays   int            `json:"retention_days"`
	Source          string         `json:"source,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
	UpdatedByUserID int64          `json:"updated_by_user_id,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

// OpsAdvancedSettings stores advanced ops configuration (data retention, aggregation).
type OpsAdvancedSettings struct {
	DataRetention               OpsDataRetentionSettings               `json:"data_retention"`
	OpenAIAccountQuotaAutoPause OpsOpenAIAccountQuotaAutoPauseSettings `json:"openai_account_quota_auto_pause"`
	IgnoreCountTokensErrors     bool                                   `json:"ignore_count_tokens_errors"`
	IgnoreContextCanceled       bool                                   `json:"ignore_context_canceled"`
	IgnoreNoAvailableAccounts   bool                                   `json:"ignore_no_available_accounts"`
	// Deprecated compatibility field. It is always normalized to true.
	IgnoreInvalidApiKeyErrors       bool `json:"ignore_invalid_api_key_errors"`
	IgnoreInsufficientBalanceErrors bool `json:"ignore_insufficient_balance_errors"`
}

type OpsOpenAIAccountQuotaAutoPauseSettings struct {
	DefaultThreshold5h float64 `json:"default_threshold_5h"`
	DefaultThreshold7d float64 `json:"default_threshold_7d"`
}

type OpsDataRetentionSettings struct {
	CleanupEnabled        bool   `json:"cleanup_enabled"`
	CleanupSchedule       string `json:"cleanup_schedule"`
	ErrorLogRetentionDays int    `json:"error_log_retention_days"`
}

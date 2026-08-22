package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

func defaultOpsAdvancedSettings() *OpsAdvancedSettings {
	return &OpsAdvancedSettings{
		DataRetention: OpsDataRetentionSettings{
			CleanupEnabled:        false,
			CleanupSchedule:       opsCleanupDefaultSchedule,
			ErrorLogRetentionDays: 30,
		},
		OpenAIAccountQuotaAutoPause:     OpsOpenAIAccountQuotaAutoPauseSettings{},
		IgnoreCountTokensErrors:         true,  // count_tokens 404 是预期行为，默认忽略
		IgnoreContextCanceled:           true,  // Default to true - client disconnects are not errors
		IgnoreNoAvailableAccounts:       false, // Default to false - this is a real routing issue
		IgnoreInvalidApiKeyErrors:       true,  // Legacy compatibility field; admission rejects are always excluded.
		IgnoreInsufficientBalanceErrors: false, // 默认不忽略，余额不足可能需要关注
	}
}

func normalizeOpsAdvancedSettings(cfg *OpsAdvancedSettings) {
	if cfg == nil {
		return
	}
	// Admission rejects are a security/traffic concern, not an operational
	// request-error category. Keep the legacy field true for old clients but do
	// not allow it to re-enable those rows.
	cfg.IgnoreInvalidApiKeyErrors = true
	cfg.OpenAIAccountQuotaAutoPause.DefaultThreshold5h = clampOpsQuotaAutoPauseThreshold(cfg.OpenAIAccountQuotaAutoPause.DefaultThreshold5h)
	cfg.OpenAIAccountQuotaAutoPause.DefaultThreshold7d = clampOpsQuotaAutoPauseThreshold(cfg.OpenAIAccountQuotaAutoPause.DefaultThreshold7d)
	cfg.DataRetention.CleanupSchedule = strings.TrimSpace(cfg.DataRetention.CleanupSchedule)
	if cfg.DataRetention.CleanupSchedule == "" {
		cfg.DataRetention.CleanupSchedule = opsCleanupDefaultSchedule
	}
	// 保留天数：0 表示每次定时清理全部（清空所有），> 0 表示按天数保留；
	// 仅在拿到非法的负数时回填默认值，避免覆盖用户主动设的 0。
	if cfg.DataRetention.ErrorLogRetentionDays < 0 {
		cfg.DataRetention.ErrorLogRetentionDays = 30
	}
}

func clampOpsQuotaAutoPauseThreshold(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func validateOpsAdvancedSettings(cfg *OpsAdvancedSettings) error {
	if cfg == nil {
		return errors.New("invalid config")
	}
	// 保留天数：0 表示每次清理全部，1-365 表示按天数保留。
	if cfg.DataRetention.ErrorLogRetentionDays < 0 || cfg.DataRetention.ErrorLogRetentionDays > 365 {
		return errors.New("error_log_retention_days must be between 0 and 365")
	}
	return nil
}

func (s *OpsService) GetOpsAdvancedSettings(ctx context.Context) (*OpsAdvancedSettings, error) {
	_ = ctx
	cfg := s.OpsAdvancedSettingsSnapshot()
	return &cfg, nil
}

// OpsAdvancedSettingsSnapshot returns a value copy for request hot paths. It
// avoids both repository I/O and pointer escape/allocation.
func (s *OpsService) OpsAdvancedSettingsSnapshot() OpsAdvancedSettings {
	if s != nil {
		if snapshot := s.runtimeSettings.Load(); snapshot != nil {
			return snapshot.advanced
		}
	}
	return *defaultOpsAdvancedSettings()
}

func (s *OpsService) UpdateOpsAdvancedSettings(ctx context.Context, cfg *OpsAdvancedSettings) (*OpsAdvancedSettings, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("setting repository not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return nil, errors.New("invalid config")
	}

	if err := validateOpsAdvancedSettings(cfg); err != nil {
		return nil, err
	}

	normalizeOpsAdvancedSettings(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpsAdvancedSettings, string(raw)); err != nil {
		return nil, err
	}
	s.storeAdvancedSettingsSnapshot(cfg)
	// Push the new quota auto-pause settings straight into the in-memory cache that
	// the OpenAI scheduling hot path reads, so the next request observes the new value
	// without waiting for the background refresher's TTL.
	if s.quotaAutoPauseSink != nil {
		s.quotaAutoPauseSink(cfg.OpenAIAccountQuotaAutoPause)
	}

	// notify cleanup service to reload schedule/enabled.
	if s.cleanupReloader != nil {
		if rerr := s.cleanupReloader.Reload(ctx); rerr != nil {
			logger.LegacyPrintf("service.ops_settings",
				"[OpsSettings] cleanup reload after advanced-settings update failed: %v", rerr)
		}
	}

	updated := &OpsAdvancedSettings{}
	_ = json.Unmarshal(raw, updated)
	return updated, nil
}

// =========================
// Metric thresholds
// =========================

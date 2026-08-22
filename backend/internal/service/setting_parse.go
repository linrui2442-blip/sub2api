package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// InitializeDefaultSettings 初始化默认设置
func (s *SettingService) InitializeDefaultSettings(ctx context.Context) error {
	// 检查是否已有设置
	_, err := s.settingRepo.GetValue(ctx, SettingKeyRegistrationEnabled)
	if err == nil {
		// 已有设置，不需要初始化
		return nil
	}
	if !errors.Is(err, ErrSettingNotFound) {
		return fmt.Errorf("check existing settings: %w", err)
	}

	loginAgreementDocumentsJSON, err := marshalLoginAgreementDocuments(defaultLoginAgreementDocuments())
	if err != nil {
		return err
	}
	forwardedClientIPHeaders := []string{}
	if s != nil && s.cfg != nil {
		forwardedClientIPHeaders = s.cfg.ForwardedClientIPSettings().Headers
	}
	forwardedClientIPHeadersJSON, err := json.Marshal(forwardedClientIPHeaders)
	if err != nil {
		return fmt.Errorf("marshal default forwarded client IP headers: %w", err)
	}

	// 初始化默认设置
	defaults := map[string]string{
		SettingKeyRegistrationEnabled:                 "true",
		SettingKeyEmailVerifyEnabled:                  "false",
		SettingKeyRegistrationEmailSuffixWhitelist:    "[]",
		SettingKeyRegistrationEmailDomainQuotaEnabled: "false",
		SettingKeyLoginAgreementEnabled:               "false",
		SettingKeyLoginAgreementMode:                  defaultLoginAgreementMode,
		SettingKeyLoginAgreementUpdatedAt:             defaultLoginAgreementDate,
		SettingKeyLoginAgreementDocuments:             loginAgreementDocumentsJSON,
		SettingKeyAPIKeyACLTrustForwardedIP:           "true",
		SettingKeyForwardedClientIPHeaders:            string(forwardedClientIPHeadersJSON),
		settingKeyForwardedClientIPModeV2:             "true",
		SettingKeySiteName:                            "Sub2API",
		SettingKeySiteLogo:                            "",
		SettingKeyTableDefaultPageSize:                "20",
		SettingKeyTablePageSizeOptions:                "[10,20,50,100]",
		SettingKeyCustomMenuItems:                     "[]",
		SettingKeyCustomEndpoints:                     "[]",
		SettingKeyDefaultConcurrency:                  strconv.Itoa(s.cfg.Default.UserConcurrency),
		SettingKeyDefaultUserRPMLimit:                 "0",
		SettingKeySMTPPort:                            "587",
		SettingKeySMTPUseTLS:                          "false",
		// Model fallback defaults
		SettingKeyEnableModelFallback:      "false",
		SettingKeyFallbackModelAnthropic:   "claude-3-5-sonnet-20241022",
		SettingKeyFallbackModelOpenAI:      "gpt-4o",
		SettingKeyFallbackModelGemini:      "gemini-2.5-pro",
		SettingKeyFallbackModelAntigravity: "gemini-2.5-pro",
		// Identity patch defaults
		SettingKeyEnableIdentityPatch: "true",
		SettingKeyIdentityPatchPrompt: "",

		// Ops monitoring defaults (vNext)
		SettingKeyOpsMonitoringEnabled: "true",

		// Grok: safe defaults — no cross-vendor model rewrite unless operators enable it.
		SettingKeyGrokDefaultTextModel:           "grok-4.5",
		SettingKeyGrokCrossClientModelMapEnabled: "true",
		SettingKeyGrokDefaultBaseURLMode:         GrokDefaultBaseURLModeCLI,

		// Affiliate (邀请返利) feature (default disabled; opt-in)

		// 风控中心功能（默认关闭，显式启用）
		SettingKeyRiskControlEnabled: "false",

		// cyber 会话屏蔽（默认关闭，TTL 默认 3600s）
		SettingKeyCyberSessionBlockEnabled:    "false",
		SettingKeyCyberSessionBlockTTLSeconds: "3600",

		// Claude Code version check (default: empty = disabled)
		SettingKeyMinClaudeCodeVersion: "",
		SettingKeyMaxClaudeCodeVersion: "",

		// codex_cli_only 加固（默认：版本不检查、名单空、默认种子指纹信号）
		SettingKeyMinCodexVersion:                      "",
		SettingKeyMaxCodexVersion:                      "",
		SettingKeyCodexCLIOnlyBlacklist:                "",
		SettingKeyCodexCLIOnlyWhitelist:                "",
		SettingKeyCodexCLIOnlyAllowAppServerClients:    "false",
		SettingKeyCodexCLIOnlyEngineFingerprintSignals: openai.DefaultEngineFingerprintSignalsJSON(),

		// 分组隔离（默认不允许未分组 Key 调度）
		SettingKeyAllowUngroupedKeyScheduling:                        "false",
		SettingKeyOpenAILowUpstreamRatePriorityEnabled:               "false",
		SettingKeyOpenAIOAuthSchedulingRateMultiplier:                "1",
		SettingKeyEnableAnthropicCacheTTL1hInjection:                 "false",
		SettingKeyRewriteMessageCacheControl:                         strconv.FormatBool(s.defaultRewriteMessageCacheControl()),
		SettingKeyEnableClientDatelineNormalization:                  "true",
		SettingKeyAntigravityUserAgentVersion:                        "",
		SettingKeyOpenAICodexUserAgent:                               "",
		SettingKeyOpenAICodexClientVersion:                           "",
		SettingKeyOpenAICodexClientVersionSynced:                     "",
		SettingKeyOpenAICodexVersionAutoSyncEnabled:                  "true",
		openAIAdvancedSchedulerSettingKey:                            "false",
		SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled:       "false",
		SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled: "false",
		SettingKeyOpenAIAdvancedSchedulerLBTopK:                      "",
		SettingKeyOpenAIAdvancedSchedulerWeightPriority:              "",
		SettingKeyOpenAIAdvancedSchedulerWeightLoad:                  "",
		SettingKeyOpenAIAdvancedSchedulerWeightQueue:                 "",
		SettingKeyOpenAIAdvancedSchedulerWeightErrorRate:             "",
		SettingKeyOpenAIAdvancedSchedulerWeightTTFT:                  "",
		SettingKeyOpenAIAdvancedSchedulerWeightReset:                 "",
		SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom:         "",
		SettingKeyOpenAIAdvancedSchedulerWeightUpstreamCost:          "",
		SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse:      "",
		SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky:         "",

		SettingKeyAllowUserViewErrorRequests: "false",
	}

	return s.settingRepo.SetMultiple(ctx, defaults)
}

func parseForwardedClientIPHeadersSetting(value string) ([]string, error) {
	var headers []string
	if err := json.Unmarshal([]byte(value), &headers); err != nil {
		return nil, fmt.Errorf("parse forwarded_client_ip_headers: %w", err)
	}
	if headers == nil {
		return nil, fmt.Errorf("parse forwarded_client_ip_headers: value must be a JSON array")
	}
	normalized, err := config.NormalizeForwardedClientIPHeaders(headers)
	if err != nil {
		return nil, fmt.Errorf("parse forwarded_client_ip_headers: %w", err)
	}
	return normalized, nil
}

// parseSettings 解析设置到结构体
func (s *SettingService) parseSettings(settings map[string]string) *SystemSettings {
	emailVerifyEnabled := settings[SettingKeyEmailVerifyEnabled] == "true"
	loginAgreementDocuments := parseLoginAgreementDocuments(settings[SettingKeyLoginAgreementDocuments])
	loginAgreementUpdatedAt := strings.TrimSpace(settings[SettingKeyLoginAgreementUpdatedAt])
	if loginAgreementUpdatedAt == "" {
		loginAgreementUpdatedAt = defaultLoginAgreementDate
	}
	apiKeyACLTrustForwardedIP := false
	forwardedClientIPHeaders := []string{}
	if s != nil && s.cfg != nil {
		runtimeSettings := s.cfg.ForwardedClientIPSettings()
		apiKeyACLTrustForwardedIP = runtimeSettings.TrustForwardedIP
		forwardedClientIPHeaders = runtimeSettings.Headers
	}
	if value, ok := settings[SettingKeyAPIKeyACLTrustForwardedIP]; ok {
		apiKeyACLTrustForwardedIP = value == "true"
	}
	if value, ok := settings[SettingKeyForwardedClientIPHeaders]; ok {
		parsed, err := parseForwardedClientIPHeadersSetting(value)
		if err != nil {
			slog.Error("invalid persisted forwarded client IP headers; forwarded trust disabled", "error", err)
			apiKeyACLTrustForwardedIP = false
			forwardedClientIPHeaders = []string{}
		} else {
			forwardedClientIPHeaders = parsed
		}
	}
	result := &SystemSettings{
		RegistrationEnabled:                 settings[SettingKeyRegistrationEnabled] == "true",
		EmailVerifyEnabled:                  emailVerifyEnabled,
		RegistrationEmailSuffixWhitelist:    ParseRegistrationEmailSuffixWhitelist(settings[SettingKeyRegistrationEmailSuffixWhitelist]),
		RegistrationEmailDomainQuotaEnabled: settings[SettingKeyRegistrationEmailDomainQuotaEnabled] == "true",
		PasswordResetEnabled:                emailVerifyEnabled && settings[SettingKeyPasswordResetEnabled] == "true",
		FrontendURL:                         settings[SettingKeyFrontendURL],
		InvitationCodeEnabled:               settings[SettingKeyInvitationCodeEnabled] == "true",
		TotpEnabled:                         settings[SettingKeyTotpEnabled] == "true",
		PasskeyEnabled:                      s.passkeySettingEnabled(settings),
		SessionBindingEnabled:               settings[SettingKeySessionBindingEnabled] == "true", // 默认关闭
		StepUpEnabled:                       settings[SettingKeyStepUpEnabled] == "true",         // 默认关闭
		AuditLogRetentionDays:               parseAuditLogRetentionDays(settings[SettingKeyAuditLogRetentionDays]),
		LoginAgreementEnabled:               settings[SettingKeyLoginAgreementEnabled] == "true",
		LoginAgreementMode:                  normalizeLoginAgreementMode(settings[SettingKeyLoginAgreementMode]),
		LoginAgreementUpdatedAt:             loginAgreementUpdatedAt,
		LoginAgreementDocuments:             loginAgreementDocuments,
		SMTPHost:                            settings[SettingKeySMTPHost],
		SMTPUsername:                        settings[SettingKeySMTPUsername],
		SMTPFrom:                            settings[SettingKeySMTPFrom],
		SMTPFromName:                        settings[SettingKeySMTPFromName],
		SMTPUseTLS:                          settings[SettingKeySMTPUseTLS] == "true",
		SMTPPasswordConfigured:              settings[SettingKeySMTPPassword] != "",
		APIKeyACLTrustForwardedIP:           apiKeyACLTrustForwardedIP,
		ForwardedClientIPHeaders:            forwardedClientIPHeaders,
		SiteName:                            s.getStringOrDefault(settings, SettingKeySiteName, "Sub2API"),
		SiteLogo:                            settings[SettingKeySiteLogo],
		SiteSubtitle:                        s.getStringOrDefault(settings, SettingKeySiteSubtitle, "Subscription to API Conversion Platform"),
		APIBaseURL:                          settings[SettingKeyAPIBaseURL],
		ContactInfo:                         settings[SettingKeyContactInfo],
		DocURL:                              settings[SettingKeyDocURL],
		HomeContent:                         settings[SettingKeyHomeContent],
		CompactHomeEnabled:                  settings[SettingKeyCompactHomeEnabled] == "true",
		HideCcsImportButton:                 settings[SettingKeyHideCcsImportButton] == "true",
		CustomMenuItems:                     settings[SettingKeyCustomMenuItems],
		CustomEndpoints:                     settings[SettingKeyCustomEndpoints],
		BackendModeEnabled:                  settings[SettingKeyBackendModeEnabled] == "true",
	}
	result.TableDefaultPageSize, result.TablePageSizeOptions = parseTablePreferences(
		settings[SettingKeyTableDefaultPageSize],
		settings[SettingKeyTablePageSizeOptions],
	)

	// 解析整数类型
	if port, err := strconv.Atoi(settings[SettingKeySMTPPort]); err == nil {
		result.SMTPPort = port
	} else {
		result.SMTPPort = 587
	}

	if concurrency, err := strconv.Atoi(settings[SettingKeyDefaultConcurrency]); err == nil {
		result.DefaultConcurrency = concurrency
	} else {
		result.DefaultConcurrency = s.cfg.Default.UserConcurrency
	}

	if rpm, err := strconv.Atoi(settings[SettingKeyDefaultUserRPMLimit]); err == nil && rpm >= 0 {
		result.DefaultUserRPMLimit = rpm
	}

	// 敏感信息直接返回，方便测试连接时使用
	result.SMTPPassword = settings[SettingKeySMTPPassword]
	// Model fallback settings
	result.EnableModelFallback = settings[SettingKeyEnableModelFallback] == "true"
	result.FallbackModelAnthropic = s.getStringOrDefault(settings, SettingKeyFallbackModelAnthropic, "claude-3-5-sonnet-20241022")
	result.FallbackModelOpenAI = s.getStringOrDefault(settings, SettingKeyFallbackModelOpenAI, "gpt-4o")
	result.FallbackModelGemini = s.getStringOrDefault(settings, SettingKeyFallbackModelGemini, "gemini-2.5-pro")
	result.FallbackModelAntigravity = s.getStringOrDefault(settings, SettingKeyFallbackModelAntigravity, "gemini-2.5-pro")

	// Identity patch settings (default: enabled, to preserve existing behavior)
	if v, ok := settings[SettingKeyEnableIdentityPatch]; ok && v != "" {
		result.EnableIdentityPatch = v == "true"
	} else {
		result.EnableIdentityPatch = true
	}
	result.IdentityPatchPrompt = settings[SettingKeyIdentityPatchPrompt]

	// Ops monitoring settings (default: enabled, fail-open)
	result.OpsMonitoringEnabled = !isFalseSettingValue(settings[SettingKeyOpsMonitoringEnabled])

	// Grok default mapping policy
	result.GrokDefaultTextModel = strings.TrimSpace(settings[SettingKeyGrokDefaultTextModel])
	if result.GrokDefaultTextModel == "" {
		result.GrokDefaultTextModel = "grok-4.5"
	}
	// Default true (missing/empty → enabled) so Claude/Codex→Grok mapping keeps working.
	// Operators can set false to disable silent cross-client rewrite.
	result.GrokCrossClientModelMapEnabled = !isFalseSettingValue(settings[SettingKeyGrokCrossClientModelMapEnabled])
	result.GrokDefaultBaseURLMode = normalizeGrokDefaultBaseURLMode(settings[SettingKeyGrokDefaultBaseURLMode])

	// 风控中心功能（默认关闭，严格 true 才启用）
	result.RiskControlEnabled = settings[SettingKeyRiskControlEnabled] == "true"

	// cyber 会话屏蔽（默认关闭，TTL 默认 3600s）
	result.CyberSessionBlockEnabled = settings[SettingKeyCyberSessionBlockEnabled] == "true"
	if v, err := strconv.Atoi(strings.TrimSpace(settings[SettingKeyCyberSessionBlockTTLSeconds])); err == nil && v > 0 {
		result.CyberSessionBlockTTLSeconds = v
	} else {
		result.CyberSessionBlockTTLSeconds = 3600
	}

	// Claude Code version check
	result.MinClaudeCodeVersion = settings[SettingKeyMinClaudeCodeVersion]
	result.MaxClaudeCodeVersion = settings[SettingKeyMaxClaudeCodeVersion]

	// 分组隔离
	result.AllowUngroupedKeyScheduling = settings[SettingKeyAllowUngroupedKeyScheduling] == "true"

	// Gateway forwarding behavior (defaults: fingerprint=true, metadata_passthrough=false,
	// cch_signing=false, claude_oauth_system_prompt_injection=true)
	if v, ok := settings[SettingKeyEnableFingerprintUnification]; ok && v != "" {
		result.EnableFingerprintUnification = v == "true"
	} else {
		result.EnableFingerprintUnification = true // default: enabled (current behavior)
	}
	result.EnableMetadataPassthrough = settings[SettingKeyEnableMetadataPassthrough] == "true"
	result.EnableCCHSigning = settings[SettingKeyEnableCCHSigning] == "true"
	if v, ok := settings[SettingKeyEnableClaudeOAuthSystemPromptInjection]; ok && v != "" {
		result.EnableClaudeOAuthSystemPromptInjection = v == "true"
	} else {
		result.EnableClaudeOAuthSystemPromptInjection = true
	}
	result.ClaudeOAuthSystemPrompt = settings[SettingKeyClaudeOAuthSystemPrompt]
	result.ClaudeOAuthSystemPromptBlocks = settings[SettingKeyClaudeOAuthSystemPromptBlocks]
	result.EnableAnthropicCacheTTL1hInjection = settings[SettingKeyEnableAnthropicCacheTTL1hInjection] == "true"
	if v, ok := settings[SettingKeyRewriteMessageCacheControl]; ok && v != "" {
		result.RewriteMessageCacheControl = v == "true"
	} else {
		result.RewriteMessageCacheControl = s.defaultRewriteMessageCacheControl()
	}
	if v, ok := settings[SettingKeyEnableClientDatelineNormalization]; ok && v != "" {
		result.EnableClientDatelineNormalization = v == "true"
	} else {
		result.EnableClientDatelineNormalization = true
	}
	result.AntigravityUserAgentVersion = antigravity.NormalizeUserAgentVersion(settings[SettingKeyAntigravityUserAgentVersion])
	result.OpenAICodexUserAgent = strings.TrimSpace(settings[SettingKeyOpenAICodexUserAgent])
	result.OpenAICodexClientVersion = NormalizeCodexClientVersion(settings[SettingKeyOpenAICodexClientVersion])
	result.OpenAICodexClientVersionSynced = NormalizeCodexClientVersion(settings[SettingKeyOpenAICodexClientVersionSynced])
	// 自动同步默认开启：缺失/空值一律视为开启，与 enable_client_dateline_normalization 同一惯例。
	if v, ok := settings[SettingKeyOpenAICodexVersionAutoSyncEnabled]; ok && v != "" {
		result.OpenAICodexVersionAutoSyncEnabled = v == "true"
	} else {
		result.OpenAICodexVersionAutoSyncEnabled = true
	}
	// codex_cli_only 加固
	result.MinCodexVersion = settings[SettingKeyMinCodexVersion]
	result.MaxCodexVersion = settings[SettingKeyMaxCodexVersion]
	result.CodexCLIOnlyBlacklist = settings[SettingKeyCodexCLIOnlyBlacklist]
	result.CodexCLIOnlyWhitelist = settings[SettingKeyCodexCLIOnlyWhitelist]
	result.CodexCLIOnlyAllowAppServerClients = settings[SettingKeyCodexCLIOnlyAllowAppServerClients] == "true"
	if raw := strings.TrimSpace(settings[SettingKeyCodexCLIOnlyEngineFingerprintSignals]); raw != "" {
		result.CodexCLIOnlyEngineFingerprintSignals = raw
	} else {
		result.CodexCLIOnlyEngineFingerprintSignals = openai.DefaultEngineFingerprintSignalsJSON() // 缺失/空 → 展示默认种子
	}

	result.OpenAILowUpstreamRatePriorityEnabled = settings[SettingKeyOpenAILowUpstreamRatePriorityEnabled] == "true"
	result.OpenAIOAuthSchedulingRateMultiplier = parseOpenAIOAuthSchedulingRateMultiplier(settings[SettingKeyOpenAIOAuthSchedulingRateMultiplier])
	result.OpenAIAdvancedSchedulerEnabled = settings[openAIAdvancedSchedulerSettingKey] == "true"
	result.OpenAIAdvancedSchedulerStickyWeightedEnabled = settings[SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled] == "true"
	result.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled = settings[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled] == "true"
	result.OpenAIAdvancedSchedulerLBTopK = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerLBTopK])
	result.OpenAIAdvancedSchedulerWeightPriority = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightPriority])
	result.OpenAIAdvancedSchedulerWeightLoad = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightLoad])
	result.OpenAIAdvancedSchedulerWeightQueue = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightQueue])
	result.OpenAIAdvancedSchedulerWeightErrorRate = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightErrorRate])
	result.OpenAIAdvancedSchedulerWeightTTFT = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightTTFT])
	result.OpenAIAdvancedSchedulerWeightReset = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightReset])
	result.OpenAIAdvancedSchedulerWeightQuotaHeadroom = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom])
	result.OpenAIAdvancedSchedulerWeightUpstreamCost = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightUpstreamCost])
	result.OpenAIAdvancedSchedulerWeightPreviousResponse = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse])
	result.OpenAIAdvancedSchedulerWeightSessionSticky = strings.TrimSpace(settings[SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky])
	result.OpenAIAdvancedSchedulerEffectiveLBTopK = s.openAIAdvancedSchedulerEffectiveLBTopK()
	effectiveWeights := s.openAIAdvancedSchedulerEffectiveWeights()
	result.OpenAIAdvancedSchedulerEffectiveWeightPriority = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.Priority)
	result.OpenAIAdvancedSchedulerEffectiveWeightLoad = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.Load)
	result.OpenAIAdvancedSchedulerEffectiveWeightQueue = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.Queue)
	result.OpenAIAdvancedSchedulerEffectiveWeightErrorRate = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.ErrorRate)
	result.OpenAIAdvancedSchedulerEffectiveWeightTTFT = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.TTFT)
	result.OpenAIAdvancedSchedulerEffectiveWeightReset = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.Reset)
	result.OpenAIAdvancedSchedulerEffectiveWeightQuotaHeadroom = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.QuotaHeadroom)
	result.OpenAIAdvancedSchedulerEffectiveWeightUpstreamCost = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.UpstreamCost)
	result.OpenAIAdvancedSchedulerEffectiveWeightPreviousResponse = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.PreviousResponse)
	result.OpenAIAdvancedSchedulerEffectiveWeightSessionSticky = formatOpenAIAdvancedSchedulerFloat(effectiveWeights.SessionSticky)

	// 账号限额通知
	result.AccountQuotaNotifyEnabled = settings[SettingKeyAccountQuotaNotifyEnabled] == "true"
	if raw := strings.TrimSpace(settings[SettingKeyAccountQuotaNotifyEmails]); raw != "" {
		result.AccountQuotaNotifyEmails = ParseNotifyEmails(raw)
	}
	if result.AccountQuotaNotifyEmails == nil {
		result.AccountQuotaNotifyEmails = []NotifyEmailEntry{}
	}

	result.AccountSchedulingThresholds = defaultAccountSchedulingThresholds()
	if raw := strings.TrimSpace(settings[SettingKeyAccountSchedulingThresholds]); raw != "" {
		if thresholds, err := parseAccountSchedulingThresholdsSetting(raw); err != nil {
			slog.Warn("[Setting] parseSettings: unmarshal account_scheduling_thresholds failed", "error", err)
		} else {
			result.AccountSchedulingThresholds = thresholds
		}
	}

	result.AllowUserViewErrorRequests = settings[SettingKeyAllowUserViewErrorRequests] == "true" // default false

	// Publish Grok default model_mapping options for accounts with empty mapping.
	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{
		DefaultText:          result.GrokDefaultTextModel,
		EnableCrossClientMap: result.GrokCrossClientModelMapEnabled,
	})

	return result
}

func isFalseSettingValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "off", "disabled":
		return true
	default:
		return false
	}
}

func (s *SettingService) openAIAdvancedSchedulerEffectiveLBTopK() string {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.LBTopK > 0 {
		return strconv.Itoa(s.cfg.Gateway.OpenAIWS.LBTopK)
	}
	return "7"
}

func (s *SettingService) openAIAdvancedSchedulerEffectiveWeights() config.GatewayOpenAIWSSchedulerScoreWeights {
	defaults := config.GatewayOpenAIWSSchedulerScoreWeights{
		Priority:         1.0,
		Load:             1.0,
		Queue:            0.7,
		ErrorRate:        0.8,
		TTFT:             0.5,
		Reset:            0.0,
		QuotaHeadroom:    0.0,
		UpstreamCost:     0.0,
		PreviousResponse: 5.0,
		SessionSticky:    3.0,
	}
	if s == nil || s.cfg == nil {
		return defaults
	}

	weights := s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights
	if !weights.IsValid() {
		return defaults
	}
	return weights
}

func formatOpenAIAdvancedSchedulerFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func (s *SettingService) normalizeOpenAIAdvancedSchedulerOverrides(settings *SystemSettings) error {
	if rate := settings.OpenAIOAuthSchedulingRateMultiplier; rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return infraerrors.BadRequest("INVALID_OPENAI_OAUTH_SCHEDULING_RATE_MULTIPLIER", "OpenAI OAuth scheduling rate multiplier must be a finite non-negative number")
	}

	lbTopK, err := normalizeOptionalPositiveIntString(settings.OpenAIAdvancedSchedulerLBTopK)
	if err != nil {
		return infraerrors.BadRequest("INVALID_OPENAI_ADVANCED_SCHEDULER_LB_TOP_K", "openai advanced scheduler TopK must be a positive integer or empty")
	}
	settings.OpenAIAdvancedSchedulerLBTopK = lbTopK

	weights := []*string{
		&settings.OpenAIAdvancedSchedulerWeightPriority,
		&settings.OpenAIAdvancedSchedulerWeightLoad,
		&settings.OpenAIAdvancedSchedulerWeightQueue,
		&settings.OpenAIAdvancedSchedulerWeightErrorRate,
		&settings.OpenAIAdvancedSchedulerWeightTTFT,
		&settings.OpenAIAdvancedSchedulerWeightReset,
		&settings.OpenAIAdvancedSchedulerWeightQuotaHeadroom,
		&settings.OpenAIAdvancedSchedulerWeightUpstreamCost,
		&settings.OpenAIAdvancedSchedulerWeightPreviousResponse,
		&settings.OpenAIAdvancedSchedulerWeightSessionSticky,
	}
	for _, target := range weights {
		normalized, err := normalizeOptionalNonNegativeFloatString(*target)
		if err != nil {
			return infraerrors.BadRequest("INVALID_OPENAI_ADVANCED_SCHEDULER_WEIGHT", "openai advanced scheduler weights must be non-negative numbers or empty")
		}
		*target = normalized
	}

	// 与 config.Validate 的 "scheduler_score_weights must not all be zero" 保持一致：
	// 覆盖值（空则回退到生效的配置值）叠加后的基础权重和不允许为 0，
	// 否则调度会静默退化为 TopK 内均匀随机。
	effective := s.openAIAdvancedSchedulerEffectiveWeights()
	resolved := config.GatewayOpenAIWSSchedulerScoreWeights{
		Priority:         resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightPriority, effective.Priority),
		Load:             resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightLoad, effective.Load),
		Queue:            resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightQueue, effective.Queue),
		ErrorRate:        resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightErrorRate, effective.ErrorRate),
		TTFT:             resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightTTFT, effective.TTFT),
		Reset:            resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightReset, effective.Reset),
		QuotaHeadroom:    resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightQuotaHeadroom, effective.QuotaHeadroom),
		UpstreamCost:     resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightUpstreamCost, effective.UpstreamCost),
		PreviousResponse: resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightPreviousResponse, effective.PreviousResponse),
		SessionSticky:    resolveOpenAIAdvancedSchedulerWeight(settings.OpenAIAdvancedSchedulerWeightSessionSticky, effective.SessionSticky),
	}
	if !resolved.IsValid() {
		return infraerrors.BadRequest("INVALID_OPENAI_ADVANCED_SCHEDULER_WEIGHT", "openai advanced scheduler weights must have finite non-zero base and total sums")
	}
	return nil
}

func parseOpenAIOAuthSchedulingRateMultiplier(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return defaultOpenAIOAuthSchedulingRateMultiplier
	}
	return value
}

// resolveOpenAIAdvancedSchedulerWeight 返回覆盖值（已归一化的非空字符串），空则回退默认值。
func resolveOpenAIAdvancedSchedulerWeight(normalized string, fallback float64) float64 {
	if normalized == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return fallback
	}
	return value
}

func normalizeOptionalPositiveIntString(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return "", fmt.Errorf("invalid positive integer")
	}
	return strconv.Itoa(value), nil
}

func normalizeOptionalNonNegativeFloatString(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("invalid non-negative float")
	}
	return strconv.FormatFloat(value, 'f', -1, 64), nil
}

func parseTablePreferences(defaultPageSizeRaw, optionsRaw string) (int, []int) {
	defaultPageSize := 20
	if v, err := strconv.Atoi(strings.TrimSpace(defaultPageSizeRaw)); err == nil {
		defaultPageSize = v
	}

	var options []int
	if strings.TrimSpace(optionsRaw) != "" {
		_ = json.Unmarshal([]byte(optionsRaw), &options)
	}

	return normalizeTablePreferences(defaultPageSize, options)
}

func normalizeTablePreferences(defaultPageSize int, options []int) (int, []int) {
	const minPageSize = 5
	const maxPageSize = 1000
	const fallbackPageSize = 20

	seen := make(map[int]struct{}, len(options))
	normalizedOptions := make([]int, 0, len(options))
	for _, option := range options {
		if option < minPageSize || option > maxPageSize {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		normalizedOptions = append(normalizedOptions, option)
	}
	sort.Ints(normalizedOptions)

	if defaultPageSize < minPageSize || defaultPageSize > maxPageSize {
		defaultPageSize = fallbackPageSize
	}

	if len(normalizedOptions) == 0 {
		normalizedOptions = []int{10, 20, 50}
	}

	return defaultPageSize, normalizedOptions
}

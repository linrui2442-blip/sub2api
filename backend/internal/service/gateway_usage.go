package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type gatewayRequestStartedAtKey struct{}
type openAIRequestStartedAtKey struct{}

// WithGatewayTokenRequestPricing freezes a request timestamp used by usage and
// audit records. The legacy name is retained until handler call sites migrate.
func WithGatewayTokenRequestPricing(ctx context.Context) (context.Context, time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	startedAt := time.Now()
	return context.WithValue(ctx, gatewayRequestStartedAtKey{}, startedAt), startedAt
}

func GatewayTokenRequestPricingAtFromContext(ctx context.Context) time.Time {
	if ctx == nil {
		return time.Time{}
	}
	startedAt, _ := ctx.Value(gatewayRequestStartedAtKey{}).(time.Time)
	return startedAt
}

// WithOpenAIRequestPricingContext freezes the operational request timestamp.
// groupID remains in the signature while handlers migrate away from pricing terminology.
func (s *OpenAIGatewayService) WithOpenAIRequestPricingContext(ctx context.Context, groupID *int64) (context.Context, time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	startedAt := time.Now()
	return context.WithValue(ctx, openAIRequestStartedAtKey{}, startedAt), startedAt
}

func (s *OpenAIGatewayService) WithOpenAITurnPricingContext(ctx context.Context, groupID *int64) (context.Context, time.Time) {
	return s.WithOpenAIRequestPricingContext(ctx, groupID)
}

func OpenAIPricingAtFromContext(ctx context.Context) time.Time {
	if ctx == nil {
		return time.Time{}
	}
	startedAt, _ := ctx.Value(openAIRequestStartedAtKey{}).(time.Time)
	return startedAt
}

func HashUsageRequestPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// UsageTokens is the provider-neutral token telemetry captured for a gateway
// request. It deliberately contains no monetary or subscription fields.
type UsageTokens struct {
	InputTokens           int
	ImageInputTokens      int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	CacheCreation5mTokens int
	CacheCreation1hTokens int
	ImageOutputTokens     int
}

// AccountQuotaState describes provider/account quota windows after an update.
// These values drive health and scheduling notifications, not user billing.
type AccountQuotaState struct {
	TotalUsed   float64
	TotalLimit  float64
	DailyUsed   float64
	DailyLimit  float64
	WeeklyUsed  float64
	WeeklyLimit float64
}

type usageLogBestEffortWriter interface {
	CreateBestEffort(ctx context.Context, log *UsageLog) error
}

// PlatformFromAPIKey 从 APIKey 关联的 Group 推导 platform 名称。
// apiKey 为 nil 或 Group 信息缺失时返回空串（调用方据此 short-circuit quota 累加）。
// 导出供 handler 层调用。

func PlatformFromAPIKey(apiKey *APIKey) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}

// QuotaPlatform 返回 user×platform 配额计量使用的平台标识。
// 强制平台路由（如 /antigravity）优先按 ctx 中的 ForcePlatform 计量，否则回退到
// APIKey 关联 Group 的平台。
//
// 注意：必须用带 ForcePlatform 的请求 context 调用（如 handler 的 c.Request.Context()）。
// 后扣运行在 worker 池的 background ctx 上没有 ForcePlatform，因此后扣平台由 handler
// 预先算定、经 RecordUsageInput.QuotaPlatform 传入，不要在后扣链路用 worker ctx 调用本函数。

func QuotaPlatform(ctx context.Context, apiKey *APIKey) string {
	if ctx != nil {
		if fp, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && fp != "" {
			return fp
		}
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		return platform
	}
	platform := PlatformFromAPIKey(apiKey)
	if platform == PlatformComposite {
		return ""
	}
	return platform
}

func resolveUsageBillingRequestID(ctx context.Context, upstreamRequestID string) string {
	// Forced durable money-event IDs must win over client/local context IDs so
	// standalone web_search / async video cannot collapse under a reused client id.
	if requestID := strings.TrimSpace(upstreamRequestID); requestID != "" {
		if isForcedUsageBillingRequestID(requestID) {
			return requestID
		}
	}
	if ctx != nil {
		if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			return "client:" + strings.TrimSpace(clientRequestID)
		}
		if requestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
			return "local:" + strings.TrimSpace(requestID)
		}
	}
	if requestID := strings.TrimSpace(upstreamRequestID); requestID != "" {
		return requestID
	}
	return "generated:" + generateRequestID()
}

func isForcedUsageBillingRequestID(requestID string) bool {
	id := strings.TrimSpace(requestID)
	return strings.HasPrefix(id, "web_search:") ||
		strings.HasPrefix(id, "grok-video:") ||
		strings.HasPrefix(id, "grok_audio:") ||
		strings.HasPrefix(id, "grok_realtime:")
}

// StableGrokAudioBillingRequestID is the durable usage_logs / dedup key for one
// voice HTTP call (TTS/STT). Prefer an upstream request id when present.

func StableGrokAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "grok_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "grok_audio:" + upstreamRequestID
}

// StableGrokRealtimeBillingRequestID is the durable usage_logs / dedup key for
// one realtime WebSocket session.

func StableGrokRealtimeBillingRequestID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if strings.HasPrefix(sessionID, "grok_realtime:") {
		return sessionID
	}
	if sessionID == "" {
		sessionID = generateRequestID()
	}
	return "grok_realtime:" + sessionID
}

func detachedBillingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, postUsageBillingTimeout)
}

func detachStreamUpstreamContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.Background(), func() {}
	}
	if !stream {
		return ctx, func() {}
	}
	return context.WithoutCancel(ctx), func() {}
}

func detachUpstreamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.Background(), func() {}
	}
	return context.WithoutCancel(ctx), func() {}
}

func writeUsageLogBestEffort(ctx context.Context, repo UsageLogRepository, usageLog *UsageLog, logKey string) {
	if repo == nil || usageLog == nil {
		return
	}
	usageCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	if writer, ok := repo.(usageLogBestEffortWriter); ok {
		if err := writer.CreateBestEffort(usageCtx, usageLog); err != nil {
			logger.LegacyPrintf(logKey, "Create usage log failed: %v", err)
			// 计费已在此前完成，日志必须落库：dropped（批处理队列超时）同样走同步兜底，
			// 否则会出现“已扣费但无 usage_log”的对账缺口（issue #3656）。
			// 重复写入由 usage_logs 的 ON CONFLICT (request_id, api_key_id) DO NOTHING 防护。
			fallbackCtx := usageCtx
			if usageCtx.Err() != nil {
				// usageCtx 已耗尽（best-effort 入队阻塞到期限）：换新的 detached 窗口，避免兜底必然失败。
				var fallbackCancel context.CancelFunc
				fallbackCtx, fallbackCancel = detachedBillingContext(context.Background())
				defer fallbackCancel()
			}
			if _, syncErr := repo.Create(fallbackCtx, usageLog); syncErr != nil {
				logger.LegacyPrintf(logKey, "Create usage log sync fallback failed: %v", syncErr)
			}
		}
		return
	}

	if _, err := repo.Create(usageCtx, usageLog); err != nil {
		logger.LegacyPrintf(logKey, "Create usage log failed: %v", err)
	}
}

type APIKeyQuotaUpdater interface {
	UpdateQuotaUsed(ctx context.Context, apiKeyID int64, cost float64) error
	UpdateRateLimitUsage(ctx context.Context, apiKeyID int64, cost float64) error
}

type RecordUsageInput struct {
	Result             *ForwardResult
	APIKey             *APIKey
	User               *User
	Account            *Account
	Subscription       any
	PricingAt          time.Time
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	SessionID          string
	RequestPayloadHash string
	ForceCacheBilling  bool
	APIKeyService      APIKeyQuotaUpdater
	QuotaPlatform      string
	ChannelUsageFields
}

type RecordUsageLongContextInput struct {
	Result                *ForwardResult
	APIKey                *APIKey
	User                  *User
	Account               *Account
	Subscription          any
	PricingAt             time.Time
	InboundEndpoint       string
	UpstreamEndpoint      string
	UserAgent             string
	IPAddress             string
	SessionID             string
	RequestPayloadHash    string
	LongContextThreshold  int
	LongContextMultiplier float64
	ForceCacheBilling     bool
	APIKeyService         APIKeyQuotaUpdater
	QuotaPlatform         string
	ChannelUsageFields
}

func (s *GatewayService) RecordUsage(ctx context.Context, input *RecordUsageInput) error {
	if input == nil {
		return errors.New("usage input is nil")
	}
	return s.recordOperationalUsage(ctx, input.Result, input.APIKey, input.User, input.Account, input.InboundEndpoint, input.UpstreamEndpoint, input.UserAgent, input.IPAddress, input.SessionID, input.ChannelUsageFields)
}

func (s *GatewayService) RecordUsageWithLongContext(ctx context.Context, input *RecordUsageLongContextInput) error {
	if input == nil {
		return errors.New("usage input is nil")
	}
	return s.recordOperationalUsage(ctx, input.Result, input.APIKey, input.User, input.Account, input.InboundEndpoint, input.UpstreamEndpoint, input.UserAgent, input.IPAddress, input.SessionID, input.ChannelUsageFields)
}

func (s *GatewayService) recordOperationalUsage(ctx context.Context, result *ForwardResult, apiKey *APIKey, user *User, account *Account, inboundEndpoint, upstreamEndpoint, userAgent, ipAddress, sessionID string, channel ChannelUsageFields) error {
	if result == nil {
		return errors.New("usage result is nil")
	}
	if apiKey == nil || user == nil || account == nil {
		return errors.New("usage identity is incomplete")
	}
	requestedModel := result.Model
	if channel.OriginalModel != "" {
		requestedModel = channel.OriginalModel
	}
	sentModel := upstreamSentModel(result.Model, result.UpstreamModel)
	durationMs := int(result.Duration.Milliseconds())
	usageLog := &UsageLog{
		UserID:                user.ID,
		APIKeyID:              apiKey.ID,
		AccountID:             account.ID,
		RequestID:             resolveUsageBillingRequestID(ctx, result.RequestID),
		Model:                 result.Model,
		RequestedModel:        requestedModel,
		UpstreamModel:         optionalTrimmedStringPtr(result.UpstreamModel),
		UpstreamResponseModel: optionalTrimmedStringPtr(result.UpstreamResponseModel),
		UpstreamModelMismatch: upstreamModelMismatch(sentModel, result.UpstreamResponseModel),
		ReasoningEffort:       result.ReasoningEffort,
		InboundEndpoint:       optionalTrimmedStringPtr(inboundEndpoint),
		UpstreamEndpoint:      optionalTrimmedStringPtr(upstreamEndpoint),
		InputTokens:           result.Usage.InputTokens,
		OutputTokens:          result.Usage.OutputTokens,
		CacheCreationTokens:   result.Usage.CacheCreationInputTokens,
		CacheReadTokens:       result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens: result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens: result.Usage.CacheCreation1hTokens,
		ImageOutputTokens:     result.Usage.ImageOutputTokens,
		Stream:                result.Stream,
		DurationMs:            &durationMs,
		FirstTokenMs:          result.FirstTokenMs,
		ImageCount:            result.ImageCount,
		ImageSize:             optionalTrimmedStringPtr(result.ImageSize),
		ImageInputSize:        optionalTrimmedStringPtr(result.ImageInputSize),
		ImageOutputSize:       optionalTrimmedStringPtr(result.ImageOutputSize),
		ImageSizeSource:       optionalTrimmedStringPtr(result.ImageSizeSource),
		ImageSizeBreakdown:    result.ImageSizeBreakdown,
		ChannelID:             optionalInt64Ptr(channel.ChannelID),
		ModelMappingChain:     optionalTrimmedStringPtr(channel.ModelMappingChain),
		UserAgent:             optionalTrimmedStringPtr(userAgent),
		IPAddress:             optionalTrimmedStringPtr(ipAddress),
		SessionID:             optionalTrimmedStringPtr(sessionID),
		GroupID:               apiKey.GroupID,
		CreatedAt:             time.Now(),
	}
	writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")
	if s.deferredService != nil {
		s.deferredService.ScheduleLastUsedUpdate(account.ID)
	}
	return nil
}

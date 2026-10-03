package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ProvideGatewayHandler assembles the protocol-compatible Anthropic/Gemini
// gateway handler used by Personal Edition.
func ProvideGatewayHandler(
	gatewayService *service.GatewayService,
	openAIGatewayService *service.OpenAIGatewayService,
	geminiCompatService *service.GeminiMessagesCompatService,
	antigravityGatewayService *service.AntigravityGatewayService,
	userService *service.UserService,
	concurrencyService *service.ConcurrencyService,
	requestEligibility service.RequestEligibilityChecker,
	usageService *service.UsageService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	userMsgQueueService *service.UserMessageQueueService,
	cfg *config.Config,
	settingService *service.SettingService,
	coordinator *securityaudit.Coordinator,
	consumerWebManager *service.ConsumerWebSessionManager,
	consumerWebTransport service.ConsumerWebSessionTransport,
) *GatewayHandler {
	h := NewGatewayHandler(gatewayService, openAIGatewayService, geminiCompatService, antigravityGatewayService,
		userService, concurrencyService, requestEligibility, usageService, apiKeyService, usageRecordWorkerPool,
		errorPassthroughService, userMsgQueueService, cfg, settingService)
	h.securityAuditCoordinator = coordinator
	h.consumerWebVideoTasks = service.NewConsumerWebVideoTaskService(consumerWebManager, consumerWebTransport)
	h.consumerWebManager = consumerWebManager
	return h
}

// ProvideOpenAIGatewayHandler assembles the OpenAI-compatible gateway handler.
func ProvideOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	concurrencyService *service.ConcurrencyService,
	requestEligibility service.RequestEligibilityChecker,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	opsService *service.OpsService,
	grokQuotaService *service.GrokQuotaService,
	cfg *config.Config,
	coordinator *securityaudit.Coordinator,
) *OpenAIGatewayHandler {
	h := NewOpenAIGatewayHandler(gatewayService, concurrencyService, requestEligibility, apiKeyService,
		usageRecordWorkerPool, errorPassthroughService, opsService, cfg)
	h.securityAuditCoordinator = coordinator
	h.grokMediaEligibilityProber = grokQuotaService
	return h
}

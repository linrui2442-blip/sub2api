package securityaudit

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
	NewSQLRepository,
	wire.Bind(new(JobRepository), new(*SQLRepository)),
	wire.Bind(new(EventRepository), new(*SQLRepository)),
	NewRedisPayloadStore,
	wire.Bind(new(PayloadStore), new(*RedisPayloadStore)),
	NewOpenAICompatibleScanner,
	wire.Bind(new(PromptScanner), new(*OpenAICompatibleScanner)),
	NewAtomicMetrics,
	wire.Bind(new(Metrics), new(*AtomicMetrics)),
	NewConfigManager,
	wire.Bind(new(ConfigStore), new(*ConfigManager)),
	NewPromptService,
	wire.Bind(new(PromptEngine), new(*PromptService)),
	wire.Bind(new(PromptAdminService), new(*PromptService)),
	NewCoordinator,
	NewPromptAdminHandler,
)

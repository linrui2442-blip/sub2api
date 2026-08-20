//go:build wireinject
// +build wireinject

package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// initializePersonalApplication is intentionally separate from the standard
// SaaS injector. Wire is demand-driven: only providers reachable from the
// Personal handler/server boundary and the explicit Personal lifecycle below
// become part of this executable graph.
func initializePersonalApplication(buildInfo handler.BuildInfo) (*Application, error) {
	wire.Build(
		config.ProviderSet,
		repository.ProviderSet,
		service.ProviderSet,
		securityaudit.ProviderSet,
		middleware.ProviderSet,
		handler.PersonalProviderSet,
		server.PersonalProviderSet,
		providePrivacyClientFactory,
		providePersonalCleanup,
		wire.Struct(new(Application), "Server", "PromptAudit", "Cleanup"),
	)
	return nil, nil
}

// providePersonalCleanup deliberately lists only long-lived services that are
// part of the private gateway/account-pool runtime. Keeping this list small is
// important because a cleanup argument is also a Wire dependency and would
// otherwise pull SaaS background jobs back into Personal startup.
func providePersonalCleanup(
	entClient *ent.Client,
	rdb *redis.Client,
	apiKeyService *service.APIKeyService,
	billingCache *service.BillingCacheService,
	opsService *service.OpsService,
	schedulerSnapshot *service.SchedulerSnapshotService,
	tokenRefresh *service.TokenRefreshService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	oauth *service.OAuthService,
	openaiOAuth *service.OpenAIOAuthService,
	geminiOAuth *service.GeminiOAuthService,
	openAIGateway *service.OpenAIGatewayService,
	auditLog *service.AuditLogService,
	promptAudit *securityaudit.PromptService,
) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		type cleanupStep struct {
			name string
			fn   func() error
		}

		parallelSteps := []cleanupStep{
			{"AuthCacheInvalidationSubscriber", func() error {
				if apiKeyService != nil {
					apiKeyService.StopAuthCacheInvalidationSubscriber()
				}
				return nil
			}},
			{"BillingCacheService", func() error {
				if billingCache != nil {
					billingCache.Stop()
				}
				return nil
			}},
			{"OpsRuntimeSettingsRefresh", func() error {
				if opsService != nil {
					opsService.StopRuntimeSettingsRefresh()
				}
				return nil
			}},
			{"SchedulerSnapshotService", func() error {
				if schedulerSnapshot != nil {
					schedulerSnapshot.Stop()
				}
				return nil
			}},
			{"TokenRefreshService", func() error {
				if tokenRefresh != nil {
					tokenRefresh.Stop()
				}
				return nil
			}},
			{"UsageRecordWorkerPool", func() error {
				if usageRecordWorkerPool != nil {
					usageRecordWorkerPool.Stop()
				}
				return nil
			}},
			{"OAuthService", func() error {
				if oauth != nil {
					oauth.Stop()
				}
				return nil
			}},
			{"OpenAIOAuthService", func() error {
				if openaiOAuth != nil {
					openaiOAuth.Stop()
				}
				return nil
			}},
			{"GeminiOAuthService", func() error {
				if geminiOAuth != nil {
					geminiOAuth.Stop()
				}
				return nil
			}},
			{"OpenAIWSPool", func() error {
				if openAIGateway != nil {
					openAIGateway.CloseOpenAIWSPool()
				}
				return nil
			}},
			{"AuditLogService", func() error {
				if auditLog != nil {
					auditLog.Stop()
				}
				return nil
			}},
			{"PromptAuditService", func() error {
				if promptAudit != nil {
					return promptAudit.Shutdown(ctx)
				}
				return nil
			}},
		}

		var wg sync.WaitGroup
		for i := range parallelSteps {
			step := parallelSteps[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := step.fn(); err != nil {
					log.Printf("[Personal Cleanup] %s failed: %v", step.name, err)
				}
			}()
		}
		wg.Wait()

		if rdb != nil {
			if err := rdb.Close(); err != nil {
				log.Printf("[Personal Cleanup] Redis failed: %v", err)
			}
		}
		if entClient != nil {
			if err := entClient.Close(); err != nil {
				log.Printf("[Personal Cleanup] Ent failed: %v", err)
			}
		}
	}
}

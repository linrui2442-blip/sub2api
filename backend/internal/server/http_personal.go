package server

import (
	"context"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// PersonalProviderSet is the HTTP/server provider boundary used by the
// Personal Edition injector.
var PersonalProviderSet = wire.NewSet(
	ProvidePersonalRouter,
	ProvideHTTPServer,
)

// ProvidePersonalRouter constructs the private router without pulling in the
// standard SaaS-only router dependencies such as subscription/payment config.
func ProvidePersonalRouter(
	cfg *config.Config,
	handlers *handler.Handlers,
	jwtAuth middleware2.JWTAuthMiddleware,
	adminAuth middleware2.AdminAuthMiddleware,
	apiKeyAuth middleware2.APIKeyAuthMiddleware,
	auditLog middleware2.AuditLogMiddleware,
	stepUpAuth middleware2.StepUpAuthMiddleware,
	apiKeyService *service.APIKeyService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	redisClient *redis.Client,
) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware2.Recovery())
	configureTrustedProxies(r, cfg.Server)

	// Web-search emulation is a gateway capability rather than a SaaS billing
	// feature, so keep the existing manager wiring available in Personal mode.
	settingService.SetWebSearchManagerBuilder(context.Background(), func(cfg *service.WebSearchEmulationConfig, proxyURLs map[int64]string) {
		if cfg == nil || !cfg.Enabled || len(cfg.Providers) == 0 {
			service.SetWebSearchManager(nil)
			return
		}
		configs := make([]websearch.ProviderConfig, 0, len(cfg.Providers))
		for _, p := range cfg.Providers {
			if p.APIKey == "" {
				continue
			}
			pc := websearch.ProviderConfig{
				Type:       p.Type,
				APIKey:     p.APIKey,
				QuotaLimit: derefInt64(p.QuotaLimit),
				ExpiresAt:  p.ExpiresAt,
			}
			if p.SubscribedAt != nil {
				pc.SubscribedAt = p.SubscribedAt
			}
			if p.ProxyID != nil {
				pc.ProxyID = *p.ProxyID
				if u, ok := proxyURLs[*p.ProxyID]; ok {
					pc.ProxyURL = u
				} else {
					slog.Warn("websearch: proxy not found for provider, skipping",
						"provider", p.Type, "proxy_id", *p.ProxyID)
					continue
				}
			}
			configs = append(configs, pc)
		}
		service.SetWebSearchManager(websearch.NewManager(configs, redisClient))
	})

	return SetupPersonalRouter(
		r,
		handlers,
		jwtAuth,
		adminAuth,
		apiKeyAuth,
		auditLog,
		stepUpAuth,
		apiKeyService,
		opsService,
		settingService,
		compositeResolver,
		cfg,
		redisClient,
	)
}

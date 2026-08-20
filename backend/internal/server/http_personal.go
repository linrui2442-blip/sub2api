package server

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
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
) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware2.Recovery())
	configureTrustedProxies(r, cfg.Server)

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
	)
}

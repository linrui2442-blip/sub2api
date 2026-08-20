package server

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
)

const frameSrcRefreshTimeout = 5 * time.Second

// SetupPersonalRouter is the Personal Edition HTTP boundary. It intentionally
// avoids the standard user/admin route registrars so commercial handlers can be
// absent from the Wire graph entirely.
func SetupPersonalRouter(
	r *gin.Engine,
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
	cfg *config.Config,
) *gin.Engine {
	middleware2.SetIngressRejectRecorder(opsService)

	var cachedFrameOrigins atomic.Pointer[[]string]
	emptyOrigins := []string{}
	cachedFrameOrigins.Store(&emptyOrigins)

	refreshFrameOrigins := func() {
		ctx, cancel := context.WithTimeout(context.Background(), frameSrcRefreshTimeout)
		defer cancel()
		origins, err := settingService.GetFrameSrcOrigins(ctx)
		if err != nil {
			return
		}
		cachedFrameOrigins.Store(&origins)
	}
	refreshFrameOrigins()

	r.Use(middleware2.RequestLogger())
	r.Use(middleware2.SessionBindingContext(cfg))
	r.Use(middleware2.Logger())
	r.Use(middleware2.CORS(cfg.CORS))
	r.Use(middleware2.SecurityHeaders(cfg.Security.CSP, func() []string {
		if p := cachedFrameOrigins.Load(); p != nil {
			return *p
		}
		return nil
	}))
	r.Use(middleware2.ServerTiming(cfg.Server.EnableServerTiming))

	if web.HasEmbeddedFrontend() {
		frontendServer, err := web.NewFrontendServer(settingService) //nolint:staticcheck
		if err != nil {                                              //nolint:staticcheck
			log.Printf("Warning: Failed to create Personal frontend server with settings injection: %v, using legacy mode", err)
			r.Use(web.ServeEmbeddedFrontend())
			settingService.SetOnUpdateCallback(refreshFrameOrigins)
		} else {
			settingService.SetOnUpdateCallback(func() {
				frontendServer.InvalidateCache()
				refreshFrameOrigins()
			})
			r.Use(frontendServer.Middleware())
		}
	} else {
		settingService.SetOnUpdateCallback(refreshFrameOrigins)
	}

	routes.RegisterCommonRoutes(r)
	v1 := r.Group("/api/v1")
	panelRateLimiter := middleware2.NewPersonalPanelRateLimiter(settingService)

	routes.RegisterPersonalAuthRoutes(v1, handlers, jwtAuth, auditLog, settingService, panelRateLimiter)
	routes.RegisterPersonalUserRoutes(v1, handlers, jwtAuth, auditLog, settingService, panelRateLimiter)
	routes.RegisterPersonalAdminRoutes(v1, handlers, adminAuth, auditLog, stepUpAuth, settingService, panelRateLimiter)

	routes.RegisterGatewayRoutes(r, handlers, apiKeyAuth, apiKeyService, opsService, settingService, compositeResolver, cfg)

	return r
}

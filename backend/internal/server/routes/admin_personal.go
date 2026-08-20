package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterPersonalAdminRoutes registers only the private control-plane routes
// needed to manage members, provider accounts, routing groups, proxies and
// audit history. Commercial SaaS administration is intentionally absent.
func RegisterPersonalAdminRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	adminAuth middleware.AdminAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	stepUpAuth middleware.StepUpAuthMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	admin := v1.Group("/admin")
	admin.Use(gin.HandlerFunc(adminAuth))
	admin.Use(panelRateLimiter.Global())
	admin.Use(gin.HandlerFunc(auditLog))

	// Private member administration. This temporarily reuses the mature
	// upstream user handler surface while public signup and commercial routes
	// remain unavailable.
	registerUserManagementRoutes(admin, h)

	// Routing groups remain core to account selection and per-key model access.
	registerGroupRoutes(admin, h)

	// Provider account pool and OAuth lifecycle.
	registerAccountRoutes(admin, h, stepUpAuth)
	registerOpenAIOAuthRoutes(admin, h)
	registerGeminiOAuthRoutes(admin, h)

	// Local proxy management is retained for provider connectivity.
	registerProxyRoutes(admin, h, stepUpAuth)

	// Local audit history remains available to the owner.
	registerAuditLogRoutes(admin, h, stepUpAuth)
}

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

	// Private member administration deliberately excludes SaaS-only balance,
	// social identity and custom-attribute surfaces.
	registerPersonalMemberRoutes(admin, h)

	// Routing groups remain core to account selection and per-key model access.
	registerGroupRoutes(admin, h)

	// Provider account pool and OAuth lifecycle. OpenAI and Gemini are enabled
	// in V1; the underlying account/provider core remains platform-generic so
	// Anthropic/Claude and future adapters can be registered without a rewrite.
	registerAccountRoutes(admin, h, stepUpAuth)
	registerOpenAIOAuthRoutes(admin, h)
	registerGeminiOAuthRoutes(admin, h)

	// Local proxy management is retained for provider connectivity.
	registerProxyRoutes(admin, h, stepUpAuth)

	// Local audit history remains available to the owner.
	registerAuditLogRoutes(admin, h, stepUpAuth)
}

// registerPersonalMemberRoutes exposes only the owner-managed private-member
// control plane. Public/SaaS identity binding, recharge balance/history and
// custom segmentation attributes are intentionally not part of Personal.
func registerPersonalMemberRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	users := admin.Group("/users")
	{
		users.GET("", h.Admin.User.List)
		users.GET("/:id", h.Admin.User.GetByID)
		users.POST("", h.Admin.User.Create)
		users.PUT("/:id", h.Admin.User.Update)
		users.DELETE("/:id", h.Admin.User.Delete)

		users.GET("/:id/api-keys", h.Admin.User.GetUserAPIKeys)
		users.GET("/:id/usage", h.Admin.User.GetUserUsage)
		users.POST("/:id/replace-group", h.Admin.User.ReplaceGroup)
		users.GET("/:id/rpm-status", h.Admin.User.GetUserRPMStatus)
		users.POST("/batch-concurrency", h.Admin.User.BatchUpdateConcurrency)
		users.POST("/batch-limits", h.Admin.User.BatchUpdateLimits)

	}
}

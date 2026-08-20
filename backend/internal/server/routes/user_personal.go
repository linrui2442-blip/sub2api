package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterPersonalUserRoutes registers the authenticated member surface used by
// Personal Edition. Public SaaS features such as affiliate, redeem,
// subscriptions, announcements, channel availability and channel monitoring
// are intentionally not exposed.
func RegisterPersonalUserRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	authenticated.Use(panelRateLimiter.Global())
	authenticated.Use(gin.HandlerFunc(auditLog))

	user := authenticated.Group("/user")
	{
		user.GET("/profile", h.User.GetProfile)
		user.PUT("/password", h.User.ChangePassword)
		user.PUT("", h.User.UpdateProfile)
		user.GET("/api-keys/:id/usage/daily", panelRateLimiter.Heavy(), h.Usage.GetMyAPIKeyDailyUsage)

		totp := user.Group("/totp")
		{
			totp.GET("/status", h.Totp.GetStatus)
			totp.GET("/verification-method", h.Totp.GetVerificationMethod)
			totp.POST("/send-code", h.Totp.SendVerifyCode)
			totp.POST("/setup", h.Totp.InitiateSetup)
			totp.POST("/enable", h.Totp.Enable)
			totp.POST("/disable", h.Totp.Disable)
			totp.POST("/step-up", h.Totp.StepUp)
		}

		passkeys := user.Group("/passkeys")
		{
			passkeys.GET("", h.Passkey.List)
			passkeys.POST("/register/begin", h.Passkey.BeginRegistration)
			passkeys.POST("/register/finish", h.Passkey.FinishRegistration)
			passkeys.PATCH("/:id", h.Passkey.Rename)
			passkeys.DELETE("/:id", h.Passkey.Delete)
		}
	}

	keys := authenticated.Group("/keys")
	{
		keys.GET("", h.APIKey.List)
		keys.GET("/:id", h.APIKey.GetByID)
		keys.POST("", h.APIKey.Create)
		keys.PUT("/:id", h.APIKey.Update)
		keys.DELETE("/:id", h.APIKey.Delete)
	}

	groups := authenticated.Group("/groups")
	{
		groups.GET("/available", h.APIKey.GetAvailableGroups)
		groups.GET("/rates", h.APIKey.GetUserGroupRates)
	}

	usage := authenticated.Group("/usage")
	usage.Use(panelRateLimiter.Heavy())
	{
		usage.GET("", h.Usage.List)
		usage.GET("/errors", h.Usage.ListErrors)
		usage.GET("/errors/:id", h.Usage.GetErrorDetail)
		usage.GET("/:id", h.Usage.GetByID)
		usage.GET("/stats", h.Usage.Stats)
		usage.GET("/dashboard/stats", h.Usage.DashboardStats)
		usage.GET("/dashboard/trend", h.Usage.DashboardTrend)
		usage.GET("/dashboard/models", h.Usage.DashboardModels)
		usage.GET("/dashboard/snapshot-v2", h.Usage.DashboardSnapshotV2)
		usage.POST("/dashboard/api-keys-usage", h.Usage.DashboardAPIKeysUsage)
	}
}

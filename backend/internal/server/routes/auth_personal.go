package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RegisterPersonalAuthRoutes exposes only the authentication surface needed by
// an owner and manually provisioned private members. Public account creation,
// email verification, password recovery, promo/invitation flows, and social
// login registration are intentionally not registered.
func RegisterPersonalAuthRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth servermiddleware.JWTAuthMiddleware,
	auditLog servermiddleware.AuditLogMiddleware,
	redisClient *redis.Client,
	settingService *service.SettingService,
	panelRateLimiter *servermiddleware.PanelRateLimiter,
) {
	rateLimiter := middleware.NewRateLimiter(redisClient)

	auth := v1.Group("/auth")
	auth.Use(servermiddleware.BackendModeAuthGuard(settingService))
	auth.Use(gin.HandlerFunc(auditLog))
	{
		auth.POST("/login", rateLimiter.LimitWithOptions("auth-login", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.Login)
		auth.POST("/login/2fa", rateLimiter.LimitWithOptions("auth-login-2fa", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.Login2FA)
		auth.POST("/passkey/login/begin", rateLimiter.LimitWithOptions("passkey-login-begin", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Passkey.BeginLogin)
		auth.POST("/passkey/login/finish", rateLimiter.LimitWithOptions("passkey-login-finish", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Passkey.FinishLogin)
		auth.POST("/refresh", rateLimiter.LimitWithOptions("refresh-token", 30, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.RefreshToken)
		auth.POST("/logout", h.Auth.Logout)
	}

	// The frontend needs public runtime settings, but no public mutation routes
	// are exposed in Personal Edition.
	settings := v1.Group("/settings")
	settings.Use(panelRateLimiter.PublicIP())
	settings.GET("/public", h.Setting.GetPublicSettings)

	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(servermiddleware.BackendModeUserGuard(settingService))
	authenticated.Use(panelRateLimiter.Global())
	{
		authenticated.GET("/auth/me", h.Auth.GetCurrentUser)
		authenticated.POST("/auth/revoke-all-sessions", h.Auth.RevokeAllSessions)
	}
}

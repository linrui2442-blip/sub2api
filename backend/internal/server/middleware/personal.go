package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/wire"
)

// ProvidePersonalJWTAuthMiddleware validates panel JWTs with the private auth
// runtime rather than requesting the standard SaaS AuthService graph.
func ProvidePersonalJWTAuthMiddleware(
	personalAuth *service.PersonalAuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) JWTAuthMiddleware {
	return JWTAuthMiddleware(jwtAuth(personalAuth.AuthService, userService, userService, settingService, auditService))
}

// ProvidePersonalAdminAuthMiddleware keeps both admin API-key and JWT behavior
// while sharing the same private auth runtime used by the Personal login UI.
func ProvidePersonalAdminAuthMiddleware(
	personalAuth *service.PersonalAuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) AdminAuthMiddleware {
	return AdminAuthMiddleware(adminAuth(personalAuth.AuthService, userService, settingService, auditService))
}

// PersonalProviderSet excludes OptionalJWT/public-route middleware and, more
// importantly, prevents JWT/admin authentication from selecting the standard
// AuthService provider with its registration/commercial dependencies.
var PersonalProviderSet = wire.NewSet(
	ProvidePersonalJWTAuthMiddleware,
	ProvidePersonalAdminAuthMiddleware,
	NewAPIKeyAuthMiddleware,
	NewAuditLogMiddleware,
	NewStepUpAuthMiddleware,
)

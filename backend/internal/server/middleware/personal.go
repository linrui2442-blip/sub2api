package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
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

// ProvidePersonalAPIKeyAuthMiddleware deliberately omits SubscriptionService.
// Personal Edition forces SIMPLE gateway semantics, and apiKeyAuthWithSubscription
// exits through its authentication-only SIMPLE branch before commercial
// subscription/balance enforcement is reached. Private-member limits are handled
// by the Personal API-key/member policy instead of SaaS subscription plans.
func ProvidePersonalAPIKeyAuthMiddleware(
	apiKeyService *service.APIKeyService,
	cfg *config.Config,
) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, nil, cfg))
}

// PersonalProviderSet excludes OptionalJWT/public-route middleware and, more
// importantly, prevents JWT/admin/API-key authentication from selecting the
// standard SaaS auth/subscription providers.
var PersonalProviderSet = wire.NewSet(
	ProvidePersonalJWTAuthMiddleware,
	ProvidePersonalAdminAuthMiddleware,
	ProvidePersonalAPIKeyAuthMiddleware,
	NewAuditLogMiddleware,
	NewStepUpAuthMiddleware,
)

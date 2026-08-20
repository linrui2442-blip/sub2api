package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/google/wire"
)

// ProvidePersonalAdminHandlers builds the admin handler subset used by the
// private Personal Edition control plane. Commercial SaaS handlers are
// intentionally absent so Wire does not construct their service graphs.
func ProvidePersonalAdminHandlers(
	userHandler *admin.UserHandler,
	groupHandler *admin.GroupHandler,
	accountHandler *admin.AccountHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	geminiOAuthHandler *admin.GeminiOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	userAttributeHandler *admin.UserAttributeHandler,
	auditLogHandler *admin.AuditLogHandler,
) *AdminHandlers {
	return &AdminHandlers{
		User:          userHandler,
		Group:         groupHandler,
		Account:       accountHandler,
		OAuth:         oauthHandler,
		OpenAIOAuth:   openaiOAuthHandler,
		GeminiOAuth:   geminiOAuthHandler,
		Proxy:         proxyHandler,
		UserAttribute: userAttributeHandler,
		AuditLog:      auditLogHandler,
	}
}

// ProvidePersonalHandlers returns a deliberately sparse Handlers aggregate.
// Existing route helpers can keep using *Handlers while the Personal Wire graph
// only constructs fields that are reachable in the private product.
func ProvidePersonalHandlers(
	authHandler *AuthHandler,
	userHandler *UserHandler,
	apiKeyHandler *APIKeyHandler,
	usageHandler *UsageHandler,
	adminHandlers *AdminHandlers,
	gatewayHandler *GatewayHandler,
	openaiGatewayHandler *OpenAIGatewayHandler,
	settingHandler *SettingHandler,
	totpHandler *TotpHandler,
	passkeyHandler *PasskeyHandler,
) *Handlers {
	return &Handlers{
		Auth:          authHandler,
		User:          userHandler,
		APIKey:        apiKeyHandler,
		Usage:         usageHandler,
		Admin:         adminHandlers,
		Gateway:       gatewayHandler,
		OpenAIGateway: openaiGatewayHandler,
		Setting:       settingHandler,
		Totp:          totpHandler,
		Passkey:       passkeyHandler,
	}
}

// PersonalProviderSet is the handler boundary for Personal Edition. Keep this
// list intentionally small; adding a constructor here means its dependency
// graph becomes part of every Personal startup.
var PersonalProviderSet = wire.NewSet(
	NewAuthHandler,
	NewUserHandler,
	NewAPIKeyHandler,
	NewUsageHandler,
	ProvideGatewayHandler,
	ProvideOpenAIGatewayHandler,
	NewTotpHandler,
	NewPasskeyHandler,
	ProvideSettingHandler,

	admin.NewUserHandler,
	admin.NewGroupHandler,
	admin.ProvideAccountHandler,
	admin.NewOAuthHandler,
	admin.NewOpenAIOAuthHandler,
	admin.NewGeminiOAuthHandler,
	admin.NewProxyHandler,
	admin.NewUserAttributeHandler,
	admin.NewAuditLogHandler,

	ProvidePersonalAdminHandlers,
	ProvidePersonalHandlers,
)

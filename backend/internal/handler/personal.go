package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/wire"
)

// ProvidePersonalAuthHandler keeps only dependencies exercised by Personal
// login/session routes. Public registration promo/redeem/user-attribute flows
// are not registered, so they must not become startup dependencies here.
func ProvidePersonalAuthHandler(
	cfg *config.Config,
	personalAuth *service.PersonalAuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	totpService *service.TotpService,
) *AuthHandler {
	return &AuthHandler{
		cfg:         cfg,
		authService: personalAuth.AuthService,
		userService: userService,
		settingSvc:  settingService,
		totpService: totpService,
	}
}

// ProvidePersonalPasskeyHandler shares the same private auth runtime used by
// password login/JWT middleware; it does not request the standard SaaS auth
// provider solely for action-captcha and token-session helpers.
func ProvidePersonalPasskeyHandler(
	passkeys *service.PasskeyService,
	personalAuth *service.PersonalAuthService,
	settingService *service.SettingService,
) *PasskeyHandler {
	return &PasskeyHandler{
		passkeys:    passkeys,
		authService: personalAuth.AuthService,
		settingSvc:  settingService,
	}
}

// ProvidePersonalUserHandler omits the notification-email, affiliate and
// identity-binding dependencies that belong to the public SaaS user surface.
func ProvidePersonalUserHandler(
	userService *service.UserService,
) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// ProvidePersonalSettingHandler serves only /settings/public. The notification
// email unsubscribe service is intentionally not attached in Personal Edition.
func ProvidePersonalSettingHandler(
	settingService *service.SettingService,
	buildInfo BuildInfo,
) *SettingHandler {
	return NewSettingHandler(settingService, buildInfo.Version)
}

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
	auditLogHandler *admin.AuditLogHandler,
) *AdminHandlers {
	return &AdminHandlers{
		User:        userHandler,
		Group:       groupHandler,
		Account:     accountHandler,
		OAuth:       oauthHandler,
		OpenAIOAuth: openaiOAuthHandler,
		GeminiOAuth: geminiOAuthHandler,
		Proxy:       proxyHandler,
		AuditLog:    auditLogHandler,
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
	service.ProvidePersonalRequestEligibilityChecker,
	ProvidePersonalAuthHandler,
	ProvidePersonalUserHandler,
	NewAPIKeyHandler,
	NewUsageHandler,
	ProvideGatewayHandler,
	ProvideOpenAIGatewayHandler,
	NewTotpHandler,
	ProvidePersonalPasskeyHandler,
	ProvidePersonalSettingHandler,

	admin.ProvidePersonalUserHandler,
	admin.NewGroupHandler,
	admin.ProvideAccountHandler,
	admin.NewOAuthHandler,
	admin.NewOpenAIOAuthHandler,
	admin.NewGeminiOAuthHandler,
	admin.NewProxyHandler,
	admin.NewAuditLogHandler,

	ProvidePersonalAdminHandlers,
	ProvidePersonalHandlers,
)

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

	// Provider account pool and OAuth lifecycle. OpenAI, Gemini API and the
	// experimental Antigravity subscription bridge are enabled in Personal V1;
	// the underlying account/provider core remains platform-generic.
	registerAccountRoutes(admin, h, stepUpAuth)
	registerOpenAIOAuthRoutes(admin, h)
	registerGeminiOAuthRoutes(admin, h)
	registerAntigravityOAuthRoutes(admin, h)

	// Local proxy management is retained for provider connectivity.
	registerProxyRoutes(admin, h, stepUpAuth)

	// These are active Gateway network/error-handling controls, not SaaS
	// administration. Their handlers and SQLite Ent schemas are part of the
	// Personal runtime and must remain reachable from the settings UI.
	registerTLSFingerprintProfileRoutes(admin, h)
	registerErrorPassthroughRoutes(admin, h)

	// Local audit history remains available to the owner.
	registerAuditLogRoutes(admin, h, stepUpAuth)
}

func registerTLSFingerprintProfileRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	profiles := admin.Group("/tls-fingerprint-profiles")
	profiles.GET("", h.Admin.TLSFingerprintProfile.List)
	profiles.GET("/:id", h.Admin.TLSFingerprintProfile.GetByID)
	profiles.POST("", h.Admin.TLSFingerprintProfile.Create)
	profiles.PUT("/:id", h.Admin.TLSFingerprintProfile.Update)
	profiles.DELETE("/:id", h.Admin.TLSFingerprintProfile.Delete)
}

func registerErrorPassthroughRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	rules := admin.Group("/error-passthrough-rules")
	rules.GET("", h.Admin.ErrorPassthrough.List)
	rules.GET("/:id", h.Admin.ErrorPassthrough.GetByID)
	rules.POST("", h.Admin.ErrorPassthrough.Create)
	rules.PUT("/:id", h.Admin.ErrorPassthrough.Update)
	rules.DELETE("/:id", h.Admin.ErrorPassthrough.Delete)
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

func registerGroupRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	groups := admin.Group("/groups")
	groups.GET("", h.Admin.Group.List)
	groups.GET("/all", h.Admin.Group.GetAll)
	groups.GET("/capacity-summary", h.Admin.Group.GetCapacitySummary)
	groups.PUT("/sort-order", h.Admin.Group.UpdateSortOrder)
	groups.GET("/:id/models-list-candidates", h.Admin.Group.GetModelsListCandidates)
	groups.GET("/:id/composite-routes", h.Admin.Group.ListCompositeRoutes)
	groups.POST("/:id/composite-routes", h.Admin.Group.CreateCompositeRoute)
	groups.POST("/:id/composite-routes/preview", h.Admin.Group.PreviewCompositeRoute)
	groups.PUT("/:id/composite-routes/:route_id", h.Admin.Group.UpdateCompositeRoute)
	groups.DELETE("/:id/composite-routes/:route_id", h.Admin.Group.DeleteCompositeRoute)
	groups.GET("/:id", h.Admin.Group.GetByID)
	groups.POST("", h.Admin.Group.Create)
	groups.POST("/:id/duplicate", h.Admin.Group.Duplicate)
	groups.PUT("/:id", h.Admin.Group.Update)
	groups.DELETE("/:id", h.Admin.Group.Delete)
	groups.GET("/:id/stats", h.Admin.Group.GetStats)
	groups.GET("/:id/api-keys", h.Admin.Group.GetGroupAPIKeys)
}

func registerAccountRoutes(admin *gin.RouterGroup, h *handler.Handlers, stepUpAuth middleware.StepUpAuthMiddleware) {
	accounts := admin.Group("/accounts")
	accounts.GET("", h.Admin.Account.List)
	accounts.GET("/:id", h.Admin.Account.GetByID)
	accounts.POST("", h.Admin.Account.Create)
	accounts.POST("/:id/duplicate", h.Admin.Account.Duplicate)
	accounts.POST("/check-mixed-channel", h.Admin.Account.CheckMixedChannel)
	accounts.POST("/import/codex-session", h.Admin.Account.ImportCodexSession)
	accounts.PUT("/:id", h.Admin.Account.Update)
	accounts.DELETE("/:id", h.Admin.Account.Delete)
	accounts.POST("/:id/test", h.Admin.Account.Test)
	accounts.POST("/:id/recover-state", h.Admin.Account.RecoverState)
	accounts.POST("/:id/refresh", h.Admin.Account.Refresh)
	accounts.POST("/:id/apply-oauth-credentials", h.Admin.Account.ApplyOAuthCredentials)
	accounts.GET("/:id/stats", h.Admin.Account.GetStats)
	accounts.POST("/:id/clear-error", h.Admin.Account.ClearError)
	accounts.POST("/:id/revert-proxy-fallback", h.Admin.Account.RevertProxyFallback)
	accounts.GET("/:id/usage", h.Admin.Account.GetUsage)
	accounts.GET("/:id/today-stats", h.Admin.Account.GetTodayStats)
	accounts.POST("/usage/batch", h.Admin.Account.GetBatchUsage)
	accounts.POST("/today-stats/batch", h.Admin.Account.GetBatchTodayStats)
	accounts.POST("/:id/clear-rate-limit", h.Admin.Account.ClearRateLimit)
	accounts.POST("/:id/reset-quota", h.Admin.Account.ResetQuota)
	accounts.GET("/:id/temp-unschedulable", h.Admin.Account.GetTempUnschedulable)
	accounts.DELETE("/:id/temp-unschedulable", h.Admin.Account.ClearTempUnschedulable)
	accounts.POST("/:id/schedulable", h.Admin.Account.SetSchedulable)
	accounts.GET("/:id/models", h.Admin.Account.GetAvailableModels)
	accounts.POST("/:id/models/sync-upstream", h.Admin.Account.SyncUpstreamModels)
	accounts.POST("/models/sync-upstream-preview", h.Admin.Account.SyncUpstreamModelsPreview)
	accounts.POST("/batch", h.Admin.Account.BatchCreate)
	accounts.GET("/data", gin.HandlerFunc(stepUpAuth), h.Admin.Account.ExportData)
	accounts.POST("/data", h.Admin.Account.ImportData)
	accounts.POST("/batch-update-credentials", h.Admin.Account.BatchUpdateCredentials)
	accounts.POST("/bulk-update", h.Admin.Account.BulkUpdate)
	accounts.POST("/batch-delete", h.Admin.Account.BatchDelete)
	accounts.POST("/batch-clear-error", h.Admin.Account.BatchClearError)
	accounts.POST("/batch-refresh", h.Admin.Account.BatchRefresh)
	accounts.GET("/antigravity/default-model-mapping", h.Admin.Account.GetAntigravityDefaultModelMapping)
	accounts.POST("/:id/shadow", h.Admin.OpenAIOAuth.CreateShadow)
	accounts.POST("/generate-auth-url", h.Admin.OAuth.GenerateAuthURL)
	accounts.POST("/generate-setup-token-url", h.Admin.OAuth.GenerateSetupTokenURL)
	accounts.POST("/exchange-code", h.Admin.OAuth.ExchangeCode)
	accounts.POST("/exchange-setup-token-code", h.Admin.OAuth.ExchangeSetupTokenCode)
	accounts.POST("/cookie-auth", h.Admin.OAuth.CookieAuth)
	accounts.POST("/setup-token-cookie-auth", h.Admin.OAuth.SetupTokenCookieAuth)
}

func registerOpenAIOAuthRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	openai := admin.Group("/openai")
	openai.POST("/generate-auth-url", h.Admin.OpenAIOAuth.GenerateAuthURL)
	openai.POST("/exchange-code", h.Admin.OpenAIOAuth.ExchangeCode)
	openai.POST("/refresh-token", h.Admin.OpenAIOAuth.RefreshToken)
	openai.POST("/accounts/:id/refresh", h.Admin.OpenAIOAuth.RefreshAccountToken)
	openai.POST("/create-from-oauth", h.Admin.OpenAIOAuth.CreateAccountFromOAuth)
	openai.POST("/create-from-codex-pat", h.Admin.OpenAIOAuth.CreateAccountFromCodexPAT)
	openai.GET("/accounts/:id/quota", h.Admin.OpenAIOAuth.QueryQuota)
	openai.POST("/accounts/:id/quota/refresh", h.Admin.OpenAIOAuth.RefreshQuota)
	openai.POST("/accounts/:id/reset-quota", h.Admin.OpenAIOAuth.ResetQuota)
}

func registerGeminiOAuthRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	gemini := admin.Group("/gemini")
	gemini.POST("/oauth/auth-url", h.Admin.GeminiOAuth.GenerateAuthURL)
	gemini.POST("/oauth/exchange-code", h.Admin.GeminiOAuth.ExchangeCode)
	gemini.GET("/oauth/capabilities", h.Admin.GeminiOAuth.GetCapabilities)
}

func registerAntigravityOAuthRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	antigravity := admin.Group("/antigravity")
	antigravity.POST("/oauth/auth-url", h.Admin.AntigravityOAuth.GenerateAuthURL)
	antigravity.POST("/oauth/exchange-code", h.Admin.AntigravityOAuth.ExchangeCode)
	antigravity.POST("/oauth/refresh-token", h.Admin.AntigravityOAuth.RefreshToken)
}

func registerProxyRoutes(admin *gin.RouterGroup, h *handler.Handlers, stepUpAuth middleware.StepUpAuthMiddleware) {
	proxies := admin.Group("/proxies")
	proxies.GET("", h.Admin.Proxy.List)
	proxies.GET("/all", h.Admin.Proxy.GetAll)
	proxies.GET("/data", gin.HandlerFunc(stepUpAuth), h.Admin.Proxy.ExportData)
	proxies.POST("/data", h.Admin.Proxy.ImportData)
	proxies.GET("/:id", h.Admin.Proxy.GetByID)
	proxies.POST("", h.Admin.Proxy.Create)
	proxies.PUT("/:id", h.Admin.Proxy.Update)
	proxies.DELETE("/:id", h.Admin.Proxy.Delete)
	proxies.POST("/:id/test", h.Admin.Proxy.Test)
	proxies.POST("/:id/quality-check", h.Admin.Proxy.CheckQuality)
	proxies.GET("/:id/stats", h.Admin.Proxy.GetStats)
	proxies.GET("/:id/accounts", h.Admin.Proxy.GetProxyAccounts)
	proxies.POST("/batch-delete", h.Admin.Proxy.BatchDelete)
	proxies.POST("/batch", h.Admin.Proxy.BatchCreate)
}

func registerAuditLogRoutes(admin *gin.RouterGroup, h *handler.Handlers, _ middleware.StepUpAuthMiddleware) {
	auditLogs := admin.Group("/audit-logs")
	auditLogs.GET("", h.Admin.AuditLog.List)
	auditLogs.GET("/:id", h.Admin.AuditLog.Get)
	auditLogs.POST("/clear", h.Admin.AuditLog.Clear)
}

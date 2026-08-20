package admin

import "github.com/Wei-Shaw/sub2api/internal/service"

// ProvidePersonalUserHandler builds the private-member admin handler without
// the commercial BillingCache dependency. Retained quota handlers already
// tolerate a nil cache and persist their source of truth through the quota
// repository, which is appropriate for the single-process Personal runtime.
func ProvidePersonalUserHandler(
	adminService service.AdminService,
	concurrencyService *service.ConcurrencyService,
	userPlatformQuotaRepo service.UserPlatformQuotaRepository,
	totpService *service.TotpService,
	userService *service.UserService,
	settingService *service.SettingService,
) *UserHandler {
	return &UserHandler{
		adminService:          adminService,
		concurrencyService:    concurrencyService,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
		totpService:           totpService,
		userService:           userService,
		settingService:        settingService,
	}
}

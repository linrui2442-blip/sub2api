package admin

import "github.com/Wei-Shaw/sub2api/internal/service"

// ProvidePersonalUserHandler builds the private-member admin handler without
// commercial billing or subscription-quota dependencies. Operational member
// concurrency and RPM controls remain available through AdminService.
func ProvidePersonalUserHandler(
	adminService service.AdminService,
	concurrencyService *service.ConcurrencyService,
	totpService *service.TotpService,
	userService *service.UserService,
	settingService *service.SettingService,
) *UserHandler {
	return &UserHandler{
		adminService:       adminService,
		concurrencyService: concurrencyService,
		totpService:        totpService,
		userService:        userService,
		settingService:     settingService,
	}
}

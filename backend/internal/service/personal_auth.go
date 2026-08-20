package service

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

// PersonalAuthService is a distinct Wire type for the private control-plane
// authentication runtime. Embedding keeps the mature token/session logic while
// preventing the standard SaaS AuthService provider from being selected just
// because Personal needs login/JWT validation.
type PersonalAuthService struct {
	*AuthService
}

// ProvidePersonalAuthService deliberately leaves public-account provisioning
// dependencies nil: redeem/invitation, registration email delivery, promo,
// default subscription assignment, affiliate and signup platform quotas.
// Personal routes never expose those flows. Captcha services remain available
// so an existing local configuration cannot turn login/passkey checks into a
// nil dependency after an upgrade.
func ProvidePersonalAuthService(
	entClient *dbent.Client,
	userRepo UserRepository,
	refreshTokenCache RefreshTokenCache,
	cfg *config.Config,
	settingService *SettingService,
	turnstileService *TurnstileService,
	tencentCaptchaService *TencentCaptchaService,
	aliyunCaptchaService *AliyunCaptchaService,
) *PersonalAuthService {
	svc := NewAuthService(
		entClient,
		userRepo,
		nil, // redeem / invitation repository
		refreshTokenCache,
		cfg,
		settingService,
		nil, // registration email service
		turnstileService,
		nil, // registration email queue
		nil, // promo service
		nil, // default subscription assigner
		nil, // affiliate service
		nil, // signup platform quota repository
	)
	svc.SetTencentCaptchaService(tencentCaptchaService)
	svc.SetAliyunCaptchaService(aliyunCaptchaService)
	return &PersonalAuthService{AuthService: svc}
}

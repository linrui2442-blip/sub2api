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
// default subscription assignment, affiliate flows, and public captcha.
func ProvidePersonalAuthService(
	entClient *dbent.Client,
	userRepo UserRepository,
	refreshTokenCache RefreshTokenCache,
	cfg *config.Config,
	settingService *SettingService,
) *PersonalAuthService {
	svc := NewAuthService(
		entClient,
		userRepo,
		refreshTokenCache,
		cfg,
		settingService,
	)
	return &PersonalAuthService{AuthService: svc}
}

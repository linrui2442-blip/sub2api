package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// UserHandler handles user-related requests
type UserHandler struct {
	userService *service.UserService
}

// ChangePasswordRequest represents the change password request payload
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// UpdateProfileRequest represents the update profile request payload
type UpdateProfileRequest struct {
	Username               *string  `json:"username"`
	AvatarURL              *string  `json:"avatar_url"`
	BalanceNotifyEnabled   *bool    `json:"balance_notify_enabled"`
	BalanceNotifyThreshold *float64 `json:"balance_notify_threshold"`
}

type userProfileResponse struct {
	dto.User
	AvatarURL         string                                 `json:"avatar_url,omitempty"`
	AvatarSource      *userProfileSourceContext              `json:"avatar_source,omitempty"`
	UsernameSource    *userProfileSourceContext              `json:"username_source,omitempty"`
	DisplayNameSource *userProfileSourceContext              `json:"display_name_source,omitempty"`
	NicknameSource    *userProfileSourceContext              `json:"nickname_source,omitempty"`
	ProfileSources    map[string]*userProfileSourceContext   `json:"profile_sources,omitempty"`
	Identities        service.UserIdentitySummarySet         `json:"identities"`
	AuthBindings      map[string]service.UserIdentitySummary `json:"auth_bindings"`
	IdentityBindings  map[string]service.UserIdentitySummary `json:"identity_bindings"`
	EmailBound        bool                                   `json:"email_bound"`
	LinuxDoBound      bool                                   `json:"linuxdo_bound"`
	OIDCBound         bool                                   `json:"oidc_bound"`
	WeChatBound       bool                                   `json:"wechat_bound"`
	DingTalkBound     bool                                   `json:"dingtalk_bound"`
}

type userProfileSourceContext struct {
	Provider string `json:"provider,omitempty"`
	Source   string `json:"source,omitempty"`
}

// GetProfile handles getting user profile
// GET /api/v1/users/me
func (h *UserHandler) GetProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	userData, err := h.userService.GetProfile(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	profileResp, err := h.buildUserProfileResponse(c.Request.Context(), subject.UserID, userData)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, profileResp)
}

// ChangePassword handles changing user password
// POST /api/v1/users/me/password
func (h *UserHandler) ChangePassword(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.ChangePasswordRequest{
		CurrentPassword: req.OldPassword,
		NewPassword:     req.NewPassword,
	}
	err := h.userService.ChangePassword(c.Request.Context(), subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Password changed successfully"})
}

// UpdateProfile handles updating user profile
// PUT /api/v1/users/me
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.UpdateProfileRequest{
		Username:               req.Username,
		AvatarURL:              req.AvatarURL,
		BalanceNotifyEnabled:   req.BalanceNotifyEnabled,
		BalanceNotifyThreshold: req.BalanceNotifyThreshold,
	}
	updatedUser, err := h.userService.UpdateProfile(c.Request.Context(), subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	profileResp, err := h.buildUserProfileResponse(c.Request.Context(), subject.UserID, updatedUser)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, profileResp)
}

type StartIdentityBindingRequest struct {
	Provider   string `json:"provider" binding:"required"`
	RedirectTo string `json:"redirect_to"`
}

type BindEmailIdentityRequest struct {
	Email      string `json:"email" binding:"required,email"`
	VerifyCode string `json:"verify_code" binding:"required"`
	Password   string `json:"password" binding:"required"`
}

type SendEmailBindingCodeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// SendNotifyEmailCodeRequest represents the request to send notify email verification code
type SendNotifyEmailCodeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// VerifyNotifyEmailRequest represents the request to verify and add notify email
type VerifyNotifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required,len=6"`
}

// RemoveNotifyEmailRequest represents the request to remove a notify email
type RemoveNotifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ToggleNotifyEmailRequest represents the request to toggle a notify email's disabled state
type ToggleNotifyEmailRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Disabled bool   `json:"disabled"`
}

func (h *UserHandler) buildUserProfileResponse(ctx context.Context, userID int64, user *service.User) (userProfileResponse, error) {
	identities, err := h.userService.GetProfileIdentitySummaries(ctx, userID, user)
	if err != nil {
		return userProfileResponse{}, err
	}
	return userProfileResponseFromService(user, identities), nil
}

func userProfileResponseFromService(user *service.User, identities service.UserIdentitySummarySet) userProfileResponse {
	base := dto.UserFromService(user)
	if base == nil {
		return userProfileResponse{}
	}
	bindings := userProfileBindingMap(identities)
	profileSources, avatarSource, usernameSource := inferUserProfileSources(user, identities)
	return userProfileResponse{
		User:              *base,
		AvatarURL:         user.AvatarURL,
		AvatarSource:      avatarSource,
		UsernameSource:    usernameSource,
		DisplayNameSource: usernameSource,
		NicknameSource:    usernameSource,
		ProfileSources:    profileSources,
		Identities:        identities,
		AuthBindings:      bindings,
		IdentityBindings:  bindings,
		EmailBound:        identities.Email.Bound,
		LinuxDoBound:      identities.LinuxDo.Bound,
		OIDCBound:         identities.OIDC.Bound,
		WeChatBound:       identities.WeChat.Bound,
		DingTalkBound:     identities.DingTalk.Bound,
	}
}

func userProfileBindingMap(identities service.UserIdentitySummarySet) map[string]service.UserIdentitySummary {
	return map[string]service.UserIdentitySummary{
		"email":    identities.Email,
		"linuxdo":  identities.LinuxDo,
		"oidc":     identities.OIDC,
		"wechat":   identities.WeChat,
		"dingtalk": identities.DingTalk,
	}
}

func inferUserProfileSources(user *service.User, identities service.UserIdentitySummarySet) (
	map[string]*userProfileSourceContext,
	*userProfileSourceContext,
	*userProfileSourceContext,
) {
	if user == nil {
		return nil, nil, nil
	}

	thirdParty := thirdPartyIdentityProviders(identities)
	var avatarSource *userProfileSourceContext
	avatarValue := strings.TrimSpace(user.AvatarURL)
	for _, summary := range thirdParty {
		if avatarValue != "" && avatarValue == strings.TrimSpace(summary.AvatarURL) {
			avatarSource = buildUserProfileSourceContext(summary.Provider)
			break
		}
	}

	usernameValue := strings.TrimSpace(user.Username)
	var usernameSource *userProfileSourceContext
	for _, summary := range thirdParty {
		if usernameValue != "" && usernameValue == strings.TrimSpace(summary.DisplayName) {
			usernameSource = buildUserProfileSourceContext(summary.Provider)
			break
		}
	}

	profileSources := map[string]*userProfileSourceContext{}
	if avatarSource != nil {
		profileSources["avatar"] = avatarSource
	}
	if usernameSource != nil {
		profileSources["username"] = usernameSource
		profileSources["display_name"] = usernameSource
		profileSources["nickname"] = usernameSource
	}
	if len(profileSources) == 0 {
		return nil, avatarSource, usernameSource
	}
	return profileSources, avatarSource, usernameSource
}

func thirdPartyIdentityProviders(identities service.UserIdentitySummarySet) []service.UserIdentitySummary {
	out := make([]service.UserIdentitySummary, 0, 3)
	for _, summary := range []service.UserIdentitySummary{identities.LinuxDo, identities.OIDC, identities.WeChat, identities.DingTalk} {
		if summary.Bound {
			out = append(out, summary)
		}
	}
	return out
}

func buildUserProfileSourceContext(provider string) *userProfileSourceContext {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	return &userProfileSourceContext{
		Provider: provider,
		Source:   provider,
	}
}

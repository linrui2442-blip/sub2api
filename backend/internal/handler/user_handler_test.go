//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserHandlerGetProfileReturnsIdentitySummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userHandlerRepoStub{
		user:       &service.User{ID: 7, Email: "member@example.com", Username: "member", Role: service.RoleUser, Status: service.StatusActive},
		identities: []service.UserAuthIdentityRecord{{ProviderType: "linuxdo", ProviderKey: "linuxdo", ProviderSubject: "member-7"}},
	}
	handler := &UserHandler{userService: service.NewUserService(repo, nil, nil)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})

	handler.GetProfile(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Code int `json:"code"`
		Data struct {
			Email      string `json:"email"`
			EmailBound bool   `json:"email_bound"`
			Identities struct {
				LinuxDo struct {
					Bound       bool   `json:"bound"`
					ProviderKey string `json:"provider_key"`
				} `json:"linuxdo"`
			} `json:"identities"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 0, response.Code)
	require.Equal(t, "member@example.com", response.Data.Email)
	require.True(t, response.Data.EmailBound)
	require.True(t, response.Data.Identities.LinuxDo.Bound)
	require.Equal(t, "linuxdo", response.Data.Identities.LinuxDo.ProviderKey)
}

func TestUserHandlerGetProfileInfersProfileSourceOnlyWhenIdentityMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userHandlerRepoStub{
		user: &service.User{
			ID: 9, Email: "member@example.com", Username: "provider-name", AvatarURL: "https://example.test/avatar.png",
			Role: service.RoleUser, Status: service.StatusActive,
		},
		identities: []service.UserAuthIdentityRecord{{
			ProviderType: "oidc", ProviderKey: "https://issuer.example.test", ProviderSubject: "member-9",
			Metadata: map[string]any{"suggested_display_name": "provider-name", "avatar_url": "https://example.test/avatar.png"},
		}},
	}
	handler := &UserHandler{userService: service.NewUserService(repo, nil, nil)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 9})

	handler.GetProfile(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Code int `json:"code"`
		Data struct {
			UsernameSource *struct {
				Provider string `json:"provider"`
			} `json:"username_source"`
			AvatarSource *struct {
				Provider string `json:"provider"`
			} `json:"avatar_source"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 0, response.Code)
	require.NotNil(t, response.Data.UsernameSource)
	require.Equal(t, "oidc", response.Data.UsernameSource.Provider)
	require.NotNil(t, response.Data.AvatarSource)
	require.Equal(t, "oidc", response.Data.AvatarSource.Provider)
}

func TestUserHandlerGetProfileRejectsUnauthenticatedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &UserHandler{userService: service.NewUserService(&userHandlerRepoStub{}, nil, nil)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)

	handler.GetProfile(c)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestUserHandlerUpdateProfilePersistsRequestedUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userHandlerRepoStub{user: &service.User{ID: 8, Email: "member@example.com", Username: "before", Role: service.RoleUser, Status: service.StatusActive}}
	handler := &UserHandler{userService: service.NewUserService(repo, nil, nil)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/users/me", strings.NewReader(`{"username":"after"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 8})

	handler.UpdateProfile(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "after", repo.user.Username)
}

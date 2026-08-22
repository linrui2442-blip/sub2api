package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPersonalAntigravityOAuthRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	registerAntigravityOAuthRoutes(admin, &handler.Handlers{
		Admin: &handler.AdminHandlers{
			AntigravityOAuth: &adminhandler.AntigravityOAuthHandler{},
		},
	})

	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	require.True(t, routes["POST /api/v1/admin/antigravity/oauth/auth-url"])
	require.True(t, routes["POST /api/v1/admin/antigravity/oauth/exchange-code"])
	require.True(t, routes["POST /api/v1/admin/antigravity/oauth/refresh-token"])
}

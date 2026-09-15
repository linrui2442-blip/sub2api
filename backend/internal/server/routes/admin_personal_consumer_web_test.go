package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPersonalConsumerWebCredentialAdminRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	registerAccountRoutes(admin, &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &adminhandler.AccountHandler{}, OAuth: &adminhandler.OAuthHandler{}, OpenAIOAuth: &adminhandler.OpenAIOAuthHandler{},
	}}, func(c *gin.Context) { c.Next() })
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["PUT /api/v1/admin/accounts/:id/consumer-web-credentials"])
}

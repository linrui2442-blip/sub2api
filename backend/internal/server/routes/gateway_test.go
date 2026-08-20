package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter(platform ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	groupPlatform := service.PlatformOpenAI
	if len(platform) > 0 && platform[0] != "" {
		groupPlatform = platform[0]
	}
	RegisterGatewayRoutes(router, &handler.Handlers{
		Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{},
	}, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{Platform: groupPlatform}})
		c.Next()
	}), nil, nil, nil, nil, &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024, TextMaxBodySize: 1024 * 1024}})
	return router
}

func registeredGatewayRoutes(router *gin.Engine) map[string]bool {
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	return routes
}

func TestPersonalGatewayCoreSurfaceIsRegistered(t *testing.T) {
	routes := registeredGatewayRoutes(newGatewayRoutesTestRouter())
	for _, route := range []string{
		"POST /v1/messages", "POST /v1/messages/count_tokens", "POST /v1/chat/completions",
		"POST /v1/responses", "GET /v1/responses", "POST /v1/responses/*subpath",
		"POST /v1/embeddings", "GET /v1/models", "GET /v1/usage",
	} {
		require.True(t, routes[route], "%s must remain registered", route)
	}
}

func TestPersonalGatewayNonCoreMediaAndSearchSurfaceIsAbsent(t *testing.T) {
	routes := registeredGatewayRoutes(newGatewayRoutesTestRouter(service.PlatformGrok))
	for _, route := range []string{
		"POST /v1/images/generations", "POST /v1/images/edits", "POST /v1/images/generations/async",
		"GET /v1/images/tasks/:task_id", "POST /v1/videos", "POST /v1/videos/generations",
		"GET /v1/videos/:request_id", "POST /v1/audio/speech", "POST /v1/audio/transcriptions",
		"POST /v1/tts", "POST /v1/stt", "POST /v1/custom-voices", "POST /v1/search",
		"POST /v1/x_search", "POST /v1/alpha/search", "POST /v1/live",
	} {
		require.False(t, routes[route], "%s must not be exposed by Personal Edition", route)
	}
}

func TestPersonalGatewayKeepsProviderNeutralAliases(t *testing.T) {
	routes := registeredGatewayRoutes(newGatewayRoutesTestRouter(service.PlatformComposite))
	for _, route := range []string{
		"POST /chat/completions", "POST /responses", "POST /embeddings",
		"POST /backend-api/codex/responses",
	} {
		require.True(t, routes[route], "%s must remain registered", route)
	}
	require.True(t, routes[http.MethodGet+" /backend-api/codex/models"])
}

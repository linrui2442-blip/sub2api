package routes

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type personalProviderResolverFunc func(context.Context, string, string) (string, string, error)

func (f personalProviderResolverFunc) ResolvePersonalProvider(ctx context.Context, model, endpointPlatform string) (string, string, error) {
	return f(ctx, model, endpointPlatform)
}

func explicitPersonalProviderResolver(_ context.Context, model, endpointPlatform string) (string, string, error) {
	return service.ResolvePersonalProvider(model, endpointPlatform)
}

func TestPersonalUnifiedRouterStripsNamespaceAndKeepsPinnedKeysUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, body, wantBody, wantPlatform string
		groupID                            *int64
		wantStatus                         int
	}{
		{name: "unified antigravity", body: `{"model":"antigravity/gemini-3.1-pro-high"}`, wantBody: `{"model":"gemini-3.1-pro-high"}`, wantPlatform: service.PlatformAntigravity, wantStatus: http.StatusOK},
		{name: "unified openai", body: `{"model":"openai/gpt-5.4"}`, wantBody: `{"model":"gpt-5.4"}`, wantPlatform: service.PlatformOpenAI, wantStatus: http.StatusOK},
		{name: "ambiguous gemini", body: `{"model":"gemini-2.5-flash"}`, wantStatus: http.StatusBadRequest},
		{name: "pinned compatibility", body: `{"model":"antigravity/gemini-3.1-pro-high"}`, wantBody: `{"model":"antigravity/gemini-3.1-pro-high"}`, groupID: ptrInt64(7), wantStatus: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := personalProviderResolverFunc(explicitPersonalProviderResolver)
			if tt.name == "ambiguous gemini" {
				resolver = func(context.Context, string, string) (string, string, error) {
					return "", "", service.ErrPersonalRouteAmbiguous
				}
			}
			router := gin.New()
			router.POST("/v1/messages", func(c *gin.Context) {
				c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: tt.groupID})
				c.Next()
			}, personalUnifiedRouterMiddleware("", resolver, openAICompatibleRoutingErrorWriter), func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				platform, _ := servermiddleware.GetForcePlatformFromContext(c)
				require.Equal(t, tt.wantPlatform, platform)
				require.JSONEq(t, tt.wantBody, string(body))
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestPersonalUnifiedKeyRoutesAcrossProvidersWhilePinnedKeyCannotOverrideGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unifiedKey := &service.APIKey{}
	for _, tt := range []struct {
		model, wantPlatform string
	}{
		{"openai/gpt-5.4", service.PlatformOpenAI},
		{"antigravity/gemini-3.1-pro-high", service.PlatformAntigravity},
	} {
		router := gin.New()
		router.POST("/v1/messages", func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyAPIKey), unifiedKey)
			c.Next()
		}, personalUnifiedRouterMiddleware("", personalProviderResolverFunc(explicitPersonalProviderResolver), openAICompatibleRoutingErrorWriter), func(c *gin.Context) {
			platform, ok := servermiddleware.GetForcePlatformFromContext(c)
			require.True(t, ok)
			require.Equal(t, tt.wantPlatform, platform)
			c.Status(http.StatusOK)
		})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"`+tt.model+`"}`)))
		require.Equal(t, http.StatusOK, rec.Code)
	}

	groupID := int64(9)
	pinnedKey := &service.APIKey{GroupID: &groupID, Group: &service.Group{Platform: service.PlatformGemini}}
	router := gin.New()
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), pinnedKey)
		c.Next()
	}, personalUnifiedRouterMiddleware("", personalProviderResolverFunc(explicitPersonalProviderResolver), openAICompatibleRoutingErrorWriter), func(c *gin.Context) {
		_, forced := servermiddleware.GetForcePlatformFromContext(c)
		require.False(t, forced, "a client namespace must not override a pinned key's group")
		require.Equal(t, service.PlatformGemini, getGroupPlatform(c))
		c.Status(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"openai/gpt-5.4"}`)))
	require.Equal(t, http.StatusOK, rec.Code)
}

func ptrInt64(v int64) *int64 { return &v }

func TestPersonalUnifiedGeminiRouteUsesEndpointHintAndStripsExplicitNamespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		path, wantModel, wantPlatform string
	}{
		{"/v1beta/models/gemini-2.5-flash:generateContent", "gemini-2.5-flash", service.PlatformGemini},
		{"/v1beta/models/antigravity/gemini-3.1-pro-high:generateContent", "gemini-3.1-pro-high", service.PlatformAntigravity},
	} {
		router := gin.New()
		router.POST("/v1beta/models/*modelAction", func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{})
			c.Next()
		}, personalUnifiedRouterMiddleware(service.PlatformGemini, personalProviderResolverFunc(explicitPersonalProviderResolver), googleRoutingErrorWriter), func(c *gin.Context) {
			require.Equal(t, tt.wantModel, compositeGeminiModelFromParams(c))
			platform, ok := servermiddleware.GetForcePlatformFromContext(c)
			require.True(t, ok)
			require.Equal(t, tt.wantPlatform, platform)
			c.Status(http.StatusOK)
		})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(`{"contents":[]}`)))
		require.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestPersonalUnifiedRouterUsesConfiguredCandidatesAndRoutingErrorsAreNotPermissionErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name           string
		resolve        personalProviderResolverFunc
		wantStatus     int
		wantDownstream int
		wantType       string
	}{
		{
			name: "unique candidate",
			resolve: func(_ context.Context, model, _ string) (string, string, error) {
				return service.PlatformAntigravity, model, nil
			},
			wantStatus: http.StatusOK, wantDownstream: 1,
		},
		{
			name: "unsupported",
			resolve: func(context.Context, string, string) (string, string, error) {
				return "", "", service.ErrPersonalRouteUnsupported
			},
			wantStatus: http.StatusBadRequest, wantType: "invalid_request_error",
		},
		{
			name: "real ambiguity",
			resolve: func(context.Context, string, string) (string, string, error) {
				return "", "", service.ErrPersonalRouteAmbiguous
			},
			wantStatus: http.StatusBadRequest, wantType: "invalid_request_error",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			downstream := 0
			router := gin.New()
			router.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{})
				c.Next()
			}, personalUnifiedRouterMiddleware("", tt.resolve, openAICompatibleRoutingErrorWriter), func(c *gin.Context) {
				downstream++
				c.Status(http.StatusOK)
			})

			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"model-alpha"}`))
			first := httptest.NewRecorder()
			router.ServeHTTP(first, request)
			require.Equal(t, tt.wantStatus, first.Code)
			require.Equal(t, tt.wantDownstream, downstream)
			if tt.wantType != "" {
				require.Contains(t, first.Body.String(), `"type":"`+tt.wantType+`"`)
				require.NotContains(t, first.Body.String(), "permission_error")
			}

			// A local routing rejection must not poison the router/listener.
			second := httptest.NewRecorder()
			router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"model-alpha"}`)))
			require.Equal(t, tt.wantStatus, second.Code)
		})
	}
}

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

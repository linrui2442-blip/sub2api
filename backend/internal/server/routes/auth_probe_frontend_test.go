//go:build embed

package routes

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type probeFrontendSettings struct{}

func (probeFrontendSettings) GetPublicSettingsForInjection(context.Context) (any, error) {
	return map[string]any{}, nil
}

func TestPersonalAuthProbeWithEmbeddedFrontend(t *testing.T) {
	t.Setenv(personal.EnvEnabled, "1")
	t.Setenv(personal.EnvAcceptanceAuthProbe, "1")
	personal.ConfigureAcceptanceAuthProbe("personal")
	t.Cleanup(func() { personal.ConfigureAcceptanceAuthProbe("source") })
	for _, legacy := range []bool{false, true} {
		for _, tc := range []struct {
			name, method, path, key, remote string
			status                          int
			html                            bool
		}{
			{"backend reached", "POST", "/_acceptance/consumer-web/auth-probe", "synthetic-valid", "127.0.0.1:1234", 503, false},
			{"missing key", "POST", "/_acceptance/consumer-web/auth-probe", "", "127.0.0.1:1234", 401, false},
			{"invalid key", "POST", "/_acceptance/consumer-web/auth-probe", "invalid", "127.0.0.1:1234", 401, false},
			{"non loopback", "POST", "/_acceptance/consumer-web/auth-probe", "synthetic-valid", "203.0.113.2:1234", 403, false},
			{"get not registered", "GET", "/_acceptance/consumer-web/auth-probe", "synthetic-valid", "127.0.0.1:1234", 404, false},
			{"nearby path", "POST", "/_acceptance/consumer-web/not-a-real-probe", "", "127.0.0.1:1234", 200, true},
			{"normal spa", "GET", "/admin/accounts", "", "127.0.0.1:1234", 200, true},
		} {
			t.Run(tc.name+map[bool]string{true: "/legacy", false: "/settings"}[legacy], func(t *testing.T) {
				r := gin.New()
				if legacy {
					r.Use(web.ServeEmbeddedFrontend())
				} else {
					f, err := web.NewFrontendServer(probeFrontendSettings{})
					require.NoError(t, err)
					r.Use(f.Middleware())
				}
				// Synthetic API-key validator: no credential repository or provider access.
				auth := middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
					if c.GetHeader("Authorization") != "Bearer synthetic-valid" {
						c.AbortWithStatusJSON(401, gin.H{"error": "invalid_api_key"})
						return
					}
					c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{})
					c.Next()
				})
				RegisterGatewayRoutes(r, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}}, auth, nil, nil, nil, nil, &config.Config{})
				req := httptest.NewRequest(tc.method, tc.path, nil)
				req.RemoteAddr = tc.remote
				if tc.key != "" {
					req.Header.Set("Authorization", "Bearer "+tc.key)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				require.Equal(t, tc.status, w.Code)
				if tc.html {
					require.Contains(t, w.Header().Get("Content-Type"), "text/html")
				} else {
					require.NotContains(t, w.Header().Get("Content-Type"), "text/html")
				}
				if tc.status == 401 {
					require.Contains(t, w.Header().Get("Content-Type"), "application/json")
				}
			})
		}
	}
}

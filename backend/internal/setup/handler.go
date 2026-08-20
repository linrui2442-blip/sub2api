package setup

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// setupGuard only permits the local owner bootstrap while no durable owner
// marker exists. Public database/Redis test and installation routes are absent.
func setupGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !NeedsSetup() {
			response.Error(c, http.StatusForbidden, "Setup is not allowed: system is already installed")
			c.Abort()
			return
		}
		c.Next()
	}
}

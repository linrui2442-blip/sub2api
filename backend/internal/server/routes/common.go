package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/gin-gonic/gin"
)

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Claude Code 遥测日志（忽略，直接返回200）
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode).
	// Personal Edition adds an explicit marker so a later manual /setup visit is
	// redirected by the frontend instead of ever falling back to the upstream
	// PostgreSQL/Redis setup wizard.
	r.GET("/setup/status", func(c *gin.Context) {
		data := gin.H{
			"needs_setup": false,
			"step":        "completed",
		}
		if personal.Enabled() {
			data["personal"] = true
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": data,
		})
	})
}

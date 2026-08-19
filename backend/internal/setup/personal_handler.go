package setup

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// PersonalSetupStatus is intentionally explicit so the embedded frontend can
// switch from the upstream PostgreSQL/Redis wizard to the one-step owner setup.
type PersonalSetupStatus struct {
	NeedsSetup bool   `json:"needs_setup"`
	Step       string `json:"step"`
	Personal   bool   `json:"personal"`
}

type PersonalInstallRequest struct {
	Admin AdminConfig `json:"admin" binding:"required"`
}

// RegisterPersonalRoutes registers only the first-run surface required by the
// single-node Personal Edition. Database and Redis test/install endpoints are
// intentionally absent. onInstalled is notified after the successful response
// has been flushed so the bootstrap HTTP server can hand the same port to the
// normal application without asking the user to restart the EXE.
func RegisterPersonalRoutes(r *gin.Engine, onInstalled func()) {
	setup := r.Group("/setup")
	setup.GET("/status", func(c *gin.Context) {
		response.Success(c, PersonalSetupStatus{
			NeedsSetup: NeedsSetup(),
			Step:       "owner",
			Personal:   true,
		})
	})

	protected := setup.Group("")
	protected.Use(setupGuard())
	protected.POST("/personal/install", func(c *gin.Context) {
		installPersonalHandler(c, onInstalled)
	})
}

func installPersonalHandler(c *gin.Context, onInstalled func()) {
	installMutex.Lock()
	defer installMutex.Unlock()

	if !NeedsSetup() {
		response.Error(c, http.StatusForbidden, "Setup is not allowed: system is already installed")
		return
	}

	var req PersonalInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	if err := InstallPersonal(req.Admin.Email, req.Admin.Password); err != nil {
		response.Error(c, http.StatusBadRequest, "Personal installation failed: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"message": "Personal Edition initialized successfully. Starting the local gateway...",
		"restart": false,
	})
	// Gin's ResponseWriter implements http.Flusher. Flush before notifying main
	// so the browser receives the successful install result before this setup
	// server begins graceful shutdown.
	c.Writer.Flush()
	if onInstalled != nil {
		go onInstalled()
	}
}

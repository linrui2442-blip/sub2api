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
// intentionally absent.
func RegisterPersonalRoutes(r *gin.Engine) {
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
	protected.POST("/personal/install", installPersonalHandler)
}

func installPersonalHandler(c *gin.Context) {
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
		"message": "Personal Edition initialized successfully. Restart the application to continue.",
		"restart": true,
	})
}

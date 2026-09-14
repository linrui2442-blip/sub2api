package handler

import (
	"mime"
	"net/http"
	"strings"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Images handles the unified Gemini image adapter. It deliberately lives next
// to the existing Gemini gateway handler so account selection and Antigravity
// OAuth transport remain shared with the native Gemini route.
func (h *GatewayHandler) Images(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || h == nil || h.gatewayService == nil || h.openAIGatewayService == nil || h.antigravityGatewayService == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "invalid_api_key", "message": "Invalid API key"}})
		return
	}
	platform, _ := middleware.GetForcePlatformFromContext(c)
	if platform != service.PlatformAntigravity {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Gemini Images adapter is only available for Antigravity"}})
		return
	}
	mediaType, _, _ := mime.ParseMediaType(strings.TrimSpace(c.GetHeader("Content-Type")))
	var body []byte
	var err error
	if strings.EqualFold(mediaType, "multipart/form-data") {
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	} else {
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return
	}
	parsed, err := h.openAIGatewayService.ParseOpenAIImagesRequestForPlatform(c, body, platform)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return
	}
	if !service.IsGeminiImageModel(parsed.Model) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Gemini Images requires a Gemini image model"}})
		return
	}
	if parsed.N != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Gemini Images adapter supports n=1 only"}})
		return
	}
	sessionHash := h.openAIGatewayService.GenerateExplicitSessionHash(c, body)
	failed := map[int64]struct{}{}
	for attempts := 0; attempts < 4; attempts++ {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, parsed.Model, failed, "", 0)
		if err != nil || selection == nil || selection.Account == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "api_error", "message": "No available Gemini image accounts"}})
			return
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)
		resultErr := func() error {
			if selection.ReleaseFunc != nil {
				defer selection.ReleaseFunc()
			}
			_, forwardErr := h.openAIGatewayService.ForwardGeminiImages(c.Request.Context(), c, h.antigravityGatewayService, account, parsed)
			return forwardErr
		}()
		if resultErr == nil {
			return
		}
		if _, ok := resultErr.(*service.UpstreamFailoverError); ok {
			failed[account.ID] = struct{}{}
			continue
		}
		if !c.Writer.Written() {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": resultErr.Error()}})
		}
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "api_error", "message": "Gemini image account failover exhausted"}})
}

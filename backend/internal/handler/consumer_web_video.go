package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type consumerWebVideoCreateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Image  string `json:"image,omitempty"`
}

func consumerWebVideoError(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{"error": gin.H{"type": "video_error", "message": message}})
}

func (h *GatewayHandler) CreateConsumerWebVideo(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || h == nil || h.gatewayService == nil || h.consumerWebVideoTasks == nil {
		consumerWebVideoError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	platform, _ := middleware.GetForcePlatformFromContext(c)
	if platform != service.PlatformAntigravity {
		consumerWebVideoError(c, http.StatusBadRequest, "Consumer Web video requires an Antigravity account")
		return
	}
	var request consumerWebVideoCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 24<<20)).Decode(&request); err != nil || strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Prompt) == "" {
		consumerWebVideoError(c, http.StatusBadRequest, "Invalid video request")
		return
	}
	var image *service.ConsumerWebVideoUpload
	if request.Image != "" {
		encoded := request.Image
		if index := strings.IndexByte(encoded, ','); strings.HasPrefix(encoded, "data:image/") && index >= 0 {
			encoded = encoded[index+1:]
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 || len(data) > 20<<20 {
			consumerWebVideoError(c, http.StatusBadRequest, "Invalid image input")
			return
		}
		mime := http.DetectContentType(data)
		if !strings.HasPrefix(mime, "image/") {
			consumerWebVideoError(c, http.StatusBadRequest, "Unsupported image MIME")
			return
		}
		image = &service.ConsumerWebVideoUpload{Name: "image", MIME: mime, Data: data}
	}
	sessionHash := h.openAIGatewayService.GenerateExplicitSessionHash(c, []byte(request.Prompt))
	selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), key.GroupID, sessionHash, request.Model, nil, "", 0)
	if err != nil || selection == nil || selection.Account == nil {
		consumerWebVideoError(c, http.StatusServiceUnavailable, "No available video account")
		return
	}
	account := selection.Account
	if account.Platform != service.PlatformAntigravity || account.GetMappedModel(request.Model) == "" {
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		consumerWebVideoError(c, http.StatusBadRequest, "Selected account cannot serve video model")
		return
	}
	task, err := h.consumerWebVideoTasks.Start(key.ID, account, request.Model, request.Prompt, image, selection.ReleaseFunc)
	if err != nil {
		consumerWebVideoError(c, http.StatusServiceUnavailable, "Video task unavailable")
		return
	}
	c.JSON(http.StatusAccepted, task)
}

func (h *GatewayHandler) GetConsumerWebVideo(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || h == nil || h.consumerWebVideoTasks == nil {
		consumerWebVideoError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	task, err := h.consumerWebVideoTasks.Status(c.Request.Context(), c.Param("request_id"), key.ID)
	if err != nil {
		consumerWebVideoError(c, http.StatusNotFound, "Video task not found")
		return
	}
	c.JSON(http.StatusOK, task)
}

func (h *GatewayHandler) GetConsumerWebVideoContent(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || h == nil || h.consumerWebVideoTasks == nil {
		consumerWebVideoError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	content, mime, err := h.consumerWebVideoTasks.Content(c.Param("request_id"), key.ID)
	if err != nil {
		if errors.Is(err, service.ErrConsumerWebVideoTaskNotFound) {
			consumerWebVideoError(c, http.StatusNotFound, "Video task not found")
			return
		}
		consumerWebVideoError(c, http.StatusAccepted, "Video content not ready")
		return
	}
	if mime == "" {
		mime = "video/mp4"
	}
	c.Data(http.StatusOK, mime, content)
}

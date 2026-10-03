package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type consumerWebProbeScheduler interface {
	SelectAccountWithLoadAwareness(context.Context, *int64, string, string, map[int64]struct{}, string, int64) (*service.AccountSelectionResult, error)
}

type consumerWebProbeManager interface {
	AcquireSession(context.Context, *service.Account) (service.ConsumerWebSessionHandle, error)
}

// acquireConsumerWebForProbe deliberately has no video driver. The production
// handler passes its real scheduler and session manager to this seam.
func acquireConsumerWebForProbe(ctx context.Context, scheduler consumerWebProbeScheduler, manager consumerWebProbeManager, groupID *int64, sessionHash string) (*service.Account, *service.ConsumerWebBootstrapTrace, error) {
	selection, err := scheduler.SelectAccountWithLoadAwareness(ctx, groupID, sessionHash, "gemini-3.1-pro-high", nil, "", 0)
	if err != nil || selection == nil || selection.Account == nil {
		return nil, nil, service.ErrNoAvailableAccounts
	}
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}
	trace := &service.ConsumerWebBootstrapTrace{}
	_, err = manager.AcquireSession(service.WithConsumerWebBootstrapTrace(ctx, trace), selection.Account)
	return selection.Account, trace, err
}

// ConsumerWebAuthProbe is an acceptance-only seam. Route registration is
// independently gated; RemoteAddr is checked here without trusting Host or
// forwarded headers. It invokes the same scheduler and manager as video create.
func (h *GatewayHandler) ConsumerWebAuthProbe(c *gin.Context) {
	host, _, splitErr := net.SplitHostPort(c.Request.RemoteAddr)
	ip := net.ParseIP(host)
	if splitErr != nil || ip == nil || !ip.IsLoopback() {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if h == nil || h.gatewayService == nil || h.consumerWebManager == nil || h.openAIGatewayService == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	sessionHash := h.openAIGatewayService.GenerateExplicitSessionHash(c, []byte("consumer-web-auth-probe"))
	account, trace, err := acquireConsumerWebForProbe(c.Request.Context(), h.gatewayService, h.consumerWebManager, key.GroupID, sessionHash)
	if account == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"session_result": "FAIL", "safe_cause": "ACCOUNT_NOT_CONSUMER_WEB_READY"})
		return
	}
	response := gin.H{
		"account_id":                        account.ID,
		"session_result":                    "FAIL",
		"safe_cause":                        "UNKNOWN",
		"bootstrap_stage_sequence":          []service.ConsumerWebBootstrapStage{},
		"google_preflight_status":           0,
		"gemini_prime_status":               0,
		"app_status":                        0,
		"app_response_received":             false,
		"app_snlm0e_present":                false,
		"rotate_count":                      0,
		"rotate_status":                     0,
		"rotate_replacement_psidts_present": false,
		"rotate_persist_result":             "NOT_ATTEMPTED",
		"otaq_status":                       0,
		"account_status":                    string(h.consumerWebManager.State(account.ID)),
	}
	stages := make([]service.ConsumerWebBootstrapStage, 0)
	for _, step := range trace.Steps() {
		stages = append(stages, step.Stage)
		switch step.Stage {
		case service.ConsumerWebStageGooglePreflight:
			response["google_preflight_status"] = step.HTTPStatus
		case service.ConsumerWebStageGeminiPrime:
			response["gemini_prime_status"] = step.HTTPStatus
		case service.ConsumerWebStageGeminiAPP:
			response["app_status"] = step.HTTPStatus
			response["app_response_received"] = step.HTTPResponseReceived
			response["app_snlm0e_present"] = step.APPTokenPresent
		case service.ConsumerWebStageRotateCookies:
			response["rotate_count"] = response["rotate_count"].(int) + 1
			response["rotate_status"] = step.HTTPStatus
			response["rotate_replacement_psidts_present"] = step.RotateReplacementPresent
			response["rotate_persist_result"] = step.RotatePersistResult
		case service.ConsumerWebStageOTAQ:
			response["otaq_status"] = step.HTTPStatus
		}
	}
	response["bootstrap_stage_sequence"] = stages
	if err != nil {
		var typed *service.ConsumerWebAcquireError
		if errors.As(err, &typed) {
			response["safe_cause"] = typed.Cause
		}
		c.JSON(http.StatusServiceUnavailable, response)
		return
	}
	response["session_result"] = "PASS"
	response["safe_cause"] = "NONE"
	c.JSON(http.StatusOK, response)
}

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
	groupID := key.GroupID
	if groupID != nil {
		id := *groupID
		groupID = &id
	}
	selectNext := func(ctx context.Context, excluded map[int64]struct{}) (*service.AccountSelectionResult, error) {
		return h.gatewayService.SelectAccountWithLoadAwareness(ctx, groupID, sessionHash, request.Model, excluded, "", 0)
	}
	task, err := h.consumerWebVideoTasks.StartWithSelector(key.ID, selection, selectNext, request.Model, request.Prompt, image)
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

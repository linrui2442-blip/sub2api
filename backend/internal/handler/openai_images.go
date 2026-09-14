package handler

import (
	"mime"
	"net/http"
	"strings"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Images handles the Personal OpenAI Images surface. The service layer owns
// parsing, capability scheduling, account mapping, and upstream transport.
func (h *OpenAIGatewayHandler) Images(c *gin.Context) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		h.errorResponse(c, http.StatusUnauthorized, "invalid_api_key", "Invalid API key")
		return
	}
	var body []byte
	var err error
	mediaType, _, mediaTypeErr := mime.ParseMediaType(strings.TrimSpace(c.GetHeader("Content-Type")))
	if mediaTypeErr == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		// Multipart image payloads are binary containers; JSON control-byte
		// normalization would scan and potentially rewrite uploaded file bytes.
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	} else {
		body, err = readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	}
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	requestModel := parsed.Model
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), key.GroupID, requestModel)
	routingModel := requestModel
	if channelMapping.Mapped {
		routingModel = channelMapping.MappedModel
	}
	ctx := service.WithOpenAIImagesEndpoint(service.WithOpenAIImageGenerationIntent(c.Request.Context()))
	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	failed := map[int64]struct{}{}
	var lastFailover *service.UpstreamFailoverError
	for attempts := 0; attempts < 32; attempts++ {
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForImages(ctx, key.GroupID, sessionHash, routingModel, failed, parsed.RequiredCapability)
		if err != nil || selection == nil || selection.Account == nil {
			if lastFailover != nil {
				h.handleFailoverExhausted(c, lastFailover, false)
			} else {
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
			}
			return
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)
		resultErr := func() error {
			if selection.ReleaseFunc != nil {
				defer selection.ReleaseFunc()
			}
			_, forwardErr := h.gatewayService.ForwardImages(ctx, c, account, body, parsed, channelMapping.MappedModel)
			return forwardErr
		}()
		if resultErr == nil {
			return
		}
		fe, ok := resultErr.(*service.UpstreamFailoverError)
		if !ok {
			if !c.Writer.Written() {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", resultErr.Error())
			}
			return
		}
		failed[account.ID] = struct{}{}
		lastFailover = fe
	}
	if lastFailover != nil {
		h.handleFailoverExhausted(c, lastFailover, false)
	}
}

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// BuildGeminiImagesRequest converts the shared OpenAI Images request into the
// native Gemini contents shape. The transport, OAuth, mapping and scheduler
// remain owned by the existing Antigravity service.
func BuildGeminiImagesRequest(parsed *OpenAIImagesRequest) ([]byte, error) {
	if parsed == nil || !IsGeminiImageModel(parsed.Model) {
		return nil, fmt.Errorf("unsupported Gemini image model")
	}
	if parsed.N != 1 {
		return nil, fmt.Errorf("Gemini Images adapter supports n=1 only")
	}
	parts := make([]map[string]any, 0, len(parsed.Uploads)+1)
	if prompt := strings.TrimSpace(parsed.Prompt); prompt != "" {
		parts = append(parts, map[string]any{"text": prompt})
	}
	for _, upload := range parsed.Uploads {
		if len(upload.Data) == 0 {
			return nil, fmt.Errorf("image upload %q is empty", upload.FileName)
		}
		mimeType := strings.TrimSpace(upload.ContentType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		parts = append(parts, map[string]any{"inlineData": map[string]any{
			"mimeType": mimeType,
			"data":     base64.StdEncoding.EncodeToString(upload.Data),
		}})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("prompt or image input is required")
	}
	payload := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}
	if ratio := geminiImageAspectRatio(parsed.Size); ratio != "" {
		payload["generationConfig"].(map[string]any)["imageConfig"] = map[string]any{"aspectRatio": ratio}
	}
	return json.Marshal(payload)
}

func geminiImageAspectRatio(size string) string {
	switch strings.TrimSpace(strings.ToLower(size)) {
	case "1024x1024", "square", "1:1":
		return "1:1"
	case "1536x1024", "1792x1024", "16:9":
		return "16:9"
	case "1024x1536", "1024x1792", "9:16":
		return "9:16"
	default:
		return ""
	}
}

type geminiImageResult struct {
	Data     string
	MimeType string
}

func collectGeminiImageResults(body []byte) []geminiImageResult {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return nil
	}
	var out []geminiImageResult
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if inline, ok := value["inlineData"].(map[string]any); ok {
				data, _ := inline["data"].(string)
				mimeType, _ := inline["mimeType"].(string)
				if strings.TrimSpace(data) != "" {
					out = append(out, geminiImageResult{Data: data, MimeType: mimeType})
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(root)
	return out
}

func (s *OpenAIGatewayService) forwardGeminiImages(
	ctx context.Context,
	c *gin.Context,
	antigravity *AntigravityGatewayService,
	account *Account,
	parsed *OpenAIImagesRequest,
) (*OpenAIForwardResult, error) {
	if antigravity == nil || account == nil {
		return nil, fmt.Errorf("Gemini Images transport is unavailable")
	}
	geminiBody, err := BuildGeminiImagesRequest(parsed)
	if err != nil {
		return nil, err
	}
	// Reuse ForwardGemini (including OAuth, proxy, endpoint failover and
	// response collection) while isolating its native JSON writer. The parent
	// request is written only after the shared transport succeeds.
	recorder := httptest.NewRecorder()
	child, _ := gin.CreateTestContext(recorder)
	child.Request = c.Request.Clone(ctx)
	child.Keys = c.Keys
	child.Params = c.Params
	result, err := antigravity.ForwardGemini(ctx, child, account, parsed.Model, "generateContent", false, geminiBody, false)
	if err != nil {
		return nil, err
	}
	images := collectGeminiImageResults(recorder.Body.Bytes())
	if len(images) == 0 {
		return nil, fmt.Errorf("Gemini upstream returned no inline image data")
	}
	openAIResults := make([]openAIResponsesImageResult, 0, len(images))
	for _, image := range images {
		openAIResults = append(openAIResults, openAIResponsesImageResult{
			Result: image.Data, Model: parsed.Model, OutputFormat: image.MimeType,
			Size: parsed.Size,
		})
	}
	if parsed.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			return nil, fmt.Errorf("streaming is not supported by response writer")
		}
		for _, image := range openAIResults {
			payload := buildOpenAIImagesStreamCompletedPayload(openAIImagesStreamPrefix(parsed)+".completed", image, parsed.ResponseFormat, 0, nil)
			if err := s.writeOpenAIImagesStreamEvent(c, flusher, openAIImagesStreamPrefix(parsed)+".completed", payload); err != nil {
				return nil, err
			}
		}
	} else {
		responseBody, err := buildOpenAIImagesAPIResponse(openAIResults, time.Now().Unix(), nil, openAIResults[0], parsed.ResponseFormat)
		if err != nil {
			return nil, err
		}
		c.Data(200, "application/json; charset=utf-8", responseBody)
	}
	if result == nil {
		result = &ForwardResult{}
	}
	return &OpenAIForwardResult{Model: parsed.Model, UpstreamModel: parsed.Model, Stream: parsed.Stream, ImageCount: len(openAIResults)}, nil
}

// ForwardGeminiImages is the unified Images adapter entry point used by the
// OpenAI-compatible handler. It does not create a second scheduler or auth
// path; the supplied Antigravity service performs the actual account transport.
func (s *OpenAIGatewayService) ForwardGeminiImages(
	ctx context.Context,
	c *gin.Context,
	antigravity *AntigravityGatewayService,
	account *Account,
	parsed *OpenAIImagesRequest,
) (*OpenAIForwardResult, error) {
	return s.forwardGeminiImages(ctx, c, antigravity, account, parsed)
}

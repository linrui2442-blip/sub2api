package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http/httptest"
)

func TestBuildGeminiImagesRequestGeneration(t *testing.T) {
	parsed := &OpenAIImagesRequest{Model: "gemini-3.1-flash-image", Prompt: "draw", N: 1, Size: "1024x1024"}
	body, err := BuildGeminiImagesRequest(parsed)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, []any{"TEXT", "IMAGE"}, payload["generationConfig"].(map[string]any)["responseModalities"])
	require.Equal(t, "1:1", payload["generationConfig"].(map[string]any)["imageConfig"].(map[string]any)["aspectRatio"])
}

func TestBuildGeminiImagesRequestEditUsesInlineData(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesEditsEndpoint, Model: "gemini-3.1-flash-image", Prompt: "edit", N: 1,
		Uploads: []OpenAIImagesUpload{{FileName: "x.png", ContentType: "image/png", Data: []byte("png")}},
	}
	body, err := BuildGeminiImagesRequest(parsed)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	parts := payload["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	require.Equal(t, "edit", parts[0].(map[string]any)["text"])
	inline := parts[1].(map[string]any)["inlineData"].(map[string]any)
	require.Equal(t, "image/png", inline["mimeType"])
	require.Equal(t, "cG5n", inline["data"])
}

func TestGeminiImagesAdapterNAndModelValidation(t *testing.T) {
	_, err := BuildGeminiImagesRequest(&OpenAIImagesRequest{Model: "gemini-3.1-flash-image", Prompt: "x", N: 2})
	require.Error(t, err)
	_, err = BuildGeminiImagesRequest(&OpenAIImagesRequest{Model: "gemini-3.1-pro", Prompt: "x", N: 1})
	require.Error(t, err)
}

func TestGeminiImagesResponseConversionAndCompletedEvents(t *testing.T) {
	results := []geminiImageResult{{Data: "aGVsbG8=", MimeType: "image/png"}}
	openAI := make([]openAIResponsesImageResult, 0, 1)
	for _, image := range results {
		openAI = append(openAI, openAIResponsesImageResult{Result: image.Data, Model: "gemini-3.1-flash-image", OutputFormat: image.MimeType})
	}
	b64Response, err := buildOpenAIImagesAPIResponse(openAI, 1, nil, openAI[0], "b64_json")
	require.NoError(t, err)
	require.Equal(t, "aGVsbG8=", gjson.GetBytes(b64Response, "data.0.b64_json").String())
	urlResponse, err := buildOpenAIImagesAPIResponse(openAI, 1, nil, openAI[0], "url")
	require.NoError(t, err)
	require.Equal(t, "data:image/png;base64,aGVsbG8=", gjson.GetBytes(urlResponse, "data.0.url").String())
	payload := buildOpenAIImagesStreamCompletedPayload("image_generation.completed", openAI[0], "b64_json", 1, nil)
	require.Contains(t, string(payload), `"type":"image_generation.completed"`)
	editPayload := buildOpenAIImagesStreamCompletedPayload("image_edit.completed", openAI[0], "b64_json", 1, nil)
	require.Contains(t, string(editPayload), `"type":"image_edit.completed"`)
}

func TestParseOpenAIImagesRequestForAntigravityPreservesSharedParser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"gemini-3.1-flash-image","prompt":"draw","n":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestForPlatform(c, []byte(`{"model":"gemini-3.1-flash-image","prompt":"draw","n":1}`), PlatformAntigravity)
	require.NoError(t, err)
	require.Equal(t, "gemini-3.1-flash-image", parsed.Model)
}

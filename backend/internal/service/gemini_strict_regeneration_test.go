package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const strictRegenerationFormat = `{"type":"json_schema","json_schema":{"name":"GenericResult","strict":true,"schema":{"type":"object","additionalProperties":false,"properties":{"kind":{"type":"string","enum":["alpha","beta"]},"label":{"type":"string","minLength":2,"maxLength":8},"choice":{"oneOf":[{"type":"string","enum":["left"]},{"type":"string","enum":["right"]}]}} ,"required":["kind","label","choice"]}}}`

func strictRegenerationBody(stream bool) []byte {
	body, _ := json.Marshal(map[string]any{
		"model":           "gemini-3.1-pro-high",
		"messages":        []any{map[string]any{"role": "user", "content": "Return JSON."}},
		"response_format": json.RawMessage(strictRegenerationFormat),
		"stream":          stream,
	})
	return body
}

func antigravityRegenerationResponse(t *testing.T, text, finish string, inputTokens, outputTokens int) *http.Response {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"response": map[string]any{
			"responseId": "resp_regeneration",
			"candidates": []any{map[string]any{
				"content":      map[string]any{"parts": []any{map[string]any{"text": text}}},
				"finishReason": finish,
			}},
			"usageMetadata": map[string]any{"promptTokenCount": inputTokens, "candidatesTokenCount": outputTokens},
		},
	})
	require.NoError(t, err)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\n")),
	}
}

func geminiRequestObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var root map[string]any
	require.NoError(t, json.Unmarshal(body, &root))
	if request, ok := root["request"].(map[string]any); ok {
		return request
	}
	return root
}

func geminiSystemInstructionText(t *testing.T, body []byte) string {
	t.Helper()
	request := geminiRequestObject(t, body)
	system, _ := request["systemInstruction"].(map[string]any)
	parts, _ := system["parts"].([]any)
	var texts []string
	for _, part := range parts {
		partMap, _ := part.(map[string]any)
		if text, ok := partMap["text"].(string); ok {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func TestAntigravityStrictRegenerationPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const firstSecret = "SUPER_SECRET_FIRST_OUTPUT_551"
	const secondSecret = "PRIVATE_BAD_JSON_VALUE_229"
	valid := `{"kind":"alpha","label":"valid","choice":"left"}`
	tests := []struct {
		name           string
		responses      []*http.Response
		wantStatus     int
		wantCalls      int
		wantInput      int
		wantOutput     int
		wantErr        bool
		wantPublicText string
		wantCorrective string
	}{
		{
			name: "first malformed second valid",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, firstSecret+secondSecret, "STOP", 5, 2),
				antigravityRegenerationResponse(t, valid, "STOP", 7, 3),
			},
			wantStatus: http.StatusOK, wantCalls: 2, wantInput: 12, wantOutput: 5,
			wantCorrective: strictJSONParsingCorrectiveInstruction,
		},
		{
			name: "first malformed second malformed",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, firstSecret, "STOP", 5, 2),
				antigravityRegenerationResponse(t, secondSecret, "STOP", 7, 3),
			},
			wantStatus: http.StatusBadGateway, wantCalls: 2, wantInput: 12, wantOutput: 5, wantErr: true,
			wantPublicText: "structured_output_validation_error",
			wantCorrective: strictJSONParsingCorrectiveInstruction,
		},
		{
			name: "first valid",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, valid, "STOP", 5, 2),
			},
			wantStatus: http.StatusOK, wantCalls: 1, wantInput: 5, wantOutput: 2,
		},
		{
			name: "enum invalid second valid",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, `{"kind":"other","label":"valid","choice":"left"}`, "STOP", 5, 2),
				antigravityRegenerationResponse(t, valid, "STOP", 7, 3),
			},
			wantStatus: http.StatusOK, wantCalls: 2, wantInput: 12, wantOutput: 5,
			wantCorrective: strictJSONValidationCorrectiveInstruction,
		},
		{
			name: "enum invalid second invalid",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, `{"kind":"other","label":"valid","choice":"left"}`, "STOP", 5, 2),
				antigravityRegenerationResponse(t, `{"kind":"other","label":"valid","choice":"left"}`, "STOP", 7, 3),
			},
			wantStatus: http.StatusBadGateway, wantCalls: 2, wantInput: 12, wantOutput: 5, wantErr: true,
			wantPublicText: "structured_output_validation_error",
			wantCorrective: strictJSONValidationCorrectiveInstruction,
		},
		{
			name: "minLength invalid second valid",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, `{"kind":"alpha","label":"x","choice":"left"}`, "STOP", 5, 2),
				antigravityRegenerationResponse(t, valid, "STOP", 7, 3),
			},
			wantStatus: http.StatusOK, wantCalls: 2, wantInput: 12, wantOutput: 5,
			wantCorrective: strictJSONValidationCorrectiveInstruction,
		},
		{
			name: "oneOf invalid second valid",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, `{"kind":"alpha","label":"valid","choice":"neither"}`, "STOP", 5, 2),
				antigravityRegenerationResponse(t, valid, "STOP", 7, 3),
			},
			wantStatus: http.StatusOK, wantCalls: 2, wantInput: 12, wantOutput: 5,
			wantCorrective: strictJSONValidationCorrectiveInstruction,
		},
		{
			name: "max tokens does not regenerate",
			responses: []*http.Response{
				antigravityRegenerationResponse(t, firstSecret, "MAX_TOKENS", 5, 2),
			},
			wantStatus: http.StatusBadGateway, wantCalls: 1, wantInput: 5, wantOutput: 2, wantErr: true,
			wantPublicText: "structured_output_validation_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: tt.responses}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			body := strictRegenerationBody(false)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, result)
			require.Equal(t, tt.wantStatus, recorder.Code)
			require.Len(t, upstream.requestBodies, tt.wantCalls)
			require.Equal(t, tt.wantInput, result.Usage.InputTokens)
			require.Equal(t, tt.wantOutput, result.Usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), tt.wantPublicText)
			require.NotContains(t, recorder.Body.String(), firstSecret)
			require.NotContains(t, recorder.Body.String(), secondSecret)
			require.NotContains(t, geminiSystemInstructionText(t, upstream.requestBodies[0]), "previous generation")
			if tt.wantCalls == 2 {
				secondRequest := string(upstream.requestBodies[1])
				require.Contains(t, geminiSystemInstructionText(t, upstream.requestBodies[1]), tt.wantCorrective)
				require.NotContains(t, secondRequest, firstSecret)
				require.NotContains(t, secondRequest, secondSecret)
				for _, forbidden := range []string{"Path", "Keyword", "Expected", "ActualType", "raw parser error"} {
					require.NotContains(t, secondRequest, forbidden)
				}
			}
			if err != nil {
				require.NotContains(t, err.Error(), firstSecret)
				require.NotContains(t, err.Error(), secondSecret)
			}
		})
	}
}

func nativeRegenerationResponse(t *testing.T, text, finish string, inputTokens, outputTokens int) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"parts": []any{map[string]any{"text": text}}},
			"finishReason": finish,
		}},
		"usageMetadata": map[string]any{"promptTokenCount": inputTokens, "candidatesTokenCount": outputTokens},
	})
	require.NoError(t, err)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}
}

func nativeOAuthRegenerationResponse(t *testing.T, text, finish string, inputTokens, outputTokens int) *http.Response {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"response": map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"parts": []any{map[string]any{"text": text}}},
				"finishReason": finish,
			}},
			"usageMetadata": map[string]any{"promptTokenCount": inputTokens, "candidatesTokenCount": outputTokens},
		},
	})
	require.NoError(t, err)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\ndata: [DONE]\n\n")),
	}
}

func TestNativeGeminiStrictRegenerationAggregatesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := `{"kind":"alpha","label":"valid","choice":"left"}`
	stub := &geminiCompatHTTPUpstreamStub{responses: []*http.Response{
		nativeRegenerationResponse(t, "SUPER_SECRET_FIRST_OUTPUT_551", "STOP", 11, 4),
		nativeRegenerationResponse(t, valid, "STOP", 13, 6),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	account := &Account{ID: 101, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key"}}
	body := strictRegenerationBody(false)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 2, stub.calls)
	require.Equal(t, 24, result.Usage.InputTokens)
	require.Equal(t, 10, result.Usage.OutputTokens)
	require.NotContains(t, recorder.Body.String(), "SUPER_SECRET_FIRST_OUTPUT_551")
	require.Len(t, stub.requestBodies, 2)
	require.Contains(t, geminiSystemInstructionText(t, stub.requestBodies[1]), strictJSONParsingCorrectiveInstruction)
	require.NotContains(t, string(stub.requestBodies[1]), "SUPER_SECRET_FIRST_OUTPUT_551")
}

func TestNativeGeminiStrictSecondFailureWritesOneSanitizedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &geminiCompatHTTPUpstreamStub{responses: []*http.Response{
		nativeRegenerationResponse(t, "SUPER_SECRET_FIRST_OUTPUT_551", "STOP", 11, 4),
		nativeRegenerationResponse(t, "PRIVATE_BAD_JSON_VALUE_229", "STOP", 13, 6),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	account := &Account{ID: 101, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key"}}
	body := strictRegenerationBody(false)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)

	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Equal(t, 2, stub.calls)
	require.Equal(t, 24, result.Usage.InputTokens)
	require.Equal(t, 10, result.Usage.OutputTokens)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"error"`))
	require.Contains(t, recorder.Body.String(), "structured_output_validation_error")
	require.NotContains(t, recorder.Body.String(), "SUPER_SECRET_FIRST_OUTPUT_551")
	require.NotContains(t, recorder.Body.String(), "PRIVATE_BAD_JSON_VALUE_229")
	require.Len(t, stub.requestBodies, 2)
	require.Contains(t, geminiSystemInstructionText(t, stub.requestBodies[1]), strictJSONParsingCorrectiveInstruction)
	require.NotContains(t, string(stub.requestBodies[1]), "SUPER_SECRET_FIRST_OUTPUT_551")
	require.NotContains(t, string(stub.requestBodies[1]), "PRIVATE_BAD_JSON_VALUE_229")
}

func TestNativeGeminiOAuthStrictRegenerationKeepsAccountAndImageCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := `{"kind":"alpha","label":"valid","choice":"left"}`
	stub := &geminiCompatHTTPUpstreamStub{responses: []*http.Response{
		nativeOAuthRegenerationResponse(t, "SUPER_SECRET_FIRST_OUTPUT_551", "STOP", 3, 2),
		nativeOAuthRegenerationResponse(t, valid, "STOP", 5, 4),
	}}
	svc := &GeminiMessagesCompatService{tokenProvider: &GeminiTokenProvider{}, httpUpstream: stub, cfg: &config.Config{}}
	account := &Account{
		ID: 101, Platform: PlatformGemini, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token", "project_id": "test-project"},
	}
	body, err := json.Marshal(map[string]any{
		"model": "gemini-3.1-pro-high",
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "Return JSON."},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="}},
			},
		}},
		"response_format": json.RawMessage(strictRegenerationFormat),
		"stream":          false,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 2, stub.calls)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 6, result.Usage.OutputTokens)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "Bearer test-token", stub.lastReq.Header.Get("Authorization"))
	postedBody, readErr := io.ReadAll(stub.lastReq.Body)
	require.NoError(t, readErr)
	require.Contains(t, string(postedBody), "test-project")
	require.NotContains(t, recorder.Body.String(), "SUPER_SECRET_FIRST_OUTPUT_551")
	require.Len(t, stub.requestBodies, 2)
	firstRequest := geminiRequestObject(t, stub.requestBodies[0])
	secondRequest := geminiRequestObject(t, stub.requestBodies[1])
	require.Equal(t, firstRequest["contents"], secondRequest["contents"])
	require.Contains(t, geminiSystemInstructionText(t, stub.requestBodies[1]), strictJSONParsingCorrectiveInstruction)
	require.NotContains(t, string(stub.requestBodies[1]), "SUPER_SECRET_FIRST_OUTPUT_551")
}

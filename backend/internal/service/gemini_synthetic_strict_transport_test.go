package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const syntheticStrictFormat = `{"type":"json_schema","json_schema":{"name":"GenericResult","strict":true,"schema":{"type":"object","additionalProperties":false,"properties":{"kind":{"type":"string","enum":["alpha","beta"]},"label":{"type":"string","minLength":2,"maxLength":8},"choice":{"oneOf":[{"type":"string","enum":["left"]},{"type":"string","enum":["right"]}]}} ,"required":["kind","label","choice"]}}}`

func syntheticStrictBody(extra map[string]any) []byte {
	body := map[string]any{
		"model": "gemini-3.1-pro-high", "messages": []any{map[string]any{"role": "user", "content": "Return structured data."}},
		"response_format": json.RawMessage(syntheticStrictFormat), "stream": false,
	}
	for key, value := range extra {
		body[key] = value
	}
	encoded, _ := json.Marshal(body)
	return encoded
}

func syntheticAntigravityResponse(t *testing.T, calls []map[string]any, inputTokens, outputTokens int) *http.Response {
	t.Helper()
	parts := make([]any, 0, len(calls))
	for _, call := range calls {
		parts = append(parts, map[string]any{"functionCall": call})
	}
	payload, err := json.Marshal(map[string]any{"response": map[string]any{
		"responseId": "resp_synthetic", "candidates": []any{map[string]any{
			"content": map[string]any{"parts": parts}, "finishReason": "STOP",
		}}, "usageMetadata": map[string]any{"promptTokenCount": inputTokens, "candidatesTokenCount": outputTokens},
	}})
	require.NoError(t, err)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\n"))}
}

func syntheticCall(value any) map[string]any {
	return map[string]any{"name": syntheticStrictJSONFunctionName, "args": map[string]any{"value": value}}
}

func assertSyntheticFinalWire(t *testing.T, body []byte) {
	t.Helper()
	root := mustObject(t, body)
	request := mustMapValue(t, root["request"])
	tools, ok := request["tools"].([]any)
	require.True(t, ok)
	declarations, ok := mustMapValue(t, tools[0])["functionDeclarations"].([]any)
	require.True(t, ok)
	declaration := mustMapValue(t, declarations[0])
	require.Equal(t, syntheticStrictJSONFunctionName, declaration["name"])
	parameters := mustMapValue(t, declaration["parametersJsonSchema"])
	require.Equal(t, false, parameters["additionalProperties"])
	require.Equal(t, []any{"value"}, parameters["required"])
	require.Contains(t, mustMapValue(t, parameters["properties"]), "value")
	require.NotContains(t, request, "responseJsonSchema")
	config := mustMapValue(t, mustMapValue(t, request["toolConfig"])["functionCallingConfig"])
	require.Equal(t, "ANY", config["mode"])
	require.Equal(t, []any{syntheticStrictJSONFunctionName}, config["allowedFunctionNames"])
}

func TestAntigravitySyntheticStrictTransportWireAndSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	value := map[string]any{"kind": "alpha", "label": "valid", "choice": "left"}
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{syntheticAntigravityResponse(t, []map[string]any{syntheticCall(value)}, 5, 2)}}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
	body := syntheticStrictBody(nil)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, upstream.requestBodies, 1)
	assertSyntheticFinalWire(t, upstream.requestBodies[0])
	require.Contains(t, recorder.Body.String(), `\"kind\":\"alpha\"`)
	for _, internal := range []string{syntheticStrictJSONFunctionName, "tool_calls", "function_call", "allowedFunctionNames"} {
		require.NotContains(t, recorder.Body.String(), internal)
	}
}

func TestAntigravitySyntheticStrictRetryPolicyAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := map[string]any{"kind": "alpha", "label": "valid", "choice": "left"}
	invalid := map[string]any{"kind": "PRIVATE_FAILED_ATTEMPT_992", "label": "x", "choice": "neither"}
	tests := []struct {
		name       string
		responses  []*http.Response
		wantStatus int
		wantCalls  int
		wantError  bool
	}{
		{"first valid", []*http.Response{syntheticAntigravityResponse(t, []map[string]any{syntheticCall(valid)}, 5, 2)}, http.StatusOK, 1, false},
		{"invalid then valid", []*http.Response{syntheticAntigravityResponse(t, []map[string]any{syntheticCall(invalid)}, 5, 2), syntheticAntigravityResponse(t, []map[string]any{syntheticCall(valid)}, 7, 3)}, http.StatusOK, 2, false},
		{"invalid twice", []*http.Response{syntheticAntigravityResponse(t, []map[string]any{syntheticCall(invalid)}, 5, 2), syntheticAntigravityResponse(t, []map[string]any{syntheticCall(invalid)}, 7, 3)}, http.StatusBadGateway, 2, true},
		{"extraction failure", []*http.Response{syntheticAntigravityResponse(t, nil, 5, 2)}, http.StatusBadGateway, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: tt.responses}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			body := syntheticStrictBody(nil)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)
			result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, result)
			require.Equal(t, tt.wantStatus, recorder.Code)
			require.Len(t, upstream.requestBodies, tt.wantCalls)
			if tt.wantCalls == 2 {
				require.Equal(t, 12, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				require.JSONEq(t, string(upstream.requestBodies[0]), string(upstream.requestBodies[1]))
			}
			require.NotContains(t, recorder.Body.String(), "PRIVATE_FAILED_ATTEMPT_992")
			if err != nil {
				require.NotContains(t, err.Error(), "PRIVATE_FAILED_ATTEMPT_992")
			}
		})
	}
}

func TestSyntheticStrictExtractionMatrix(t *testing.T) {
	correct := apicompat.AnthropicContentBlock{Type: "tool_use", Name: syntheticStrictJSONFunctionName, Input: json.RawMessage(`{"value":null}`)}
	tests := []struct {
		name   string
		blocks []apicompat.AnthropicContentBlock
		ok     bool
	}{
		{"one", []apicompat.AnthropicContentBlock{correct}, true}, {"zero", nil, false},
		{"wrong", []apicompat.AnthropicContentBlock{{Type: "tool_use", Name: "other", Input: json.RawMessage(`{"value":1}`)}}, false},
		{"missing args", []apicompat.AnthropicContentBlock{{Type: "tool_use", Name: syntheticStrictJSONFunctionName}}, false},
		{"missing value", []apicompat.AnthropicContentBlock{{Type: "tool_use", Name: syntheticStrictJSONFunctionName, Input: json.RawMessage(`{}`)}}, false},
		{"multiple matching", []apicompat.AnthropicContentBlock{correct, correct}, false},
		{"matching unrelated", []apicompat.AnthropicContentBlock{correct, {Type: "tool_use", Name: "other", Input: json.RawMessage(`{}`)}}, false},
		{"multiple unrelated", []apicompat.AnthropicContentBlock{{Type: "tool_use", Name: "a"}, {Type: "tool_use", Name: "b"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractGeminiSyntheticStrictValue(&apicompat.AnthropicResponse{Content: tt.blocks})
			if tt.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSyntheticStrictWrapperExtrasDoNotLeak(t *testing.T) {
	response := &apicompat.AnthropicResponse{Content: []apicompat.AnthropicContentBlock{{
		Type: "tool_use",
		Name: syntheticStrictJSONFunctionName,
		Input: json.RawMessage(
			`{"value":"public-result","unexpected":"SUPER_SECRET_SYNTHETIC_ARG_431"}`,
		),
	}}}

	value, err := extractGeminiSyntheticStrictValue(response)
	require.NoError(t, err)
	require.Equal(t, "public-result", value)

	_, err = extractGeminiSyntheticStrictValue(&apicompat.AnthropicResponse{Content: []apicompat.AnthropicContentBlock{{
		Type:  "tool_use",
		Name:  syntheticStrictJSONFunctionName,
		Input: json.RawMessage(`{"unexpected":"SUPER_SECRET_SYNTHETIC_ARG_431"}`),
	}}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SUPER_SECRET_SYNTHETIC_ARG_431")
}

func TestSyntheticStrictRootTypesAndNestedValue(t *testing.T) {
	cases := []struct {
		name   string
		schema map[string]any
		value  any
	}{
		{"object", map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}}, map[string]any{"value": "nested caller value"}},
		{"array", map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, []any{float64(1)}},
		{"string", map[string]any{"type": "string"}, "x"}, {"number", map[string]any{"type": "number"}, 1.5}, {"integer", map[string]any{"type": "integer"}, float64(2)},
		{"boolean", map[string]any{"type": "boolean"}, true}, {"null", map[string]any{"type": "null"}, nil},
		{"nullable", map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			response := &apicompat.AnthropicResponse{Content: []apicompat.AnthropicContentBlock{{Type: "tool_use", Name: syntheticStrictJSONFunctionName, Input: mustJSONRaw(t, map[string]any{"value": tt.value})}}}
			chat, err := syntheticStrictChatResponse(response, "gemini", &geminiStructuredOutput{Strict: true, Schema: tt.schema})
			require.NoError(t, err)
			require.NotNil(t, chat)
			require.NotContains(t, string(chat.Choices[0].Message.Content), syntheticStrictJSONFunctionName)
		})
	}
}

func TestSyntheticStrictOriginalSchemaValidationMatrix(t *testing.T) {
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"kind", "items", "score", "label", "union", "exclusive"},
		"properties": map[string]any{
			"kind":      map[string]any{"type": "string", "enum": []any{"alpha"}},
			"items":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "maxItems": float64(2)},
			"score":     map[string]any{"type": "number", "minimum": float64(1), "maximum": float64(2)},
			"label":     map[string]any{"type": "string", "minLength": float64(2), "maxLength": float64(4)},
			"union":     map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}},
			"exclusive": map[string]any{"oneOf": []any{map[string]any{"type": "number"}, map[string]any{"minimum": float64(0)}}},
		},
	}
	validBase := func() map[string]any {
		return map[string]any{"kind": "alpha", "items": []any{float64(1)}, "score": 1.5, "label": "good", "union": nil, "exclusive": float64(-1)}
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
		valid  bool
	}{
		{"valid complex", func(map[string]any) {}, true},
		{"required", func(v map[string]any) { delete(v, "kind") }, false},
		{"additional", func(v map[string]any) { v["extra"] = true }, false},
		{"enum", func(v map[string]any) { v["kind"] = "PRIVATE_CALLER_VALUE_812" }, false},
		{"maxItems", func(v map[string]any) { v["items"] = []any{1.0, 2.0, 3.0} }, false},
		{"minimum", func(v map[string]any) { v["score"] = 0.0 }, false}, {"maximum", func(v map[string]any) { v["score"] = 3.0 }, false},
		{"anyOf", func(v map[string]any) { v["union"] = true }, false}, {"oneOf exact", func(v map[string]any) { v["exclusive"] = 1.0 }, false},
		{"minLength", func(v map[string]any) { v["label"] = "x" }, false}, {"maxLength", func(v map[string]any) { v["label"] = "excess" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := validBase()
			tt.mutate(value)
			response := &apicompat.AnthropicResponse{Content: []apicompat.AnthropicContentBlock{{Type: "tool_use", Name: syntheticStrictJSONFunctionName, Input: mustJSONRaw(t, map[string]any{"value": value})}}}
			_, err := syntheticStrictChatResponse(response, "gemini", &geminiStructuredOutput{Strict: true, Schema: schema})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "PRIVATE_CALLER_VALUE_812")
			}
		})
	}
}

func TestSyntheticStrictLargeSchemasPreserveStructure(t *testing.T) {
	shapes := map[string]map[string]any{}
	shapes["small realistic"] = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []any{"id"}}
	mediumProps := map[string]any{}
	for i := 0; i < 40; i++ {
		mediumProps[fmt.Sprintf("field_%d", i)] = map[string]any{"type": "string", "description": "generic field"}
	}
	shapes["medium realistic"] = map[string]any{"type": "object", "additionalProperties": false, "properties": mediumProps}
	deep := map[string]any{"type": "string"}
	for i := 0; i < 12; i++ {
		deep = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"child": deep}, "required": []any{"child"}}
	}
	shapes["deep nested"] = deep
	largeProps := map[string]any{}
	for i := 0; i < 200; i++ {
		largeProps[fmt.Sprintf("property_%03d", i)] = map[string]any{"type": "integer", "minimum": float64(0)}
	}
	shapes["large realistic"] = map[string]any{"type": "object", "additionalProperties": false, "properties": largeProps}
	enums := make([]any, 100)
	for i := range enums {
		enums[i] = fmt.Sprintf("choice_%03d", i)
	}
	shapes["enum heavy"] = map[string]any{"enum": enums}
	shapes["array heavy"] = map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"n": map[string]any{"type": "number"}}}}, "maxItems": float64(500)}
	for name, schema := range shapes {
		t.Run(name, func(t *testing.T) {
			body, err := applyGeminiSyntheticStrictTransport([]byte(`{"contents":[{"role":"user","parts":[{"text":"x"}]}]}`), &geminiStructuredOutput{Strict: true, Schema: schema})
			require.NoError(t, err)
			require.Contains(t, string(body), syntheticStrictJSONFunctionName)
			require.NotContains(t, string(body), `"$schema"`)
			if name == "enum heavy" {
				var root map[string]any
				require.NoError(t, json.Unmarshal(body, &root))
				tools, ok := root["tools"].([]any)
				require.True(t, ok)
				decls, ok := mustMapValue(t, tools[0])["functionDeclarations"].([]any)
				require.True(t, ok)
				params := mustMapValue(t, mustMapValue(t, decls[0])["parametersJsonSchema"])
				valueSchema := mustMapValue(t, mustMapValue(t, params["properties"])["value"])
				require.Equal(t, "string", valueSchema["type"])
			}
		})
	}
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func TestAntigravitySyntheticStrictRetryDoesNotDuplicateImageCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	invalid := map[string]any{"kind": "bad", "label": "x", "choice": "bad"}
	valid := map[string]any{"kind": "alpha", "label": "valid", "choice": "left"}
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{syntheticAntigravityResponse(t, []map[string]any{syntheticCall(invalid)}, 2, 1), syntheticAntigravityResponse(t, []map[string]any{syntheticCall(valid)}, 3, 2)}}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
	bodyMap := map[string]any{
		"model": "gemini-3.1-pro-high",
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "x"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="}},
			},
		}},
		"response_format": json.RawMessage(syntheticStrictFormat),
	}
	body, _ := json.Marshal(bodyMap)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, upstream.requestBodies, 2)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
}

func TestAntigravitySyntheticStrictProviderRejectionDoesNotRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"message":"PRIVATE_PROVIDER_SCHEMA_REJECTION_557"}}`,
		)),
	}}}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
	body := syntheticStrictBody(nil)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(
		context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil,
	)

	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requestBodies, 1)
	require.NotContains(t, recorder.Body.String(), "PRIVATE_PROVIDER_SCHEMA_REJECTION_557")
}

func TestAntigravitySyntheticStrictToolConflictsFailBeforeProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := map[string]map[string]any{
		"caller function": {"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "f", "parameters": map[string]any{"type": "object"}}}}},
		"tool choice":     {"tool_choice": "required"}, "built in": {"tools": []any{map[string]any{"type": "x_search"}}},
		"mixed":    {"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "f"}}, map[string]any{"type": "x_search"}}},
		"parallel": {"parallel_tool_calls": true},
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			body := syntheticStrictBody(extra)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)
			result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), "cannot currently be combined")
			require.Empty(t, upstream.requestBodies)
		})
	}
}

func TestNativeGeminiStrictRemainsNativeAndDoesNotRegenerate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &geminiCompatHTTPUpstreamStub{responses: []*http.Response{{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader([]byte(`{"candidates":[{"content":{"parts":[{"text":"not-json"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2}}`)))}}}
	svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
	account := &Account{ID: 101, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key"}}
	body := syntheticStrictBody(nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, stub.calls)
	require.Len(t, stub.requestBodies, 1)
	request := mustObject(t, stub.requestBodies[0])
	config := mustMapValue(t, request["generationConfig"])
	require.Equal(t, "application/json", config["responseMimeType"])
	require.Contains(t, config, "responseJsonSchema")
	require.NotContains(t, string(stub.requestBodies[0]), syntheticStrictJSONFunctionName)
	var diagnostic *StrictJSONValidationError
	require.True(t, errors.As(err, &diagnostic))
	require.Equal(t, "parsing", diagnostic.Stage)
}

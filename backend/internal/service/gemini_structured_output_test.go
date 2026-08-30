package service

import (
	"context"
	"encoding/json"
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

const schemaProbeResponseFormat = `{
  "type":"json_schema",
  "json_schema":{
    "name":"SchemaProbe",
    "strict":true,
    "schema":{
      "type":"object",
      "additionalProperties":false,
      "properties":{
        "alphaNode":{"type":"string","enum":["x","y"]},
        "betaRef":{"type":["string","null"]},
        "gammaItems":{"type":"array","items":{"type":"string"}},
        "nestedNode":{
          "type":"object",
          "additionalProperties":false,
          "properties":{"deltaFlag":{"type":"boolean"}},
          "required":["deltaFlag"]
        }
      },
      "required":["alphaNode","betaRef","gammaItems","nestedNode"]
    }
  }
}`

func TestParseAndTranslateGeminiStrictJSONSchema(t *testing.T) {
	output, err := parseGeminiStructuredOutput(json.RawMessage(schemaProbeResponseFormat), false)
	require.NoError(t, err)
	require.NotNil(t, output)
	require.Equal(t, "SchemaProbe", output.Name)
	require.True(t, output.Strict)

	original := []byte(`{
      "systemInstruction":{"parts":[{"text":"system"}]},
      "contents":[{"role":"user","parts":[{"text":"hello"},{"inlineData":{"mimeType":"image/png","data":"abc"}}]}],
      "generationConfig":{"temperature":0.2}
    }`)
	translated, err := applyGeminiStructuredOutput(original, output)
	require.NoError(t, err)

	var request map[string]any
	require.NoError(t, json.Unmarshal(translated, &request))
	config := request["generationConfig"].(map[string]any)
	require.Equal(t, "application/json", config["responseMimeType"])
	schema := config["responseJsonSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	require.Contains(t, properties, "alphaNode")
	require.Contains(t, properties, "betaRef")
	require.Contains(t, properties, "gammaItems")
	require.Contains(t, properties, "nestedNode")
	require.Equal(t, []any{"alphaNode", "betaRef", "gammaItems", "nestedNode"}, schema["required"])
	require.Equal(t, []any{"x", "y"}, properties["alphaNode"].(map[string]any)["enum"])
	require.Equal(t, "string", properties["gammaItems"].(map[string]any)["items"].(map[string]any)["type"])
	require.Equal(t, []any{"string", "null"}, properties["betaRef"].(map[string]any)["type"])

	// Structured-output injection must not alter system, text, or multimodal parts.
	require.Equal(t, request["systemInstruction"], mustObject(t, original)["systemInstruction"])
	require.Equal(t, request["contents"], mustObject(t, original)["contents"])
	require.Equal(t, 0.2, config["temperature"])
}

func TestGeminiStrictJSONSchemaSurvivesActualUpstreamRequestBuilder(t *testing.T) {
	output, err := parseGeminiStructuredOutput(json.RawMessage(schemaProbeResponseFormat), false)
	require.NoError(t, err)
	translated, err := applyGeminiStructuredOutput([]byte(`{"contents":[{"role":"user","parts":[{"text":"probe"}]}]}`), output)
	require.NoError(t, err)

	upstream := httptest.NewServer(nil)
	defer upstream.Close()
	account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "must-not-leak", "base_url": upstream.URL}}
	service := &GeminiMessagesCompatService{cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}}
	build, _ := service.buildGeminiChatCompletionsUpstreamRequestFunc(account, "gemini-3.1-pro", translated, false, false)
	request, _, err := build(context.Background())
	require.NoError(t, err)
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.NotContains(t, string(body), "must-not-leak")

	config := mustObject(t, body)["generationConfig"].(map[string]any)
	require.Equal(t, "application/json", config["responseMimeType"])
	schema := config["responseJsonSchema"].(map[string]any)
	require.Equal(t, false, schema["additionalProperties"])
	require.Equal(t, []any{"alphaNode", "betaRef", "gammaItems", "nestedNode"}, schema["required"])
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"alphaNode", "betaRef", "gammaItems", "nestedNode"} {
		require.Contains(t, properties, name)
	}
	require.Equal(t, []any{"x", "y"}, properties["alphaNode"].(map[string]any)["enum"])
	require.Equal(t, []any{"string", "null"}, properties["betaRef"].(map[string]any)["type"])
	gammaItems := properties["gammaItems"].(map[string]any)
	require.Equal(t, "array", gammaItems["type"])
	require.Equal(t, "string", gammaItems["items"].(map[string]any)["type"])
	nested := properties["nestedNode"].(map[string]any)
	require.Equal(t, "object", nested["type"])
	require.Equal(t, false, nested["additionalProperties"])
	require.Equal(t, []any{"deltaFlag"}, nested["required"])
	require.Equal(t, "boolean", nested["properties"].(map[string]any)["deltaFlag"].(map[string]any)["type"])
}

func TestValidateJSONSchemaCombinatorSemantics(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		schema map[string]any
		valid  bool
	}{
		{
			name:  "oneOf exactly one branch matches",
			value: "alpha",
			schema: map[string]any{"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "number"},
			}},
			valid: true,
		},
		{
			name:  "oneOf two branches match",
			value: "alpha",
			schema: map[string]any{"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "string", "minLength": float64(1)},
			}},
		},
		{
			name:  "oneOf no branches match",
			value: true,
			schema: map[string]any{"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "number"},
			}},
		},
		{
			name:  "anyOf match still enforces sibling constraint",
			value: "x",
			schema: map[string]any{
				"anyOf":     []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}},
				"type":      "string",
				"minLength": float64(2),
			},
		},
		{
			name:  "oneOf match still enforces sibling constraint",
			value: "x",
			schema: map[string]any{
				"oneOf":     []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}},
				"type":      "string",
				"minLength": float64(2),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSONSchemaValue(tt.value, tt.schema, "$")
			if tt.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestGeminiStrictJSONSchemaRejectsSilentDegradation(t *testing.T) {
	t.Run("unsupported keyword", func(t *testing.T) {
		raw := strings.Replace(schemaProbeResponseFormat, `"type":"string","enum":["x","y"]`, `"type":"string","pattern":"secret-value"`, 1)
		output, err := parseGeminiStructuredOutput(json.RawMessage(raw), false)
		require.Nil(t, output)
		require.ErrorContains(t, err, "structured_output_not_supported")
		require.ErrorContains(t, err, "pattern")
		require.NotContains(t, err.Error(), "secret-value")
	})

	t.Run("strict streaming", func(t *testing.T) {
		output, err := parseGeminiStructuredOutput(json.RawMessage(schemaProbeResponseFormat), true)
		require.Nil(t, output)
		require.EqualError(t, err, "structured_output_not_supported: strict json_schema with stream=true")
	})

	t.Run("strict object openness", func(t *testing.T) {
		raw := strings.Replace(schemaProbeResponseFormat, `"additionalProperties":false,`, "", 1)
		output, err := parseGeminiStructuredOutput(json.RawMessage(raw), false)
		require.Nil(t, output)
		require.ErrorContains(t, err, "strict object requires additionalProperties=false")
	})
}

func TestGeminiStrictJSONSchemaHandlesCallerMetadataAndLocalLengthConstraints(t *testing.T) {
	raw := json.RawMessage(`{
      "type":"json_schema",
      "json_schema":{"name":"CallerShape","strict":true,"schema":{
        "$schema":"https://json-schema.org/draft/2020-12/schema",
        "type":"object","additionalProperties":false,
        "properties":{
          "label":{"type":"string","minLength":2,"maxLength":4},
          "kind":{"enum":["A","B"]},
          "nullable":{"anyOf":[{"type":"string","minLength":1},{"type":"null"}]}
        },
        "required":["label","kind","nullable"]
      }}
    }`)
	output, err := parseGeminiStructuredOutput(raw, false)
	require.NoError(t, err)
	translated, err := applyGeminiStructuredOutput([]byte(`{"contents":[]}`), output)
	require.NoError(t, err)
	schema := mustObject(t, translated)["generationConfig"].(map[string]any)["responseJsonSchema"].(map[string]any)
	require.NotContains(t, schema, "$schema")
	properties := schema["properties"].(map[string]any)
	require.NotContains(t, properties["label"].(map[string]any), "minLength")
	require.NotContains(t, properties["label"].(map[string]any), "maxLength")
	require.Equal(t, "string", properties["kind"].(map[string]any)["type"])

	require.NoError(t, validateStrictStructuredChatResponse(chatResponseWithText(t, `{"label":"good","kind":"A","nullable":null}`), output))
	require.ErrorContains(t, validateStrictStructuredChatResponse(chatResponseWithText(t, `{"label":"x","kind":"A","nullable":null}`), output), "minLength")
	require.ErrorContains(t, validateStrictStructuredChatResponse(chatResponseWithText(t, `{"label":"excess","kind":"A","nullable":null}`), output), "maxLength")
}

func TestValidateGeminiStrictStructuredResponse(t *testing.T) {
	output, err := parseGeminiStructuredOutput(json.RawMessage(schemaProbeResponseFormat), false)
	require.NoError(t, err)

	valid := chatResponseWithText(t, `{"alphaNode":"x","betaRef":null,"gammaItems":["g"],"nestedNode":{"deltaFlag":true}}`)
	require.NoError(t, validateStrictStructuredChatResponse(valid, output))

	tests := []struct {
		name string
		text string
		want string
	}{
		{"missing required", `{"alphaNode":"x","betaRef":null,"nestedNode":{"deltaFlag":true}}`, "gammaItems"},
		{"wrong enum", `{"alphaNode":"z","betaRef":null,"gammaItems":[],"nestedNode":{"deltaFlag":true}}`, "enum"},
		{"wrong nullable type", `{"alphaNode":"x","betaRef":3,"gammaItems":[],"nestedNode":{"deltaFlag":true}}`, "type mismatch"},
		{"additional property", `{"alphaNode":"x","betaRef":null,"gammaItems":[],"nestedNode":{"deltaFlag":true},"extra":true}`, "additional property"},
		{"invalid JSON", `{`, "not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStrictStructuredChatResponse(chatResponseWithText(t, tt.text), output)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestGeminiStrictResponseValidationIsWiredBeforeHTTP200(t *testing.T) {
	output, err := parseGeminiStructuredOutput(json.RawMessage(schemaProbeResponseFormat), false)
	require.NoError(t, err)
	service := &GeminiMessagesCompatService{cfg: &config.Config{}}

	t.Run("valid response passes", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		upstream := geminiHTTPResponse(`{"alphaNode":"x","betaRef":null,"gammaItems":[],"nestedNode":{"deltaFlag":true}}`)
		_, err := service.handleChatCompletionsNonStreamingResponseFromGemini(context, upstream, "gemini-3.1-pro", false, output)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, recorder.Code)
	})

	t.Run("invalid response is a safe gateway error", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		upstream := geminiHTTPResponse(`{"alphaNode":"x","betaRef":null,"gammaItems":[],"nestedNode":{"deltaFlag":true},"AUTHORIZATION_SECRET":"must-not-leak"}`)
		_, err := service.handleChatCompletionsNonStreamingResponseFromGemini(context, upstream, "gemini-3.1-pro", false, output)
		require.Error(t, err)
		require.Equal(t, http.StatusBadGateway, recorder.Code)
		require.Contains(t, recorder.Body.String(), "structured_output_validation_error")
		require.NotContains(t, recorder.Body.String(), "AUTHORIZATION_SECRET")
		require.NotContains(t, recorder.Body.String(), "must-not-leak")
	})
}

func TestGeminiRequestWithoutResponseFormatIsUnchanged(t *testing.T) {
	output, err := parseGeminiStructuredOutput(nil, false)
	require.NoError(t, err)
	require.Nil(t, output)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"normal"}]}]}`)
	translated, err := applyGeminiStructuredOutput(body, output)
	require.NoError(t, err)
	require.Equal(t, body, translated)
}

func chatResponseWithText(t *testing.T, text string) *apicompat.ChatCompletionsResponse {
	t.Helper()
	content, err := json.Marshal(text)
	require.NoError(t, err)
	return &apicompat.ChatCompletionsResponse{Choices: []apicompat.ChatChoice{{Message: apicompat.ChatMessage{Content: content}}}}
}

func mustObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal(raw, &value))
	return value
}

func geminiHTTPResponse(text string) *http.Response {
	body, _ := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"parts": []any{map[string]any{"text": text}}},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{"promptTokenCount": 1, "candidatesTokenCount": 1},
	})
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
}

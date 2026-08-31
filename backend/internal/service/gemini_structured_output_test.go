package service

import (
	"context"
	"encoding/json"
	"errors"
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
	config := mustMapValue(t, request["generationConfig"])
	require.Equal(t, "application/json", config["responseMimeType"])
	schema := mustMapValue(t, config["responseJsonSchema"])
	properties := mustMapValue(t, schema["properties"])
	require.Contains(t, properties, "alphaNode")
	require.Contains(t, properties, "betaRef")
	require.Contains(t, properties, "gammaItems")
	require.Contains(t, properties, "nestedNode")
	require.Equal(t, []any{"alphaNode", "betaRef", "gammaItems", "nestedNode"}, schema["required"])
	alphaNode := mustMapValue(t, properties["alphaNode"])
	require.Equal(t, []any{"x", "y"}, alphaNode["enum"])
	gammaItems := mustMapValue(t, properties["gammaItems"])
	itemSchema := mustMapValue(t, gammaItems["items"])
	require.Equal(t, "string", itemSchema["type"])
	require.Equal(t, []any{"string", "null"}, mustMapValue(t, properties["betaRef"])["type"])

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

	config := mustMapValue(t, mustObject(t, body)["generationConfig"])
	require.Equal(t, "application/json", config["responseMimeType"])
	schema := mustMapValue(t, config["responseJsonSchema"])
	require.Equal(t, false, schema["additionalProperties"])
	require.Equal(t, []any{"alphaNode", "betaRef", "gammaItems", "nestedNode"}, schema["required"])
	properties := mustMapValue(t, schema["properties"])
	for _, name := range []string{"alphaNode", "betaRef", "gammaItems", "nestedNode"} {
		require.Contains(t, properties, name)
	}
	require.Equal(t, []any{"x", "y"}, mustMapValue(t, properties["alphaNode"])["enum"])
	require.Equal(t, []any{"string", "null"}, mustMapValue(t, properties["betaRef"])["type"])
	gammaItems := mustMapValue(t, properties["gammaItems"])
	require.Equal(t, "array", gammaItems["type"])
	require.Equal(t, "string", mustMapValue(t, gammaItems["items"])["type"])
	nested := mustMapValue(t, properties["nestedNode"])
	require.Equal(t, "object", nested["type"])
	require.Equal(t, false, nested["additionalProperties"])
	require.Equal(t, []any{"deltaFlag"}, nested["required"])
	nestedProperties := mustMapValue(t, nested["properties"])
	require.Equal(t, "boolean", mustMapValue(t, nestedProperties["deltaFlag"])["type"])
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
	config := mustMapValue(t, mustObject(t, translated)["generationConfig"])
	schema := mustMapValue(t, config["responseJsonSchema"])
	require.NotContains(t, schema, "$schema")
	properties := mustMapValue(t, schema["properties"])
	label := mustMapValue(t, properties["label"])
	require.NotContains(t, label, "minLength")
	require.NotContains(t, label, "maxLength")
	require.Equal(t, "string", mustMapValue(t, properties["kind"])["type"])

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

func TestStrictJSONValidationDiagnosticsAreTypedAndValueFree(t *testing.T) {
	tests := []struct {
		name, path, keyword, expected, actualType, sentinel string
		value                                               any
		schema                                              map[string]any
	}{
		{"root type", "$", "type", "object", "string", "PRIVATE_MODEL_STRING_77AA", "PRIVATE_MODEL_STRING_77AA", map[string]any{"type": "object"}},
		{"required", "$", "required", "required property", "object", "", map[string]any{}, map[string]any{"type": "object", "required": []any{"requiredField"}}},
		{"enum", "$", "enum", "allowed enum member", "string", "SUPER_SECRET_ENUM_VALUE_9F21", "SUPER_SECRET_ENUM_VALUE_9F21", map[string]any{"type": "string", "enum": []any{"allowed"}}},
		{"additionalProperties", "$", "additionalProperties", "no additional properties", "object", "PRIVATE_VALUE_77AA", map[string]any{"extra": "PRIVATE_VALUE_77AA"}, map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{"nested type", "$.items[0].value", "type", "integer", "string", "PRIVATE_NESTED_77AA", map[string]any{"items": []any{map[string]any{"value": "PRIVATE_NESTED_77AA"}}}, map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "integer"}}}}}}},
		{"oneOf", "$", "oneOf", "exactly one schema", "string", "PRIVATE_ONE_OF_77AA", "PRIVATE_ONE_OF_77AA", map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "string", "minLength": float64(1)}}}},
		{"anyOf", "$", "anyOf", "at least one schema", "boolean", "", true, map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}},
		{"minLength", "$", "minLength", "minimum string length", "string", "PRIVATE_SHORT_77AA", "PRIVATE_SHORT_77AA", map[string]any{"type": "string", "minLength": float64(100)}},
		{"maxLength", "$", "maxLength", "maximum string length", "string", "PRIVATE_EXCESS_77AA", "PRIVATE_EXCESS_77AA", map[string]any{"type": "string", "maxLength": float64(2)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJSONSchemaValue(tt.value, tt.schema, "$")
			var diagnostic *StrictJSONValidationError
			require.ErrorAs(t, err, &diagnostic)
			require.Equal(t, tt.path, diagnostic.Path)
			require.Equal(t, tt.keyword, diagnostic.Keyword)
			require.Equal(t, tt.expected, diagnostic.Expected)
			require.Equal(t, tt.actualType, diagnostic.ActualType)
			require.Equal(t, "validation", diagnostic.Stage)
			metadata := strings.Join([]string{diagnostic.Path, diagnostic.Keyword, diagnostic.Expected, diagnostic.ActualType, diagnostic.Stage}, "\n")
			if tt.sentinel != "" {
				require.NotContains(t, diagnostic.Error(), tt.sentinel)
				require.NotContains(t, metadata, tt.sentinel)
			}
		})
	}
}

func TestStrictJSONValidationDiagnosticsCoverExtractionAndParsing(t *testing.T) {
	output := &geminiStructuredOutput{Strict: true, Schema: map[string]any{"type": "object"}}

	t.Run("content extraction", func(t *testing.T) {
		response := &apicompat.ChatCompletionsResponse{Choices: []apicompat.ChatChoice{{Message: apicompat.ChatMessage{Content: json.RawMessage(`{"private":"value"}`)}}}}
		err := validateStrictStructuredChatResponse(response, output)
		var diagnostic *StrictJSONValidationError
		require.ErrorAs(t, err, &diagnostic)
		require.Equal(t, "$.choices[0].message.content", diagnostic.Path)
		require.Equal(t, "type", diagnostic.Keyword)
		require.Equal(t, "object", diagnostic.ActualType)
		require.NotContains(t, diagnostic.Error(), "private")
		require.NotContains(t, diagnostic.Error(), "value")
	})

	t.Run("JSON parsing", func(t *testing.T) {
		err := validateStrictStructuredChatResponse(chatResponseWithText(t, "PRIVATE_INVALID_JSON"), output)
		var diagnostic *StrictJSONValidationError
		require.ErrorAs(t, err, &diagnostic)
		require.Equal(t, "$", diagnostic.Path)
		require.Equal(t, "parse", diagnostic.Keyword)
		require.Equal(t, "invalid_json", diagnostic.ActualType)
		require.Equal(t, "parsing", diagnostic.Stage)
		require.NotContains(t, diagnostic.Error(), "PRIVATE_INVALID_JSON")
	})
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

	t.Run("invalid response is deferred to the regeneration boundary", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		upstream := geminiHTTPResponse(`{"alphaNode":"x","betaRef":null,"gammaItems":[],"nestedNode":{"deltaFlag":true},"AUTHORIZATION_SECRET":"must-not-leak"}`)
		_, err := service.handleChatCompletionsNonStreamingResponseFromGemini(context, upstream, "gemini-3.1-pro", false, output)
		require.Error(t, err)
		require.Empty(t, recorder.Body.String())
		require.NotContains(t, recorder.Body.String(), "AUTHORIZATION_SECRET")
		require.NotContains(t, recorder.Body.String(), "must-not-leak")
	})
}

func TestGeminiStrictValidationDiagnosticStaysInternal(t *testing.T) {
	output, err := parseGeminiStructuredOutput(json.RawMessage(schemaProbeResponseFormat), false)
	require.NoError(t, err)
	service := &GeminiMessagesCompatService{cfg: &config.Config{}}
	const sentinel = "SUPER_SECRET_ENUM_VALUE_9F21"
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	upstream := geminiHTTPResponse(`{"alphaNode":"` + sentinel + `","betaRef":null,"gammaItems":[],"nestedNode":{"deltaFlag":true}}`)

	_, err = service.handleChatCompletionsNonStreamingResponseFromGemini(context, upstream, "gemini-3.1-pro", false, output)

	require.EqualError(t, err, "upstream response did not satisfy requested strict JSON schema")
	var diagnostic *StrictJSONValidationError
	require.True(t, errors.As(err, &diagnostic))
	require.Equal(t, "$.alphaNode", diagnostic.Path)
	require.Equal(t, "enum", diagnostic.Keyword)
	require.NotContains(t, diagnostic.Error(), sentinel)
	require.Empty(t, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), sentinel)
	require.NotContains(t, recorder.Body.String(), diagnostic.Path)
	require.NotContains(t, recorder.Body.String(), diagnostic.Keyword)
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

func mustMapValue(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	require.True(t, ok)
	return result
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

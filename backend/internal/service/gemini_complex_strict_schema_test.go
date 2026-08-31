package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func genericComplexStrictSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"header": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"label":    map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(600)},
					"category": map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(600)},
				},
				"required": []any{"label", "category"},
			},
			"optionalReference": nullableConstrainedString(),
			"summary":           constrainedString(),
			"optionalNote":      nullableConstrainedString(),
			"settings": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"mode":  constrainedString(),
					"label": constrainedString(),
				},
				"required": []any{"mode", "label"},
			},
			"references": map[string]any{
				"type":     "array",
				"maxItems": float64(30),
				"items":    constrainedString(),
			},
			"action": constrainedString(),
			"presentation": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"format":         map[string]any{"enum": []any{"compact", "expanded", "tabular", "graphical", "none"}},
					"purpose":        constrainedString(),
					"optionalDetail": nullableConstrainedString(),
				},
				"required": []any{"format", "purpose", "optionalDetail"},
			},
		},
		"required": []any{"header", "optionalReference", "summary", "optionalNote", "settings", "references", "action", "presentation"},
	}
}

func constrainedString() map[string]any {
	return map[string]any{"type": "string", "minLength": float64(1), "maxLength": float64(600)}
}

func nullableConstrainedString() map[string]any {
	return map[string]any{"anyOf": []any{constrainedString(), map[string]any{"type": "null"}}}
}

func validGenericComplexDocument() map[string]any {
	return map[string]any{
		"header":            map[string]any{"label": "A useful label", "category": "general"},
		"optionalReference": "reference-1",
		"summary":           "concise summary",
		"optionalNote":      "optional context",
		"settings":          map[string]any{"mode": "standard", "label": "default"},
		"references":        []any{"reference-1", "reference-2"},
		"action":            "continue",
		"presentation": map[string]any{
			"format": "compact", "purpose": "display the result", "optionalDetail": nil,
		},
	}
}

func genericComplexOutput() *geminiStructuredOutput {
	return &geminiStructuredOutput{Name: "ComplexDocument", Strict: true, Schema: genericComplexStrictSchema()}
}

func genericComplexResponse(t *testing.T, value any) *apicompat.ChatCompletionsResponse {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return chatResponseWithText(t, string(raw))
}

func requireGenericComplexDiagnostic(t *testing.T, err error, path, keyword, expected, actualType, stage string) {
	t.Helper()
	require.Error(t, err)
	var diagnostic *StrictJSONValidationError
	require.True(t, errors.As(err, &diagnostic))
	require.Equal(t, path, diagnostic.Path)
	require.Equal(t, keyword, diagnostic.Keyword)
	require.Equal(t, expected, diagnostic.Expected)
	require.Equal(t, actualType, diagnostic.ActualType)
	require.Equal(t, stage, diagnostic.Stage)
}

func TestGeminiGenericComplexStrictSchemaTransformationIsNarrow(t *testing.T) {
	original := genericComplexStrictSchema()
	responseFormat, err := json.Marshal(map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name": "ComplexDocument", "strict": true, "schema": original,
		},
	})
	require.NoError(t, err)
	parsed, err := parseGeminiStructuredOutput(responseFormat, false)
	require.NoError(t, err)
	require.True(t, parsed.Strict)
	require.Equal(t, original, parsed.Schema)

	transformed, ok := geminiUpstreamJSONSchema(original).(map[string]any)
	require.True(t, ok)

	removed := make([]string, 0)
	added := make([]string, 0)
	compareGenericComplexSchema(t, "$", original, transformed, &removed, &added)
	require.ElementsMatch(t, []string{
		"$.properties.header.properties.label.maxLength", "$.properties.header.properties.label.minLength",
		"$.properties.header.properties.category.maxLength", "$.properties.header.properties.category.minLength",
		"$.properties.optionalReference.anyOf[0].maxLength", "$.properties.optionalReference.anyOf[0].minLength",
		"$.properties.summary.maxLength", "$.properties.summary.minLength",
		"$.properties.optionalNote.anyOf[0].maxLength", "$.properties.optionalNote.anyOf[0].minLength",
		"$.properties.settings.properties.mode.maxLength", "$.properties.settings.properties.mode.minLength",
		"$.properties.settings.properties.label.maxLength", "$.properties.settings.properties.label.minLength",
		"$.properties.references.items.maxLength", "$.properties.references.items.minLength",
		"$.properties.action.maxLength", "$.properties.action.minLength",
		"$.properties.presentation.properties.purpose.maxLength", "$.properties.presentation.properties.purpose.minLength",
		"$.properties.presentation.properties.optionalDetail.anyOf[0].maxLength", "$.properties.presentation.properties.optionalDetail.anyOf[0].minLength",
	}, removed)
	require.Equal(t, []string{"$.properties.presentation.properties.format.type"}, added)

	format := schemaAt(t, transformed, "properties", "presentation", "properties", "format")
	require.Equal(t, "string", format["type"])
	require.Equal(t, []any{"compact", "expanded", "tabular", "graphical", "none"}, format["enum"])
	require.Contains(t, schemaAt(t, transformed, "properties", "optionalNote"), "anyOf")
	require.Contains(t, schemaAt(t, transformed, "properties", "optionalReference"), "anyOf")
	require.Equal(t, float64(30), schemaAt(t, transformed, "properties", "references")["maxItems"])

	body, err := applyGeminiStructuredOutput([]byte(`{"contents":[]}`), parsed)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	config, ok := request["generationConfig"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "application/json", config["responseMimeType"])
	require.Equal(t, transformed, config["responseJsonSchema"])
}

func compareGenericComplexSchema(t *testing.T, path string, original, transformed any, removed, added *[]string) {
	t.Helper()
	switch source := original.(type) {
	case map[string]any:
		target, ok := transformed.(map[string]any)
		require.True(t, ok, path)
		for key, sourceValue := range source {
			targetValue, present := target[key]
			if !present {
				require.Contains(t, []string{"minLength", "maxLength", "$schema"}, key, path+"."+key)
				*removed = append(*removed, path+"."+key)
				continue
			}
			compareGenericComplexSchema(t, path+"."+key, sourceValue, targetValue, removed, added)
		}
		for key := range target {
			if _, present := source[key]; !present {
				require.Equal(t, "type", key, path+"."+key)
				require.Equal(t, "string", target[key], path+"."+key)
				*added = append(*added, path+"."+key)
			}
		}
	case []any:
		target, ok := transformed.([]any)
		require.True(t, ok, path)
		require.Len(t, target, len(source), path)
		for i := range source {
			compareGenericComplexSchema(t, path+"["+string(rune('0'+i))+"]", source[i], target[i], removed, added)
		}
	default:
		require.Equal(t, original, transformed, path)
	}
}

func schemaAt(t *testing.T, root map[string]any, keys ...string) map[string]any {
	t.Helper()
	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		require.True(t, ok, key)
		current = next
	}
	return current
}

func TestGeminiGenericComplexStrictSchemaValidationMatrix(t *testing.T) {
	output := genericComplexOutput()
	t.Run("valid object", func(t *testing.T) {
		require.NoError(t, validateStrictStructuredChatResponse(genericComplexResponse(t, validGenericComplexDocument()), output))
	})
	t.Run("empty nonnullable nested label", func(t *testing.T) {
		value := validGenericComplexDocument()
		settings, ok := value["settings"].(map[string]any)
		require.True(t, ok)
		settings["label"] = ""
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output), "$.settings.label", "minLength", "minimum string length", "string", "validation")
	})
	t.Run("empty nullable note", func(t *testing.T) {
		value := validGenericComplexDocument()
		value["optionalNote"] = ""
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output), "$.optionalNote", "anyOf", "at least one schema", "string", "validation")
	})
	t.Run("nullable null", func(t *testing.T) {
		value := validGenericComplexDocument()
		value["optionalNote"] = nil
		require.NoError(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output))
	})
	t.Run("normal string maxLength", func(t *testing.T) {
		value := validGenericComplexDocument()
		value["summary"] = strings.Repeat("x", 601)
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output), "$.summary", "maxLength", "maximum string length", "string", "validation")
	})
	t.Run("nullable branch maxLength", func(t *testing.T) {
		value := validGenericComplexDocument()
		value["optionalNote"] = strings.Repeat("x", 601)
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output), "$.optionalNote", "anyOf", "at least one schema", "string", "validation")
	})
	t.Run("references maxItems", func(t *testing.T) {
		value := validGenericComplexDocument()
		claims := make([]any, 31)
		for i := range claims {
			claims[i] = "claim"
		}
		value["references"] = claims
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output), "$.references", "maxItems", "maximum array length", "array", "validation")
	})
	t.Run("invalid format enum is value free", func(t *testing.T) {
		const sentinel = "PRIVATE_INVALID_ENUM_KIND"
		value := validGenericComplexDocument()
		presentation, ok := value["presentation"].(map[string]any)
		require.True(t, ok)
		presentation["format"] = sentinel
		err := validateStrictStructuredChatResponse(genericComplexResponse(t, value), output)
		requireGenericComplexDiagnostic(t, err, "$.presentation.format", "enum", "allowed enum member", "string", "validation")
		require.NotContains(t, err.Error(), sentinel)
	})
	t.Run("missing required", func(t *testing.T) {
		value := validGenericComplexDocument()
		delete(value, "action")
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output), "$", "required", "required property", "object", "validation")
	})
	t.Run("additional property", func(t *testing.T) {
		value := validGenericComplexDocument()
		value["privateUnexpected"] = "must not leak"
		err := validateStrictStructuredChatResponse(genericComplexResponse(t, value), output)
		requireGenericComplexDiagnostic(t, err, "$", "additionalProperties", "no additional properties", "object", "validation")
		require.NotContains(t, err.Error(), "privateUnexpected")
		require.NotContains(t, err.Error(), "must not leak")
	})
	t.Run("valid string array", func(t *testing.T) {
		value := validGenericComplexDocument()
		value["references"] = []any{"a", "b", "c"}
		require.NoError(t, validateStrictStructuredChatResponse(genericComplexResponse(t, value), output))
	})
	t.Run("malformed assistant JSON", func(t *testing.T) {
		requireGenericComplexDiagnostic(t, validateStrictStructuredChatResponse(chatResponseWithText(t, `{"header":`), output), "$", "parse", "valid JSON", "invalid_json", "parsing")
	})
	t.Run("invalid content envelope", func(t *testing.T) {
		response := &apicompat.ChatCompletionsResponse{Choices: []apicompat.ChatChoice{{Message: apicompat.ChatMessage{Content: json.RawMessage(`{"private":"value"}`)}}}}
		err := validateStrictStructuredChatResponse(response, output)
		requireGenericComplexDiagnostic(t, err, "$.choices[0].message.content", "type", "string", "object", "extraction")
		require.NotContains(t, err.Error(), "private")
		require.NotContains(t, err.Error(), "value")
	})
}

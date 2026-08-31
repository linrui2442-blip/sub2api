package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func applyGeminiSyntheticStrictTransport(body []byte, output *geminiStructuredOutput) ([]byte, error) {
	if output == nil || !output.Strict {
		return body, nil
	}
	projected, ok := geminiUpstreamJSONSchema(output.Schema).(map[string]any)
	if !ok {
		return nil, errors.New("strict structured output schema projection must be an object")
	}
	declaration := antigravity.GeminiToolDeclaration{FunctionDeclarations: []antigravity.GeminiFunctionDecl{{
		Name:        syntheticStrictJSONFunctionName,
		Description: "Return the requested structured result.",
		ParametersJSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": projected,
			},
			"required":             []string{"value"},
			"additionalProperties": false,
		},
	}}}
	toolConfig := antigravity.GeminiToolConfig{FunctionCallingConfig: &antigravity.GeminiFunctionCallingConfig{
		Mode:                 "ANY",
		AllowedFunctionNames: []string{syntheticStrictJSONFunctionName},
	}}

	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("parse Gemini request: %w", err)
	}
	toolJSON, err := json.Marshal(declaration)
	if err != nil {
		return nil, err
	}
	var toolValue map[string]any
	if err := json.Unmarshal(toolJSON, &toolValue); err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(toolConfig)
	if err != nil {
		return nil, err
	}
	var configValue map[string]any
	if err := json.Unmarshal(configJSON, &configValue); err != nil {
		return nil, err
	}
	request["tools"] = []any{toolValue}
	request["toolConfig"] = configValue
	if generationConfig, ok := request["generationConfig"].(map[string]any); ok {
		delete(generationConfig, "responseMimeType")
		delete(generationConfig, "responseJsonSchema")
	}
	return json.Marshal(request)
}

func extractGeminiSyntheticStrictValue(response *apicompat.AnthropicResponse) (any, error) {
	if response == nil {
		return nil, strictJSONValidationFailure("$", "synthetic_function", "exactly one function call", "missing", "synthetic_extraction", "synthetic structured response is missing")
	}
	calls := make([]apicompat.AnthropicContentBlock, 0, 1)
	for _, block := range response.Content {
		if block.Type == "tool_use" {
			calls = append(calls, block)
		}
	}
	if len(calls) != 1 {
		return nil, strictJSONValidationFailure("$", "synthetic_function", "exactly one function call", "ambiguous", "synthetic_extraction", "synthetic structured response must contain exactly one function call")
	}
	call := calls[0]
	if call.Name != syntheticStrictJSONFunctionName {
		return nil, strictJSONValidationFailure("$", "synthetic_function", "internal structured-output function", "wrong_function", "synthetic_extraction", "synthetic structured response called an unexpected function")
	}
	if len(call.Input) == 0 || string(call.Input) == "null" {
		return nil, strictJSONValidationFailure("$.args", "required", "function arguments", "missing", "synthetic_extraction", "synthetic structured response is missing function arguments")
	}
	var args map[string]any
	if err := json.Unmarshal(call.Input, &args); err != nil || args == nil {
		return nil, strictJSONValidationFailure("$.args", "type", "object", "invalid", "synthetic_extraction", "synthetic structured response has invalid function arguments")
	}
	value, exists := args["value"]
	if !exists {
		return nil, strictJSONValidationFailure("$.args.value", "required", "transport value", "missing", "synthetic_extraction", "synthetic structured response is missing transport value")
	}
	return value, nil
}

func syntheticStrictChatResponse(response *apicompat.AnthropicResponse, originalModel string, output *geminiStructuredOutput) (*apicompat.ChatCompletionsResponse, error) {
	value, err := extractGeminiSyntheticStrictValue(response)
	if err != nil {
		return nil, err
	}
	if err := validateJSONSchemaValue(value, output.Schema, "$"); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, strictJSONValidationFailure("$", "serialization", "JSON-serializable value", jsonValueType(value), "synthetic_extraction", "synthetic structured value could not be serialized")
	}
	clean := *response
	clean.Content = []apicompat.AnthropicContentBlock{{Type: "text", Text: string(encoded)}}
	clean.StopReason = apicompat.AnthropicStopReasonPtr("end_turn")
	responsesResponse := apicompat.AnthropicToResponsesResponse(&clean)
	return apicompat.ResponsesToChatCompletions(responsesResponse, originalModel), nil
}

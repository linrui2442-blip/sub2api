package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

type geminiStructuredOutput struct {
	Name   string
	Strict bool
	Schema map[string]any
}

// StrictJSONValidationError carries value-free diagnostics for an internal
// strict structured-output failure. It must never contain response values.
type StrictJSONValidationError struct {
	Path       string
	Keyword    string
	Expected   string
	ActualType string
	Stage      string
}

const maxStrictStructuredOutputGenerations = 2

const (
	strictJSONParsingCorrectiveInstruction    = "The previous generation did not produce syntactically valid JSON. Return exactly one JSON value that conforms to the provided JSON Schema. Return JSON only. Do not include prose, explanations, Markdown, or code fences."
	strictJSONValidationCorrectiveInstruction = "The previous generation did not satisfy the requested strict JSON Schema. Regenerate the response as exactly one JSON value that fully conforms to the provided schema. Return JSON only. Do not include prose, explanations, Markdown, or code fences."
)

type strictStructuredOutputAttemptError struct {
	diagnostic error
	usage      ClaudeUsage
	retryable  bool
}

func (e *strictStructuredOutputAttemptError) Error() string {
	return "upstream response did not satisfy requested strict JSON schema"
}

func (e *strictStructuredOutputAttemptError) Unwrap() error { return e.diagnostic }

func newStrictStructuredOutputAttemptError(validationErr error, response *apicompat.ChatCompletionsResponse, usage ClaudeUsage) *strictStructuredOutputAttemptError {
	var diagnostic *StrictJSONValidationError
	retryable := errors.As(validationErr, &diagnostic) &&
		(diagnostic.Stage == "parsing" || diagnostic.Stage == "validation")
	if response != nil && len(response.Choices) > 0 && response.Choices[0].FinishReason == "length" {
		retryable = false
	}
	return &strictStructuredOutputAttemptError{diagnostic: validationErr, usage: usage, retryable: retryable}
}

func addClaudeUsage(total *ClaudeUsage, usage ClaudeUsage) {
	if total == nil {
		return
	}
	total.InputTokens += usage.InputTokens
	total.OutputTokens += usage.OutputTokens
	total.CacheCreationInputTokens += usage.CacheCreationInputTokens
	total.CacheReadInputTokens += usage.CacheReadInputTokens
	total.CacheCreation5mTokens += usage.CacheCreation5mTokens
	total.CacheCreation1hTokens += usage.CacheCreation1hTokens
	total.ImageOutputTokens += usage.ImageOutputTokens
}

func strictJSONCorrectiveInstruction(validationErr error) (string, bool) {
	var diagnostic *StrictJSONValidationError
	if !errors.As(validationErr, &diagnostic) {
		return "", false
	}
	switch diagnostic.Stage {
	case "parsing":
		return strictJSONParsingCorrectiveInstruction, true
	case "validation":
		return strictJSONValidationCorrectiveInstruction, true
	default:
		return "", false
	}
}

func applyGeminiStrictCorrectiveInstruction(body []byte, validationErr error) ([]byte, error) {
	instruction, ok := strictJSONCorrectiveInstruction(validationErr)
	if !ok {
		return body, nil
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	target := root
	if request, ok := root["request"].(map[string]any); ok {
		target = request
	}

	systemInstruction, _ := target["systemInstruction"].(map[string]any)
	if systemInstruction == nil {
		systemInstruction = make(map[string]any)
		target["systemInstruction"] = systemInstruction
	}
	parts, _ := systemInstruction["parts"].([]any)
	systemInstruction["parts"] = append(parts, map[string]any{"text": instruction})
	return json.Marshal(root)
}

func (e *StrictJSONValidationError) Error() string {
	if e == nil {
		return "strict JSON validation failed"
	}
	return fmt.Sprintf("strict JSON validation failed at %s: keyword=%s expected=%s actual_type=%s stage=%s", e.Path, e.Keyword, e.Expected, e.ActualType, e.Stage)
}

// strictJSONSanitizedError preserves a typed diagnostic for errors.As while
// keeping Error() identical to the generic public error. This prevents normal
// handler logging from persisting diagnostic metadata.
type strictJSONSanitizedError struct {
	message    string
	diagnostic *StrictJSONValidationError
}

func (e *strictJSONSanitizedError) Error() string { return e.message }
func (e *strictJSONSanitizedError) Unwrap() error { return e.diagnostic }

func preserveStrictJSONValidationDiagnostic(publicErr, validationErr error) error {
	var diagnostic *StrictJSONValidationError
	if publicErr == nil || !errors.As(validationErr, &diagnostic) {
		return publicErr
	}
	return &strictJSONSanitizedError{message: publicErr.Error(), diagnostic: diagnostic}
}

func strictJSONValidationFailure(path, keyword, expected, actualType, stage, format string, args ...any) error {
	diagnostic := &StrictJSONValidationError{Path: path, Keyword: keyword, Expected: expected, ActualType: actualType, Stage: stage}
	return fmt.Errorf(format+": %w", append(args, diagnostic)...)
}

func jsonValueType(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		if typed == math.Trunc(typed) {
			return "integer"
		}
		return "number"
	case bool:
		return "boolean"
	default:
		return "unknown"
	}
}

func rawJSONType(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "invalid_json"
	}
	return jsonValueType(value)
}

func declaredJSONType(declaration any) string {
	if name, ok := declaration.(string); ok {
		return name
	}
	if names, ok := declaration.([]any); ok {
		result := make([]string, 0, len(names))
		for _, rawName := range names {
			if name, ok := rawName.(string); ok {
				result = append(result, name)
			}
		}
		if len(result) > 0 {
			return strings.Join(result, " or ")
		}
	}
	return "declared JSON type"
}

var supportedGeminiJSONSchemaKeywords = map[string]struct{}{
	"$schema": {},
	"type":    {}, "title": {}, "description": {}, "enum": {},
	"items": {}, "minItems": {}, "maxItems": {}, "minimum": {}, "maximum": {},
	"minLength": {}, "maxLength": {},
	"anyOf": {}, "oneOf": {}, "properties": {}, "additionalProperties": {}, "required": {},
}

func parseGeminiStructuredOutput(raw json.RawMessage, stream bool) (*geminiStructuredOutput, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var format struct {
		Type       string `json:"type"`
		JSONSchema *struct {
			Name   string         `json:"name"`
			Strict *bool          `json:"strict"`
			Schema map[string]any `json:"schema"`
		} `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &format); err != nil {
		return nil, errors.New("invalid response_format")
	}
	if format.Type != "json_schema" {
		return nil, nil
	}
	if format.JSONSchema == nil || strings.TrimSpace(format.JSONSchema.Name) == "" || format.JSONSchema.Schema == nil {
		return nil, errors.New("json_schema name and schema are required")
	}
	strict := format.JSONSchema.Strict != nil && *format.JSONSchema.Strict
	if strict && stream {
		return nil, errors.New("structured_output_not_supported: strict json_schema with stream=true")
	}
	if err := validateGeminiJSONSchemaSupport(format.JSONSchema.Schema, "$", strict); err != nil {
		return nil, fmt.Errorf("structured_output_not_supported: %w", err)
	}
	return &geminiStructuredOutput{Name: strings.TrimSpace(format.JSONSchema.Name), Strict: strict, Schema: format.JSONSchema.Schema}, nil
}

func validateGeminiJSONSchemaSupport(schema map[string]any, path string, strict bool) error {
	for key := range schema {
		if _, ok := supportedGeminiJSONSchemaKeywords[key]; !ok {
			return fmt.Errorf("unsupported schema keyword %q at %s", key, path)
		}
	}
	if declaredType, ok := schema["type"]; ok {
		if err := validateSchemaTypeDeclaration(declaredType, path); err != nil {
			return err
		}
	} else if _, hasEnum := schema["enum"]; !hasEnum {
		if _, hasAnyOf := schema["anyOf"]; !hasAnyOf {
			if _, hasOneOf := schema["oneOf"]; !hasOneOf {
				return fmt.Errorf("schema type is required at %s", path)
			}
		}
	}
	if rawProperties, present := schema["properties"]; present {
		properties, ok := rawProperties.(map[string]any)
		if !ok {
			return fmt.Errorf("properties must be an object at %s", path)
		}
		for name, child := range properties {
			childSchema, ok := child.(map[string]any)
			if !ok {
				return fmt.Errorf("property schema must be an object at %s.properties.%s", path, name)
			}
			if err := validateGeminiJSONSchemaSupport(childSchema, path+".properties."+name, strict); err != nil {
				return err
			}
		}
	}
	if rawRequired, present := schema["required"]; present {
		required, ok := rawRequired.([]any)
		if !ok {
			return fmt.Errorf("required must be an array at %s", path)
		}
		for _, rawName := range required {
			if _, ok := rawName.(string); !ok {
				return fmt.Errorf("required entries must be strings at %s", path)
			}
		}
	}
	if rawEnum, present := schema["enum"]; present {
		values, ok := rawEnum.([]any)
		if !ok || len(values) == 0 {
			return fmt.Errorf("enum must be a non-empty array at %s", path)
		}
	}
	if item, ok := schema["items"]; ok {
		itemSchema, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("items must be a schema object at %s", path)
		}
		if err := validateGeminiJSONSchemaSupport(itemSchema, path+".items", strict); err != nil {
			return err
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		if rawAlternatives, present := schema[keyword]; present {
			alternatives, ok := rawAlternatives.([]any)
			if !ok {
				return fmt.Errorf("%s must be an array at %s", keyword, path)
			}
			if len(alternatives) == 0 {
				return fmt.Errorf("%s must not be empty at %s", keyword, path)
			}
			for i, alternative := range alternatives {
				alternativeSchema, ok := alternative.(map[string]any)
				if !ok {
					return fmt.Errorf("%s entry must be a schema object at %s", keyword, path)
				}
				if err := validateGeminiJSONSchemaSupport(alternativeSchema, fmt.Sprintf("%s.%s[%d]", path, keyword, i), strict); err != nil {
					return err
				}
			}
		}
	}
	if additional, present := schema["additionalProperties"]; present {
		if _, ok := additional.(bool); !ok {
			return fmt.Errorf("additionalProperties must be boolean at %s", path)
		}
	}
	if strict {
		if objectTypeIncludes(schema["type"]) {
			additional, present := schema["additionalProperties"]
			if !present || additional != false {
				return fmt.Errorf("strict object requires additionalProperties=false at %s", path)
			}
		}
	}
	return nil
}

func validateSchemaTypeDeclaration(value any, path string) error {
	valid := func(s string) bool {
		switch s {
		case "object", "array", "string", "number", "integer", "boolean", "null":
			return true
		default:
			return false
		}
	}
	switch typed := value.(type) {
	case string:
		if valid(typed) {
			return nil
		}
	case []any:
		if len(typed) > 0 {
			for _, item := range typed {
				name, ok := item.(string)
				if !ok || !valid(name) {
					return fmt.Errorf("unsupported schema type at %s", path)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("unsupported schema type at %s", path)
}

func objectTypeIncludes(value any) bool {
	if value == "object" {
		return true
	}
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if item == "object" {
				return true
			}
		}
	}
	return false
}

func applyGeminiStructuredOutput(body []byte, output *geminiStructuredOutput) ([]byte, error) {
	if output == nil {
		return body, nil
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("parse Gemini request: %w", err)
	}
	config, _ := request["generationConfig"].(map[string]any)
	if config == nil {
		config = make(map[string]any)
		request["generationConfig"] = config
	}
	config["responseMimeType"] = "application/json"
	config["responseJsonSchema"] = geminiUpstreamJSONSchema(output.Schema)
	return json.Marshal(request)
}

// geminiUpstreamJSONSchema keeps the Gemini-supported structural contract exact.
// $schema is metadata, while minLength/maxLength are enforced by SUB2's strict
// response validator because Gemini responseJsonSchema does not advertise them.
func geminiUpstreamJSONSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "$schema" || key == "minLength" || key == "maxLength" {
				continue
			}
			cleaned[key] = geminiUpstreamJSONSchema(child)
		}
		if _, hasType := cleaned["type"]; !hasType {
			if enum, ok := cleaned["enum"].([]any); ok && allStrings(enum) {
				cleaned["type"] = "string"
			}
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(typed))
		for i, child := range typed {
			cleaned[i] = geminiUpstreamJSONSchema(child)
		}
		return cleaned
	default:
		return value
	}
}

func allStrings(values []any) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if _, ok := value.(string); !ok {
			return false
		}
	}
	return true
}

func validateStrictStructuredChatResponse(response *apicompat.ChatCompletionsResponse, output *geminiStructuredOutput) error {
	if output == nil || !output.Strict {
		return nil
	}
	if response == nil || len(response.Choices) == 0 {
		return strictJSONValidationFailure("$", "response", "at least one response choice", "missing", "extraction", "structured response has no choices")
	}
	for index, choice := range response.Choices {
		contentPath := fmt.Sprintf("$.choices[%d].message.content", index)
		var text string
		if err := json.Unmarshal(choice.Message.Content, &text); err != nil {
			return strictJSONValidationFailure(contentPath, "type", "string", rawJSONType(choice.Message.Content), "extraction", "choice %d content is not text", index)
		}
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return strictJSONValidationFailure("$", "parse", "valid JSON", "invalid_json", "parsing", "choice %d content is not valid JSON", index)
		}
		if err := validateJSONSchemaValue(value, output.Schema, "$" /* never include output values */); err != nil {
			return err
		}
	}
	return nil
}

func validateJSONSchemaValue(value any, schema map[string]any, path string) error {
	if alternatives, ok := schema["anyOf"].([]any); ok {
		if countMatchingSchemas(value, alternatives, path) == 0 {
			return strictJSONValidationFailure(path, "anyOf", "at least one schema", jsonValueType(value), "validation", "value does not match anyOf at %s", path)
		}
	}
	if alternatives, ok := schema["oneOf"].([]any); ok {
		if matches := countMatchingSchemas(value, alternatives, path); matches != 1 {
			return strictJSONValidationFailure(path, "oneOf", "exactly one schema", jsonValueType(value), "validation", "value matches %d oneOf schemas at %s; expected exactly one", matches, path)
		}
	}
	if enum, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			if reflect.DeepEqual(value, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return strictJSONValidationFailure(path, "enum", "allowed enum member", jsonValueType(value), "validation", "value is not in enum at %s", path)
		}
	}
	if declaredType, present := schema["type"]; present && !matchesDeclaredType(value, declaredType) {
		return strictJSONValidationFailure(path, "type", declaredJSONType(declaredType), jsonValueType(value), "validation", "type mismatch at %s", path)
	}
	switch typed := value.(type) {
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, rawName := range required {
				name, ok := rawName.(string)
				if !ok {
					return strictJSONValidationFailure(path, "required", "valid required declaration", "schema", "validation", "invalid required declaration at %s", path)
				}
				if _, present := typed[name]; !present {
					return strictJSONValidationFailure(path, "required", "required property", "object", "validation", "required property %s is missing at %s", name, path)
				}
			}
		}
		for name, childValue := range typed {
			child, known := properties[name]
			if !known {
				if schema["additionalProperties"] == false {
					return strictJSONValidationFailure(path, "additionalProperties", "no additional properties", "object", "validation", "additional property is not allowed at %s", path)
				}
				continue
			}
			childSchema, ok := child.(map[string]any)
			if !ok {
				return strictJSONValidationFailure(path+"."+name, "schema", "valid property schema", "schema", "validation", "invalid property schema at %s", path)
			}
			if err := validateJSONSchemaValue(childValue, childSchema, path+"."+name); err != nil {
				return err
			}
		}
	case []any:
		if minimum, ok := schema["minItems"].(float64); ok && len(typed) < int(minimum) {
			return strictJSONValidationFailure(path, "minItems", "minimum array length", "array", "validation", "array is shorter than minItems at %s", path)
		}
		if maximum, ok := schema["maxItems"].(float64); ok && len(typed) > int(maximum) {
			return strictJSONValidationFailure(path, "maxItems", "maximum array length", "array", "validation", "array is longer than maxItems at %s", path)
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range typed {
				if err := validateJSONSchemaValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case string:
		length := utf8.RuneCountInString(typed)
		if minimum, ok := schema["minLength"].(float64); ok && length < int(minimum) {
			return strictJSONValidationFailure(path, "minLength", "minimum string length", "string", "validation", "string is shorter than minLength at %s", path)
		}
		if maximum, ok := schema["maxLength"].(float64); ok && length > int(maximum) {
			return strictJSONValidationFailure(path, "maxLength", "maximum string length", "string", "validation", "string is longer than maxLength at %s", path)
		}
	case float64:
		if minimum, ok := schema["minimum"].(float64); ok && typed < minimum {
			return strictJSONValidationFailure(path, "minimum", "minimum numeric value", jsonValueType(typed), "validation", "number is below minimum at %s", path)
		}
		if maximum, ok := schema["maximum"].(float64); ok && typed > maximum {
			return strictJSONValidationFailure(path, "maximum", "maximum numeric value", jsonValueType(typed), "validation", "number is above maximum at %s", path)
		}
	}
	return nil
}

func countMatchingSchemas(value any, alternatives []any, path string) int {
	matches := 0
	for _, raw := range alternatives {
		if schema, ok := raw.(map[string]any); ok && validateJSONSchemaValue(value, schema, path) == nil {
			matches++
		}
	}
	return matches
}

func matchesDeclaredType(value any, declaration any) bool {
	matches := func(name string) bool {
		switch name {
		case "null":
			return value == nil
		case "object":
			_, ok := value.(map[string]any)
			return ok
		case "array":
			_, ok := value.([]any)
			return ok
		case "string":
			_, ok := value.(string)
			return ok
		case "number":
			_, ok := value.(float64)
			return ok
		case "integer":
			n, ok := value.(float64)
			return ok && n == math.Trunc(n)
		case "boolean":
			_, ok := value.(bool)
			return ok
		default:
			return false
		}
	}
	if name, ok := declaration.(string); ok {
		return matches(name)
	}
	if names, ok := declaration.([]any); ok {
		for _, rawName := range names {
			if name, ok := rawName.(string); ok && matches(name) {
				return true
			}
		}
	}
	return false
}

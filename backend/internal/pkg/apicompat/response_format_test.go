package apicompat

import (
	"encoding/json"
	"testing"
)

func convertTestSchema(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "x", "schema": schema}})
	var out map[string]json.RawMessage
	if err := json.Unmarshal(chatResponseFormatToResponsesTextFormat(b), &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	_ = json.Unmarshal(out["schema"], &result)
	return result
}

func TestResponseFormatConstTypeInference(t *testing.T) {
	cases := []struct {
		name  string
		value any
		typ   string
	}{
		{"integer", float64(1), "integer"}, {"string", "x", "string"}, {"boolean", true, "boolean"},
		{"number", 1.5, "number"}, {"null", nil, "null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := convertTestSchema(t, map[string]any{"const": tc.value})
			if got["type"] != tc.typ || got["const"] != tc.value {
				t.Fatalf("got %#v", got)
			}
		})
	}
	got := convertTestSchema(t, map[string]any{"type": "number", "const": 1})
	if got["type"] != "number" {
		t.Fatalf("explicit type changed: %#v", got)
	}
}

func TestResponseFormatConstTypeInferenceRecursive(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"schemaVersion": map[string]any{"const": 1}}, "items": map[string]any{"const": true}, "anyOf": []any{map[string]any{"const": "x"}}, "oneOf": []any{map[string]any{"const": 1.5}}, "allOf": []any{map[string]any{"const": nil}}, "prefixItems": []any{map[string]any{"const": false}}, "required": []string{"schemaVersion"}, "additionalProperties": false}
	got := convertTestSchema(t, schema)
	t.Logf("normalized=%#v", got)
	props := got["properties"].(map[string]any)
	if props["schemaVersion"].(map[string]any)["type"] != "integer" {
		t.Fatal("properties not normalized")
	}
	if got["items"].(map[string]any)["type"] != "boolean" || got["anyOf"].([]any)[0].(map[string]any)["type"] != "string" || got["oneOf"].([]any)[0].(map[string]any)["type"] != "number" || got["allOf"].([]any)[0].(map[string]any)["type"] != "null" || got["prefixItems"].([]any)[0].(map[string]any)["type"] != "boolean" {
		t.Fatal("recursive normalization incomplete")
	}
}

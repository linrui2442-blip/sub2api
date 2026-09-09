package service

import "testing"

func TestGeminiStructuredOutputAdapterConstValues(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		value    any
	}{
		{"integer-1", "integer", float64(1)},
		{"integer-1.0", "integer", float64(1)},
		{"integer-1.5", "integer", float64(1.5)},
		{"number-1.5", "number", float64(1.5)},
		{"string", "string", "x"},
		{"string-number", "string", float64(1)},
		{"boolean", "boolean", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (geminiStructuredOutputAdapter{}).prepare(map[string]any{"type": tt.typeName, "const": tt.value}, false)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if _, ok := got["const"]; ok {
				t.Fatal("const was not removed")
			}
			enum, ok := got["enum"].([]any)
			if !ok || len(enum) != 1 {
				t.Fatalf("enum = %#v, want one value", got["enum"])
			}
			if enum[0] != tt.value {
				t.Fatalf("enum value = %#v, want %#v", enum[0], tt.value)
			}
		})
	}
}

func TestStructuredOutputAdapterProviderIsolation(t *testing.T) {
	base := map[string]any{"type": "string", "const": "v1"}
	for _, tc := range []struct {
		provider  structuredOutputProvider
		wantConst bool
		wantEnum  bool
	}{
		{structuredOutputProviderGemini, false, true},
		{structuredOutputProviderAntigravity, false, true},
		{structuredOutputProviderOpenAI, true, false},
		{structuredOutputProviderAnthropic, true, false},
	} {
		got, err := resolveStructuredOutputAdapter(tc.provider).prepare(base, false)
		if err != nil {
			t.Fatalf("%s: %v", tc.provider, err)
		}
		_, hasConst := got["const"]
		_, hasEnum := got["enum"]
		if hasConst != tc.wantConst || hasEnum != tc.wantEnum {
			t.Fatalf("%s: const=%v enum=%v", tc.provider, hasConst, hasEnum)
		}
	}
}

func TestGeminiStructuredOutputAdapterRecursiveConst(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"name":  map[string]any{"type": "string", "const": "x"},
		"items": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "const": float64(1)}},
	}}
	got, err := (geminiStructuredOutputAdapter{}).prepare(schema, false)
	if err != nil {
		t.Fatal(err)
	}
	props := got["properties"].(map[string]any)
	if _, ok := props["name"].(map[string]any)["const"]; ok {
		t.Fatal("const was not removed")
	}
	if _, ok := props["name"].(map[string]any)["enum"]; !ok {
		t.Fatal("nested const was not normalized")
	}
}

func TestGeminiStructuredOutputAdapterRejectsNullConst(t *testing.T) {
	_, err := (geminiStructuredOutputAdapter{}).prepare(map[string]any{"type": "null", "const": nil}, false)
	if err == nil {
		t.Fatal("expected null const rejection")
	}
}

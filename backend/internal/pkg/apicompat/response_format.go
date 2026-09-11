package apicompat

import "encoding/json"

func chatResponseFormatToResponsesTextFormat(raw json.RawMessage) json.RawMessage {
	raw = normalizedRawJSON(raw)
	if len(raw) == 0 {
		return nil
	}

	obj, ok := rawJSONObject(raw)
	if !ok || rawString(obj["type"]) != "json_schema" {
		return raw
	}

	schemaRaw := normalizedRawJSON(obj["json_schema"])
	if len(schemaRaw) == 0 {
		return raw
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return raw
	}
	schema["type"] = rawJSONString("json_schema")
	if nested, ok := rawJSONObject(schema["schema"]); ok {
		normalizeConstTypes(nested)
		data, _ := json.Marshal(nested)
		schema["schema"] = data
	}

	out, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return out
}

func normalizeConstTypes(schema map[string]json.RawMessage) {
	if _, hasType := schema["type"]; !hasType {
		if v, ok := schema["const"]; ok {
			var x any
			if json.Unmarshal(v, &x) == nil {
				switch n := x.(type) {
				case nil:
					schema["type"] = rawJSONString("null")
				case bool:
					schema["type"] = rawJSONString("boolean")
				case string:
					schema["type"] = rawJSONString("string")
				case float64:
					if n == float64(int64(n)) {
						schema["type"] = rawJSONString("integer")
					} else {
						schema["type"] = rawJSONString("number")
					}
				}
			}
		}
	}
	for _, key := range []string{"properties", "items", "anyOf", "oneOf", "allOf", "prefixItems"} {
		if key == "properties" {
			var props map[string]json.RawMessage
			if json.Unmarshal(schema[key], &props) == nil {
				for k, raw := range props {
					var child map[string]json.RawMessage
					if json.Unmarshal(raw, &child) == nil {
						normalizeConstTypes(child)
						props[k], _ = json.Marshal(child)
					}
				}
				schema[key], _ = json.Marshal(props)
				continue
			}
		}
		var v any
		if json.Unmarshal(schema[key], &v) != nil {
			continue
		}
		var walk func(any)
		walk = func(x any) {
			switch n := x.(type) {
			case map[string]any:
				for k, c := range n {
					walk(c)
					_ = k
				}
			case []any:
				for _, c := range n {
					walk(c)
				}
			}
		}
		walk(v)
		var normalize func(any) any
		normalize = func(x any) any {
			switch n := x.(type) {
			case map[string]any:
				b, _ := json.Marshal(n)
				var m map[string]json.RawMessage
				_ = json.Unmarshal(b, &m)
				normalizeConstTypes(m)
				nb, _ := json.Marshal(m)
				var out any
				_ = json.Unmarshal(nb, &out)
				return out
			case []any:
				for i, c := range n {
					n[i] = normalize(c)
				}
				return n
			}
			return x
		}
		if b, err := json.Marshal(normalize(v)); err == nil {
			schema[key] = b
		}
	}
}

func responsesTextFormatToChatResponseFormat(raw json.RawMessage) json.RawMessage {
	raw = normalizedRawJSON(raw)
	if len(raw) == 0 {
		return nil
	}

	obj, ok := rawJSONObject(raw)
	if !ok || rawString(obj["type"]) != "json_schema" {
		return raw
	}
	if _, alreadyChatShape := obj["json_schema"]; alreadyChatShape {
		return raw
	}

	schema := make(map[string]json.RawMessage, len(obj))
	for key, value := range obj {
		if key == "type" {
			continue
		}
		schema[key] = value
	}
	if len(schema) == 0 {
		return raw
	}

	schemaRaw, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	out, err := json.Marshal(map[string]json.RawMessage{
		"type":        rawJSONString("json_schema"),
		"json_schema": schemaRaw,
	})
	if err != nil {
		return raw
	}
	return out
}

func normalizedRawJSON(raw json.RawMessage) json.RawMessage {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func rawJSONObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func rawJSONString(value string) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

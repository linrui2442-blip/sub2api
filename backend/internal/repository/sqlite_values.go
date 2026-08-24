package repository

import (
	"encoding/json"
	"fmt"
)

// sqliteJSONList serializes slices for SQLite's json_each table-valued function.
// Repository callers only pass slices of scalar values, for which Marshal cannot fail.
func sqliteJSONList(values any) string {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

type sqliteInt64Slice []int64

func (s *sqliteInt64Slice) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}
	var raw []byte
	switch value := src.(type) {
	case string:
		raw = []byte(value)
	case []byte:
		raw = value
	default:
		return fmt.Errorf("scan integer list from %T", src)
	}
	if len(raw) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(raw, s)
}

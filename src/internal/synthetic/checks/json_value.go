package checks

import (
	"encoding/json"
	"strings"
)

// ExtractJSONPath resolves a dot-separated path (e.g. "data.account.balance")
// into a float64 from a decoded JSON object. Returns false when any segment is
// missing, the intermediate value is not a map, or the leaf is not numeric.
// Mirrors the Python json_value_checker.py behaviour exactly.
func ExtractJSONPath(data map[string]any, path string) (float64, bool) {
	parts := strings.Split(path, ".")
	var current any = data
	for _, p := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return 0, false
		}
		current, ok = m[p]
		if !ok {
			return 0, false
		}
	}
	switch v := current.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

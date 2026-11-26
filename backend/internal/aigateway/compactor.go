package aigateway

import (
	"encoding/json"
	"unicode/utf8"
)

// CompactToolJSON trims oversized JSON tool responses structurally so the LLM always
// receives valid JSON instead of a broken mid-string truncation.
func CompactToolJSON(raw string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 5500
	}
	if len(raw) <= maxBytes {
		return raw
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return safeTruncateUTF8(raw, maxBytes)
	}
	compacted := shrinkValue(parsed, 5, 240)
	out, err := json.Marshal(compacted)
	if err != nil {
		return safeTruncateUTF8(raw, maxBytes)
	}
	if len(out) <= maxBytes {
		return string(out)
	}
	compacted = shrinkValue(parsed, 2, 120)
	out, err = json.Marshal(compacted)
	if err == nil && len(out) <= maxBytes {
		return string(out)
	}
	// Final valid JSON envelope fallback.
	preview, _ := json.Marshal(map[string]any{
		"truncated": true,
		"orig_len":  len(raw),
		"preview":   safeTruncateUTF8(raw, max(256, maxBytes-128)),
	})
	return string(preview)
}

func shrinkValue(v any, maxSlice, maxStr int) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = shrinkValue(val, maxSlice, maxStr)
		}
		return out
	case []any:
		if len(x) <= maxSlice {
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = shrinkValue(item, maxSlice, maxStr)
			}
			return out
		}
		out := make([]any, 0, maxSlice+1)
		for i := 0; i < maxSlice; i++ {
			out = append(out, shrinkValue(x[i], maxSlice, maxStr))
		}
		out = append(out, map[string]any{"_omitted_items": len(x) - maxSlice})
		return out
	case string:
		if utf8.RuneCountInString(x) > maxStr {
			return safeTruncateUTF8(x, maxStr*3) + "..."
		}
		return x
	default:
		return v
	}
}

func safeTruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

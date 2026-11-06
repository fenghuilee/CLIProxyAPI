package aigc

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SanitizeProviderResponse strips or replaces large base64 payloads (e.g. b64_json, Data URIs)
// from upstream provider responses before storing them into database logs, preventing storage bloat
// while preserving all metadata, token usage metrics, revised prompts, and error structures.
func SanitizeProviderResponse(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		// Non-JSON payload: if abnormally large, truncate safely
		if len(raw) > 4096 {
			truncated := append(raw[:4096], []byte(fmt.Sprintf("... [truncated %d bytes]", len(raw)-4096))...)
			return truncated
		}
		return raw
	}

	sanitized := sanitizeNode(root)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return raw
	}
	return out
}

func sanitizeNode(node any) any {
	switch v := node.(type) {
	case map[string]any:
		res := make(map[string]any, len(v))
		for k, val := range v {
			lowerK := strings.ToLower(k)
			if lowerK == "b64_json" || lowerK == "base64" || lowerK == "image_base64" || lowerK == "image_b64" || lowerK == "file_base64" {
				if s, ok := val.(string); ok && len(s) > 64 {
					res[k] = fmt.Sprintf("[base64 omitted; length=%d bytes, stored in TOS/artifacts]", len(s))
					continue
				}
			}
			res[k] = sanitizeNode(val)
		}
		return res
	case []any:
		res := make([]any, len(v))
		for i, val := range v {
			res[i] = sanitizeNode(val)
		}
		return res
	case string:
		if strings.HasPrefix(v, "data:image/") || strings.HasPrefix(v, "data:video/") || strings.HasPrefix(v, "data:audio/") {
			if idx := strings.Index(v, ";base64,"); idx != -1 && len(v) > 128 {
				return fmt.Sprintf("[data URI omitted; length=%d bytes, stored in TOS/artifacts]", len(v))
			}
		}
		return v
	default:
		return node
	}
}

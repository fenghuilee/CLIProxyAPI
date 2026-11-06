package aigc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeProviderResponse(t *testing.T) {
	t.Run("OpenAI b64_json stripping", func(t *testing.T) {
		largeBase64 := strings.Repeat("A", 50000)
		raw := map[string]any{
			"created": 1787566677,
			"data": []any{
				map[string]any{
					"b64_json":       largeBase64,
					"revised_prompt": "A cute cat on Mars",
				},
			},
			"usage": map[string]any{
				"input_tokens":  82,
				"output_tokens": 158,
				"total_tokens":  240,
			},
		}
		rawBytes, err := json.Marshal(raw)
		if err != nil {
			t.Fatalf("failed to marshal raw input: %v", err)
		}

		sanitized := SanitizeProviderResponse(rawBytes)
		if strings.Contains(string(sanitized), largeBase64) {
			t.Fatalf("sanitized output still contains large base64 payload")
		}

		var parsed map[string]any
		if err := json.Unmarshal(sanitized, &parsed); err != nil {
			t.Fatalf("failed to parse sanitized output: %v", err)
		}

		dataList, ok := parsed["data"].([]any)
		if !ok || len(dataList) != 1 {
			t.Fatalf("expected data array with 1 item, got %v", parsed["data"])
		}

		item0 := dataList[0].(map[string]any)
		b64Val := item0["b64_json"].(string)
		if !strings.HasPrefix(b64Val, "[base64 omitted; length=50000 bytes") {
			t.Fatalf("unexpected b64_json replacement: %s", b64Val)
		}
		if item0["revised_prompt"] != "A cute cat on Mars" {
			t.Fatalf("revised_prompt was corrupted: %v", item0["revised_prompt"])
		}

		usage := parsed["usage"].(map[string]any)
		if usage["total_tokens"].(float64) != 240 {
			t.Fatalf("usage tokens corrupted: %v", usage)
		}
	})

	t.Run("Data URI stripping in string", func(t *testing.T) {
		largeDataURI := "data:image/png;base64," + strings.Repeat("B", 1000)
		raw := map[string]any{
			"status": "succeeded",
			"image":  largeDataURI,
		}
		rawBytes, _ := json.Marshal(raw)
		sanitized := SanitizeProviderResponse(rawBytes)

		if strings.Contains(string(sanitized), strings.Repeat("B", 100)) {
			t.Fatalf("sanitized output still contains data URI payload")
		}
	})

	t.Run("Nil and empty payload", func(t *testing.T) {
		if len(SanitizeProviderResponse(nil)) != 0 {
			t.Fatalf("expected nil/empty for nil input")
		}
		if len(SanitizeProviderResponse([]byte(""))) != 0 {
			t.Fatalf("expected empty for empty input")
		}
	})

	t.Run("Normal JSON with URLs preserved", func(t *testing.T) {
		raw := `{"status":"succeeded","url":"https://example.com/output.png","created":12345}`
		sanitized := SanitizeProviderResponse([]byte(raw))
		var parsed map[string]any
		if err := json.Unmarshal(sanitized, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if parsed["url"] != "https://example.com/output.png" {
			t.Fatalf("url was modified: %v", parsed["url"])
		}
	})
}

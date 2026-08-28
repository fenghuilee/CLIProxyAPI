package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

func safeJSONRawMessage(s any) json.RawMessage {
	if s == nil {
		return nil
	}
	var str string
	switch v := s.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		b, err := json.Marshal(v)
		if err == nil {
			return json.RawMessage(b)
		}
		return nil
	}

	trimmed := strings.TrimSpace(str)
	if trimmed == "" {
		return nil
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	// Try base64 decoding
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		if json.Valid(decoded) {
			return json.RawMessage(decoded)
		}
	}
	// Fallback as valid JSON string
	if b, err := json.Marshal(trimmed); err == nil {
		return json.RawMessage(b)
	}
	return nil
}

func parseTime(v any) time.Time {
	if v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		// Try standard layouts
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05.999",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
		}
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

func parseTimePtr(v any) *time.Time {
	if v == nil {
		return nil
	}
	t := parseTime(v)
	if t.IsZero() {
		return nil
	}
	return &t
}

func parseUint64(v any) uint64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case uint64:
		return n
	case int64:
		return uint64(n)
	case int:
		return uint64(n)
	case float64:
		return uint64(n)
	case float32:
		return uint64(n)
	}
	return 0
}

func parseInt(v any) int {
	return int(parseInt64(v))
}

func parseInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	}
	return 0
}

func parseString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprint(v)
	}
}

func rowToGeneration(row map[string]any) aigc.ContentGeneration {
	var metadata map[string]any
	if metaRaw := row["metadata"]; metaRaw != nil {
		if metaStr := parseString(metaRaw); metaStr != "" {
			_ = json.Unmarshal([]byte(metaStr), &metadata)
		}
	}

	return aigc.ContentGeneration{
		ID:               parseString(row["generation_id"]),
		RequestID:        parseString(row["request_id"]),
		Kind:             aigc.ContentKind(parseString(row["kind"])),
		Model:            parseString(row["model"]),
		Status:           aigc.GenerationStatus(parseString(row["status"])),
		Stage:            aigc.GenerationStage(parseString(row["stage"])),
		Progress:         parseInt(row["progress"]),
		Revision:         parseUint64(row["revision"]),
		APIKey:           parseString(row["api_key"]),
		ClientIP:         parseString(row["client_ip"]),
		Input:            safeJSONRawMessage(row["input"]),
		PreparedInput:    safeJSONRawMessage(row["prepared_input"]),
		ProviderRequest:  safeJSONRawMessage(row["provider_request"]),
		ProviderResponse: safeJSONRawMessage(row["provider_response"]),
		Output:           safeJSONRawMessage(row["output"]),
		Provider:         parseString(row["provider"]),
		ProviderTaskID:   parseString(row["provider_task_id"]),
		AuthID:           parseString(row["auth_id"]),
		ErrorCode:        parseString(row["error_code"]),
		ErrorMessage:     parseString(row["error_message"]),
		Metadata:         metadata,
		BillingUsage:     safeJSONRawMessage(row["billing_usage"]),
		WorkerID:         parseString(row["worker_id"]),
		LeaseUntil:       parseTimePtr(row["lease_until"]),
		CreatedAt:        parseTime(row["created_at"]),
		UpdatedAt:        parseTime(row["updated_at"]),
		ExpiresAt:        parseTimePtr(row["expires_at"]),
	}
}

func rowToArtifact(row map[string]any) aigc.ContentGenerationArtifact {
	var metadata map[string]any
	if metaRaw := row["metadata"]; metaRaw != nil {
		if metaStr := parseString(metaRaw); metaStr != "" {
			_ = json.Unmarshal([]byte(metaStr), &metadata)
		}
	}

	return aigc.ContentGenerationArtifact{
		ID:              parseUint64(row["id"]),
		GenerationID:    parseString(row["generation_id"]),
		ArtifactType:    parseString(row["artifact_type"]),
		StorageProvider: parseString(row["storage_provider"]),
		URI:             parseString(row["uri"]),
		MIMEType:        parseString(row["mime_type"]),
		SizeBytes:       parseInt64(row["size_bytes"]),
		SHA256:          parseString(row["sha256"]),
		Metadata:        metadata,
		CreatedAt:       parseTime(row["created_at"]),
		ExpiresAt:       parseTimePtr(row["expires_at"]),
	}
}

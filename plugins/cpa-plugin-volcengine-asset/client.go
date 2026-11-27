package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// AssetItem is a channel-neutral registered media asset.
type AssetItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	FileType     string `json:"file_type"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// AssetAdapter keeps protocol and authentication out of the shared lifecycle.
type AssetAdapter interface {
	CreateAsset(ctx context.Context, name, fileType, url string) (AssetItem, error)
	GetAsset(ctx context.Context, assetID string) (AssetItem, error)
}

// AssetAPIError contains HTTP status, error classification code, and optional diagnostic message.
type AssetAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *AssetAPIError) Error() string {
	if e.Message != "" {
		if e.Code != "" {
			return fmt.Sprintf("asset API error (HTTP %d, code %s: %s)", e.Status, e.Code, e.Message)
		}
		return fmt.Sprintf("asset API error (HTTP %d: %s)", e.Status, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("asset API error (HTTP %d, code %s)", e.Status, e.Code)
	}
	return fmt.Sprintf("asset API error (HTTP %d)", e.Status)
}

func (e *AssetAPIError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

// parseUpstreamError extracts structured error details from non-200 or platform error responses.
func parseUpstreamError(statusCode int, raw []byte, defaultCode string, secrets ...string) *AssetAPIError {
	code := defaultCode
	var message string

	if len(raw) > 0 {
		if ojson.GetBytes(raw, "ResponseMetadata.Error").Exists() {
			errNode := ojson.GetBytes(raw, "ResponseMetadata.Error")
			if errNode.Type == ojson.String {
				message = errNode.String()
			} else {
				if c := errNode.Get("Code").String(); c != "" {
					code = c
				}
				if m := errNode.Get("Message").String(); m != "" {
					message = m
				}
			}
		}
		if message == "" && code == defaultCode && ojson.GetBytes(raw, "error").Exists() {
			errNode := ojson.GetBytes(raw, "error")
			if errNode.Type == ojson.String {
				message = errNode.String()
			} else {
				if c := errNode.Get("code").String(); c != "" {
					code = c
				}
				if m := errNode.Get("message").String(); m != "" {
					message = m
				}
			}
		}
		if message == "" && code == defaultCode && ojson.GetBytes(raw, "message").Exists() {
			message = ojson.GetBytes(raw, "message").String()
			if c := ojson.GetBytes(raw, "code").String(); c != "" && c != "0" {
				code = c
			}
		}
		if message == "" && code == defaultCode && ojson.GetBytes(raw, "ErrorMessage").Exists() {
			message = ojson.GetBytes(raw, "ErrorMessage").String()
		}
		if message == "" && code == defaultCode {
			clean := strings.TrimSpace(string(raw))
			if len(clean) > 0 && len(clean) <= 256 && !strings.Contains(clean, "<html") && !strings.Contains(clean, "<!DOCTYPE") {
				if !ojson.Valid(clean) {
					message = clean
				}
			}
		}
	}

	code = sanitizeErrorText(code, secrets...)
	message = sanitizeErrorText(message, secrets...)

	return &AssetAPIError{
		Status:  statusCode,
		Code:    code,
		Message: message,
	}
}

// sanitizeErrorText strips tokens, keys, and internal URLs from diagnostic error messages.
func sanitizeErrorText(text string, secrets ...string) string {
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if len(s) > 3 {
			text = strings.ReplaceAll(text, s, "[REDACTED]")
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			if u, err := url.Parse(s); err == nil && len(u.Host) > 3 {
				text = strings.ReplaceAll(text, u.Host, "[REDACTED]")
				text = strings.ReplaceAll(text, u.String(), "[REDACTED]")
			}
		}
	}
	return text
}

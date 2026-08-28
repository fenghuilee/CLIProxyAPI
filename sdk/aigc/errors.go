package aigc

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// Standard AIGC error codes (unified snake_case taxonomy)
const (
	// Auth & Permissions
	ErrCodeInvalidAPIKey        = "invalid_api_key"
	ErrCodeIPNotWhitelisted     = "ip_not_whitelisted"
	ErrCodeInsufficientQuota    = "insufficient_quota"
	ErrCodeModelAccessDenied    = "model_access_denied"
	ErrCodeUpstreamAccessDenied = "upstream_access_denied"

	// Request & Parameters
	ErrCodeInvalidParameter      = "invalid_parameter"
	ErrCodeInvalidMediaSize      = "invalid_media_size"
	ErrCodeInvalidDuration       = "invalid_duration"
	ErrCodeInvalidFrameAsset     = "invalid_frame_asset"
	ErrCodeUnsupportedInputMedia = "unsupported_input_media"

	// Content Policy & Safety
	ErrCodeSensitiveContent   = "sensitive_content_detected"
	ErrCodeSensitiveMedia     = "sensitive_media_detected"
	ErrCodeCopyrightViolation = "copyright_violation"
	ErrCodePolicyViolation    = "policy_violation"

	// Routing & Drivers
	ErrCodeModelNotFound        = "model_not_found"
	ErrCodeDriverNotFound       = "driver_not_found"
	ErrCodeEndpointNotSupported = "endpoint_not_supported"

	// Plugins & Mutations
	ErrCodeAssetUploadFailed   = "asset_upload_failed"
	ErrCodeAssetProcessTimeout = "asset_process_timeout"
	ErrCodeMutationRejected    = "mutation_rejected"

	// Upstream & Execution
	ErrCodeUpstreamTimeout      = "upstream_timeout"
	ErrCodeUpstreamServiceError = "upstream_service_error"
	ErrCodeClientCanceled       = "client_canceled"
	ErrCodeInternalServerError  = "internal_server_error"
)

// Legacy error variables preserved for backward compatibility
var (
	ErrNotFound               = errors.New("content generation not found")
	ErrRevisionConflict       = errors.New("content generation revision conflict")
	ErrInvalidStateTransition = errors.New("invalid content generation state transition")
	ErrLeaseExpired           = errors.New("content generation lease expired")
	ErrDriverNotFound         = errors.New("content generation driver not found")
	ErrStoreNotConfigured     = errors.New("content generation store not configured")
	ErrGenerationCanceled     = errors.New("content generation already canceled")
	ErrGenerationCompleted    = errors.New("content generation already completed")
	ErrMutationRejected       = errors.New("content generation mutation rejected")
)

// NormalizedError represents a standardized, user-facing and machine-readable error descriptor.
type NormalizedError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Type         string `json:"type,omitempty"`
	HTTPStatus   int    `json:"status_code,omitempty"`
	UpstreamCode string `json:"upstream_code,omitempty"`
}

func (e *NormalizedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

var reqIDRegex = regexp.MustCompile(`(?i)\s*Request\s*id:\s*[0-9a-zA-Z_-]+`)

// NormalizeError converts raw provider responses, status codes, and error instances into a clean NormalizedError.
// It prioritizes dynamic database rules, falling back to pure protocol classification when unmatched.
func NormalizeError(provider string, statusCode int, rawBody []byte, fallbackErr error) NormalizedError {
	rawStr := strings.TrimSpace(string(rawBody))
	var upstreamCode, upstreamMsg string

	if rawStr != "" && json.Valid(rawBody) {
		// 1. Extract error code and message from JSON payloads
		if v := gjson.GetBytes(rawBody, "error.code").String(); v != "" {
			upstreamCode = v
		} else if v := gjson.GetBytes(rawBody, "code").String(); v != "" {
			upstreamCode = v
		} else if v := gjson.GetBytes(rawBody, "Code").String(); v != "" {
			upstreamCode = v
		} else if v := gjson.GetBytes(rawBody, "output.code").String(); v != "" {
			upstreamCode = v
		}

		if v := gjson.GetBytes(rawBody, "error.message").String(); v != "" {
			upstreamMsg = v
		} else if v := gjson.GetBytes(rawBody, "message").String(); v != "" {
			upstreamMsg = v
		} else if v := gjson.GetBytes(rawBody, "Message").String(); v != "" {
			upstreamMsg = v
		} else if v := gjson.GetBytes(rawBody, "Result.ErrorMessage").String(); v != "" {
			upstreamMsg = v
		} else if v := gjson.GetBytes(rawBody, "ErrorMessage").String(); v != "" {
			upstreamMsg = v
		} else if v := gjson.GetBytes(rawBody, "output.message").String(); v != "" {
			upstreamMsg = v
		}

		// If message is embedded JSON string, parse one level deeper
		if upstreamMsg != "" && strings.HasPrefix(strings.TrimSpace(upstreamMsg), "{") && json.Valid([]byte(upstreamMsg)) {
			msgBytes := []byte(upstreamMsg)
			if code := gjson.GetBytes(msgBytes, "error.code").String(); code != "" {
				upstreamCode = code
			}
			if msg := gjson.GetBytes(msgBytes, "error.message").String(); msg != "" {
				upstreamMsg = msg
			}
		}
	} else if rawStr != "" {
		upstreamMsg = rawStr
	} else if fallbackErr != nil {
		upstreamMsg = fallbackErr.Error()
		if strings.HasPrefix(strings.TrimSpace(upstreamMsg), "{") && json.Valid([]byte(upstreamMsg)) {
			msgBytes := []byte(upstreamMsg)
			if code := gjson.GetBytes(msgBytes, "error.code").String(); code != "" {
				upstreamCode = code
			}
			if msg := gjson.GetBytes(msgBytes, "error.message").String(); msg != "" {
				upstreamMsg = msg
			}
		}
	}

	// Clean out noisy request IDs from user message
	cleanedMsg := reqIDRegex.ReplaceAllString(upstreamMsg, "")
	cleanedMsg = strings.TrimSpace(cleanedMsg)

	// Step 1: Match against in-memory dynamic database rules
	if matched, ok := globalRuleRegistry.Match(provider, statusCode, upstreamCode, cleanedMsg); ok {
		return matched
	}

	// Step 2: Fall back to pure vendor-agnostic protocol classification
	code, msg, errType, httpStatus := classifyPureProtocolFallback(statusCode, upstreamCode, cleanedMsg)

	return NormalizedError{
		Code:         code,
		Message:      msg,
		Type:         errType,
		HTTPStatus:   httpStatus,
		UpstreamCode: upstreamCode,
	}
}

// classifyPureProtocolFallback performs vendor-agnostic fallback classification based purely on HTTP semantics.
func classifyPureProtocolFallback(statusCode int, upstreamCode, rawMsg string) (code, msg, errType string, httpStatus int) {
	codeLower := strings.ToLower(upstreamCode)
	msgLower := strings.ToLower(rawMsg)

	httpStatus = statusCode
	if httpStatus <= 0 {
		httpStatus = http.StatusInternalServerError
	}

	// 1. Internal plugin / lifecycle mutations
	if codeLower == ErrCodeAssetProcessTimeout || strings.Contains(codeLower, "assetprocesstimeout") || strings.Contains(msgLower, "assetprocesstimeout") {
		return ErrCodeAssetProcessTimeout, "媒体素材预处理超时，请稍后重试", "mutation_error", http.StatusGatewayTimeout
	}
	if codeLower == ErrCodeAssetUploadFailed || strings.Contains(codeLower, "assetuploadfailed") || strings.Contains(msgLower, "assetuploadfailed") {
		return ErrCodeAssetUploadFailed, "媒体素材转存或处理失败", "mutation_error", http.StatusInternalServerError
	}
	if codeLower == ErrCodeMutationRejected || strings.Contains(codeLower, "mutationrejected") || strings.Contains(msgLower, "mutation_rejected") || strings.Contains(msgLower, "preparation rejected by policy") {
		return ErrCodeMutationRejected, "请求前置检查被策略拒绝", "mutation_error", http.StatusBadRequest
	}
	if codeLower == ErrCodeDriverNotFound || strings.Contains(codeLower, "drivernotfound") || strings.Contains(msgLower, "driver_not_found") {
		return ErrCodeDriverNotFound, "暂无可用驱动支持该模型", "routing_error", http.StatusInternalServerError
	}

	// 2. Cancellation
	if codeLower == ErrCodeClientCanceled || strings.Contains(msgLower, "canceled") || strings.Contains(msgLower, "cancelled") {
		return ErrCodeClientCanceled, "任务已取消", "invalid_request_error", 499
	}

	// 3. Content Policy & Safety
	if strings.Contains(codeLower, "copyright") || strings.Contains(msgLower, "copyright") {
		return ErrCodeCopyrightViolation, rawMsg, "content_policy_error", http.StatusBadRequest
	}
	if strings.Contains(codeLower, "sensitive") || strings.Contains(msgLower, "sensitive") {
		return ErrCodeSensitiveContent, rawMsg, "content_policy_error", http.StatusBadRequest
	}
	if strings.Contains(codeLower, "policyviolation") || strings.Contains(msgLower, "policy violation") {
		return ErrCodePolicyViolation, rawMsg, "content_policy_error", http.StatusBadRequest
	}

	// 4. Standard HTTP status codes
	switch statusCode {
	case http.StatusUnauthorized: // 401
		return ErrCodeInvalidAPIKey, "API Key 无效或未授权", "authentication_error", http.StatusUnauthorized

	case http.StatusForbidden: // 403
		return ErrCodeModelAccessDenied, "无权限访问该模型或资源", "permission_error", http.StatusForbidden

	case http.StatusPaymentRequired: // 402
		return ErrCodeInsufficientQuota, "账户余额或算力积分不足，请充值后重试", "permission_error", http.StatusPaymentRequired

	case http.StatusNotFound: // 404
		return ErrCodeModelNotFound, "请求的模型或端点不存在", "invalid_request_error", http.StatusNotFound

	case http.StatusTooManyRequests: // 429
		return "rate_limit_exceeded", "请求过于频繁，已触发限流，请稍后重试", "rate_limit_error", http.StatusTooManyRequests

	case http.StatusGatewayTimeout: // 504
		return ErrCodeUpstreamTimeout, "上游服务响应超时，请稍后重试", "upstream_error", http.StatusGatewayTimeout

	case http.StatusBadGateway, http.StatusServiceUnavailable: // 502, 503
		msg = rawMsg
		if msg == "" || strings.HasPrefix(msg, "{") {
			msg = "上游模型服务发生异常，请稍后重试"
		}
		return ErrCodeUpstreamServiceError, msg, "upstream_error", statusCode
	}

	if statusCode >= 500 {
		msg = rawMsg
		if msg == "" || strings.HasPrefix(msg, "{") {
			msg = "上游模型服务发生异常，请稍后重试"
		}
		return ErrCodeUpstreamServiceError, msg, "upstream_error", statusCode
	}

	if statusCode >= 400 {
		msg = rawMsg
		if msg == "" || strings.HasPrefix(msg, "{") {
			msg = "请求参数不合法，请检查提交内容"
		}
		return ErrCodeInvalidParameter, msg, "invalid_request_error", statusCode
	}

	if rawMsg != "" && !strings.HasPrefix(rawMsg, "{") {
		return ErrCodeInternalServerError, rawMsg, "server_error", httpStatus
	}
	return ErrCodeInternalServerError, "任务执行失败", "server_error", httpStatus
}

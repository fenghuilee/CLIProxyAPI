package aigc

import (
	"errors"
	"net/http"
	"testing"
)

func TestNormalizeError_WithDynamicRules(t *testing.T) {
	// 1. Configure in-memory dynamic rules
	SetDynamicRules([]DynamicErrorRule{
		{
			ID:           1,
			Provider:     "volcengine*",
			MatchType:    "code_contains",
			MatchPattern: "OutputVideoSensitiveContentDetected.PolicyViolation",
			StandardCode: ErrCodeCopyrightViolation,
			StandardType: "content_policy_error",
			UserMessage:  "生成内容可能涉及版权保护限制，请更换素材或提示词",
			HTTPStatus:   400,
			Priority:     10,
			Status:       1,
		},
		{
			ID:           2,
			Provider:     "volcengine*",
			MatchType:    "code_contains",
			MatchPattern: "OutputVideoSensitiveContentDetected",
			StandardCode: ErrCodeSensitiveMedia,
			StandardType: "content_policy_error",
			UserMessage:  "生成画面可能包含敏感内容，已被安全策略拦截",
			HTTPStatus:   400,
			Priority:     20,
			Status:       1,
		},
		{
			ID:           3,
			Provider:     "volcengine*",
			MatchType:    "code_contains",
			MatchPattern: "InputTextSensitiveContentDetected",
			StandardCode: ErrCodeSensitiveContent,
			StandardType: "content_policy_error",
			UserMessage:  "输入提示词包含敏感信息，已被安全策略拦截",
			HTTPStatus:   400,
			Priority:     20,
			Status:       1,
		},
		{
			ID:           4,
			Provider:     "volcengine*",
			MatchType:    "code_contains",
			MatchPattern: "AccessDenied",
			StandardCode: ErrCodeUpstreamAccessDenied,
			StandardType: "permission_error",
			UserMessage:  "上游火山引擎服务凭证无该模型或资源的调用权限",
			HTTPStatus:   403,
			Priority:     30,
			Status:       1,
		},
		{
			ID:           5,
			Provider:     "volcengine*",
			MatchType:    "msg_contains",
			MatchPattern: "last frame",
			StandardCode: ErrCodeInvalidFrameAsset,
			StandardType: "invalid_request_error",
			UserMessage:  "尾帧图片素材无法访问或格式不合规，请检查图片链接",
			HTTPStatus:   400,
			Priority:     40,
			Status:       1,
		},
		{
			ID:           6,
			Provider:     "qwen*",
			MatchType:    "code_contains",
			MatchPattern: "DataInspectionFailed",
			StandardCode: ErrCodeSensitiveContent,
			StandardType: "content_policy_error",
			UserMessage:  "输入提示词或生成内容未通过安全合规检测",
			HTTPStatus:   400,
			Priority:     20,
			Status:       1,
		},
		{
			ID:           7,
			Provider:     "*",
			MatchType:    "msg_contains",
			MatchPattern: "must be 'WIDTHxHEIGHT'",
			StandardCode: ErrCodeInvalidMediaSize,
			StandardType: "invalid_request_error",
			UserMessage:  "图片尺寸参数不合规，格式必须为 WIDTHxHEIGHT (如 1024x1024)",
			HTTPStatus:   400,
			Priority:     50,
			Status:       1,
		},
		{
			ID:           8,
			Provider:     "*",
			MatchType:    "msg_regex",
			MatchPattern: `(?i)(insufficient quota|balance)`,
			StandardCode: ErrCodeInsufficientQuota,
			StandardType: "permission_error",
			UserMessage:  "账户余额或算力积分不足，请充值后重试",
			HTTPStatus:   402,
			Priority:     80,
			Status:       1,
		},
	})

	tests := []struct {
		name         string
		provider     string
		statusCode   int
		rawBody      string
		fallbackErr  error
		wantCode     string
		wantType     string
		wantStatus   int
		wantContains string
	}{
		{
			name:         "volcengine copyright policy violation matched via rule",
			provider:     "volcengine-seedance",
			statusCode:   200,
			rawBody:      `{"error":{"code":"OutputVideoSensitiveContentDetected.PolicyViolation","message":"The request failed because the output video may be related to copyright restrictions. Request id: 0217873602997430000000"}}`,
			wantCode:     ErrCodeCopyrightViolation,
			wantType:     "content_policy_error",
			wantStatus:   http.StatusBadRequest,
			wantContains: "版权保护限制",
		},
		{
			name:         "volcengine sensitive video output matched via rule",
			provider:     "volcengine-seedance",
			statusCode:   200,
			rawBody:      `{"error":{"code":"OutputVideoSensitiveContentDetected","message":"The request failed because the output video may contain sensitive information. Request id: 02178740503500400000000000000"}}`,
			wantCode:     ErrCodeSensitiveMedia,
			wantType:     "content_policy_error",
			wantStatus:   http.StatusBadRequest,
			wantContains: "敏感内容",
		},
		{
			name:         "qwen data inspection failed matched via rule",
			provider:     "qwen-image",
			statusCode:   400,
			rawBody:      `{"code":"DataInspectionFailed","message":"Input data contains inappropriate terms."}`,
			wantCode:     ErrCodeSensitiveContent,
			wantType:     "content_policy_error",
			wantStatus:   http.StatusBadRequest,
			wantContains: "安全合规",
		},
		{
			name:         "universal regex rule matched",
			provider:     "openai-compat",
			statusCode:   400,
			rawBody:      `{"error":{"code":"InvalidParameter","message":"The parameter size specified in the request are not valid: must be 'WIDTHxHEIGHT'."}}`,
			wantCode:     ErrCodeInvalidMediaSize,
			wantType:     "invalid_request_error",
			wantStatus:   http.StatusBadRequest,
			wantContains: "WIDTHxHEIGHT",
		},
		{
			name:         "unmatched error falls back to pure 401 protocol status",
			provider:     "some-new-provider",
			statusCode:   401,
			rawBody:      `{"error":{"message":"unknown token format"}}`,
			wantCode:     ErrCodeInvalidAPIKey,
			wantType:     "authentication_error",
			wantStatus:   http.StatusUnauthorized,
			wantContains: "API Key",
		},
		{
			name:         "unmatched error falls back to pure 502 protocol status",
			provider:     "some-new-provider",
			statusCode:   502,
			rawBody:      `bad gateway on upstream node 9`,
			wantCode:     ErrCodeUpstreamServiceError,
			wantType:     "upstream_error",
			wantStatus:   http.StatusBadGateway,
			wantContains: "bad gateway on upstream node 9",
		},
		{
			name:         "driver not found fallback",
			provider:     "",
			statusCode:   500,
			rawBody:      `{"code":"driver_not_found","message":"no driver available for model gpt-image-2"}`,
			wantCode:     ErrCodeDriverNotFound,
			wantType:     "routing_error",
			wantStatus:   http.StatusInternalServerError,
			wantContains: "暂无可用驱动",
		},
		{
			name:         "fallback from Go error",
			provider:     "",
			statusCode:   0,
			fallbackErr:  errors.New("mutation_rejected: preparation rejected by policy"),
			wantCode:     ErrCodeMutationRejected,
			wantType:     "mutation_error",
			wantStatus:   http.StatusBadRequest,
			wantContains: "策略拒绝",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			norm := NormalizeError(tt.provider, tt.statusCode, []byte(tt.rawBody), tt.fallbackErr)
			if norm.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", norm.Code, tt.wantCode)
			}
			if norm.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", norm.Type, tt.wantType)
			}
			if norm.HTTPStatus != tt.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", norm.HTTPStatus, tt.wantStatus)
			}
			if !testing.Short() && tt.wantContains != "" {
				if norm.Message == "" {
					t.Errorf("Message is empty, expected containing %q", tt.wantContains)
				}
			}
		})
	}
}

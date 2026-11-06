package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// User represents a registered portal and billing user.
type User struct {
	ID           uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string          `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string          `gorm:"size:255;not null" json:"-"`
	Role         string          `gorm:"size:16;not null;default:'user'" json:"role"`
	GroupName    string          `gorm:"size:64;not null;default:'default'" json:"group_name"`
	Quota        decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0" json:"quota"`
	FrozenQuota  decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0" json:"frozen_quota"`
	UsedQuota    decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0" json:"used_quota"`
	Status       int8            `gorm:"not null;default:1" json:"status"` // 1: active, 0: disabled
	LastLoginAt  *time.Time      `json:"last_login_at"`
	CreatedAt    time.Time       `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time       `gorm:"not null;autoUpdateTime" json:"updated_at"`

	APIKeys []APIKey `gorm:"foreignKey:UserID" json:"api_keys,omitempty"`
}

func (User) TableName() string { return "users" }

// ModelGroup represents a model access permission group.
type ModelGroup struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	DisplayName string    `gorm:"size:128;not null" json:"display_name"`
	Models      string    `gorm:"type:text;not null" json:"models"` // JSON encoded string of []string
	Status      int8      `gorm:"not null;default:1" json:"status"` // 1: active, 0: disabled
	CreatedAt   time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (ModelGroup) TableName() string { return "model_groups" }

// ModelGroupDTO represents the API response view of a model group where models is a native string slice.
type ModelGroupDTO struct {
	ID          uint64    `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Models      []string  `json:"models"`
	Status      int8      `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (g ModelGroup) ToDTO() ModelGroupDTO {
	return ModelGroupDTO{
		ID:          g.ID,
		Name:        g.Name,
		DisplayName: g.DisplayName,
		Models:      ParseModelsJSON(g.Models),
		Status:      g.Status,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}
}

// ParseModelsJSON safely parses a JSON string or comma-separated string into a string slice.
func ParseModelsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"*"}
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err == nil && len(models) > 0 {
		var cleaned []string
		for _, m := range models {
			if s := strings.TrimSpace(m); s != "" {
				cleaned = append(cleaned, s)
			}
		}
		if len(cleaned) > 0 {
			return cleaned
		}
	}
	// If it has JSON brackets and unmarshal failed, fallback to ["*"]
	if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "{") || strings.HasSuffix(raw, "]") || strings.HasSuffix(raw, "}") {
		return []string{"*"}
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		var cleaned []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				cleaned = append(cleaned, s)
			}
		}
		if len(cleaned) > 0 {
			return cleaned
		}
	}
	if !strings.Contains(raw, " ") && !strings.Contains(raw, "\n") {
		return []string{raw}
	}
	return []string{"*"}
}

// APIKey represents an API Key owned by a User.
type APIKey struct {
	ID         uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     uint64         `gorm:"index;not null" json:"user_id"`
	Key        string         `gorm:"column:api_key;size:255;uniqueIndex;not null" json:"api_key"`
	Name       string         `gorm:"size:128;not null;default:'Default Key'" json:"name"`
	Status     int8           `gorm:"not null;default:1" json:"status"` // 1: enabled, 0: disabled
	LastUsedAt *time.Time     `json:"last_used_at"`
	CreatedAt  time.Time      `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (APIKey) TableName() string { return "api_keys" }

// RechargeOrder stores payment orders for recharging points.
type RechargeOrder struct {
	ID        uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	OrderNo   string          `gorm:"size:64;uniqueIndex;not null" json:"order_no"`
	UserID    uint64          `gorm:"index;not null" json:"user_id"`
	Username  string          `gorm:"->" json:"username,omitempty"`
	AmountCNY decimal.Decimal `gorm:"type:decimal(12,2);not null" json:"amount_cny"`
	Points    decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"points"`
	Channel   string          `gorm:"size:16;not null" json:"channel"` // wechat, alipay
	TradeNo   string          `gorm:"size:128" json:"trade_no"`
	Status    string          `gorm:"size:16;not null;default:'pending'" json:"status"` // pending, paid, failed, closed
	QRCodeURL string          `gorm:"type:text" json:"qr_code_url,omitempty"`
	PaidAt    *time.Time      `json:"paid_at"`
	CreatedAt time.Time       `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time       `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (RechargeOrder) TableName() string { return "recharge_orders" }

// RechargePackage represents one recharge option.
type RechargePackage struct {
	ID        uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Code      string          `gorm:"size:64;not null;index" json:"code"`
	Name      string          `gorm:"size:128;not null" json:"name"`
	Channel   string          `gorm:"size:16;not null" json:"channel"`
	AmountCNY decimal.Decimal `gorm:"type:decimal(12,2);not null" json:"amount_cny"`
	Points    decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"points"`
	SortOrder int             `gorm:"not null;default:0" json:"sort_order"`
	Status    int8            `gorm:"not null;default:1" json:"status"`
	CreatedAt time.Time       `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time       `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (RechargePackage) TableName() string { return "recharge_packages" }

// Ledger transaction types
const (
	LedgerTypeRecharge     = "recharge"
	LedgerTypeManualAdjust = "manual_adjust"
	LedgerTypeTokenDeduct  = "token_deduct"
	LedgerTypeCallDeduct   = "call_deduct"
	LedgerTypeAIGCFreeze   = "aigc_freeze"
	LedgerTypeAIGCSettle   = "aigc_settle"
	LedgerTypeAIGCRefund   = "aigc_refund"
)

// Model billing types
const (
	BillingTypePerToken    = "per_token"
	BillingTypeDynamicCall = "dynamic_call"
	BillingTypePerCall     = "per_call"
)

// WalletLedger represents immutable wallet balance changes.
type WalletLedger struct {
	ID           uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint64          `gorm:"index;not null" json:"user_id"`
	Username     string          `gorm:"->" json:"username,omitempty"`
	Type         string          `gorm:"size:32;not null" json:"type"` // recharge, call_deduct, token_deduct, aigc_freeze, aigc_settle, aigc_refund, manual_adjust
	DeltaQuota   decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"delta_quota"`
	DeltaFrozen  decimal.Decimal `gorm:"type:decimal(20,8);not null;default:0" json:"delta_frozen"`
	BalanceAfter decimal.Decimal `gorm:"type:decimal(20,8);not null" json:"balance_after"`
	RefID        string          `gorm:"size:64;index;not null" json:"ref_id"`
	Remark       string          `gorm:"size:255" json:"remark"`
	CreatedAt    time.Time       `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (WalletLedger) TableName() string { return "wallet_ledger" }

// RemarkAdminAdjust formats an admin manual adjustment remark.
func RemarkAdminAdjust(reason string, delta decimal.Decimal) string {
	sign := ""
	if delta.IsPositive() {
		sign = "+"
	}
	if reason == "" {
		reason = "管理员调账"
	}
	return fmt.Sprintf("人工调账: %s (变动: %s%s)", reason, sign, delta.StringFixed(8))
}

// RemarkAdminRefund formats an admin manual refund remark.
func RemarkAdminRefund(reason, taskID string) string {
	if reason == "" {
		reason = "管理员退费"
	}
	if taskID != "" {
		return fmt.Sprintf("人工退款: %s (任务: %s)", reason, taskID)
	}
	return fmt.Sprintf("人工退款: %s", reason)
}

// RemarkManualRecharge formats a manual recharge order ledger remark.
func RemarkManualRecharge(channel, operatorRemark string, amountCNY, points decimal.Decimal) string {
	cName := channel
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "wechat", "wxpay", "微信", "微信支付":
		cName = "微信支付"
	case "alipay", "支付宝":
		cName = "支付宝"
	case "epay", "易支付":
		cName = "易支付"
	}
	if cName == "" {
		cName = "在线支付"
	}
	if operatorRemark != "" && !strings.HasPrefix(operatorRemark, "管理员手动补单") {
		return fmt.Sprintf("充值补单 (%s): %s (支付: %s 元, 到账: %s 积分)", cName, operatorRemark, amountCNY.StringFixed(2), points.StringFixed(8))
	}
	return fmt.Sprintf("充值补单 (%s): 支付 %s 元，到账 %s 积分", cName, amountCNY.StringFixed(2), points.StringFixed(8))
}

// UsageStatistic represents an inbound API request log.
type UsageStatistic struct {
	ID                  uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	RequestID           string          `gorm:"column:request_id;size:64;index;not null;default:''" json:"request_id"`
	GenerationID        string          `gorm:"column:generation_id;size:64;index" json:"generation_id,omitempty"`
	UserID              uint64          `gorm:"index;not null;default:0" json:"user_id"`
	APIKeyID            uint64          `gorm:"index;not null;default:0" json:"api_key_id"`
	Protocol            string          `gorm:"size:32;not null;default:'openai'" json:"protocol"`
	Model               string          `gorm:"size:128;index;not null;default:''" json:"model"`
	ClientIP            string          `gorm:"size:64;not null;default:''" json:"client_ip"`
	Failed              int             `gorm:"type:tinyint;not null;default:0" json:"failed"`
	FailureStatusCode   int             `gorm:"not null;default:0" json:"failure_status_code"`
	FailureBody         string          `gorm:"type:text" json:"failure_body,omitempty"`
	LatencyMs           int64           `gorm:"not null;default:0" json:"latency_ms"`
	TTFTMs              int64           `gorm:"not null;default:0" json:"ttft_ms"`
	InputTokens         int64           `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens        int64           `gorm:"not null;default:0" json:"output_tokens"`
	ReasoningTokens     int64           `gorm:"not null;default:0" json:"reasoning_tokens"`
	CachedTokens        int64           `gorm:"not null;default:0" json:"cached_tokens"`
	CacheReadTokens     int64           `gorm:"not null;default:0" json:"cache_read_tokens"`
	CacheCreationTokens int64           `gorm:"not null;default:0" json:"cache_creation_tokens"`
	TotalTokens         int64           `gorm:"not null;default:0" json:"total_tokens"`
	Cost                decimal.Decimal `gorm:"type:decimal(14,8);not null;default:0" json:"cost"`
	CreatedAt           time.Time       `gorm:"not null;index" json:"created_at"`

	// Joined fields for display
	Username string `gorm:"->" json:"username,omitempty"`
	APIKey   string `gorm:"->" json:"api_key,omitempty"`
	KeyName  string `gorm:"->" json:"key_name,omitempty"`
}

func (UsageStatistic) TableName() string { return "usage_statistics" }

// UsageStatisticDTO represents the API view of a usage log with failure_body parsed if valid JSON.
type UsageStatisticDTO struct {
	ID                  uint64          `json:"id"`
	RequestID           string          `json:"request_id"`
	GenerationID        string          `json:"generation_id,omitempty"`
	UserID              uint64          `json:"user_id"`
	APIKeyID            uint64          `json:"api_key_id"`
	Protocol            string          `json:"protocol"`
	Model               string          `json:"model"`
	ClientIP            string          `json:"client_ip"`
	Failed              int             `json:"failed"`
	FailureStatusCode   int             `json:"failure_status_code"`
	FailureBody         any             `json:"failure_body,omitempty"`
	LatencyMs           int64           `json:"latency_ms"`
	TTFTMs              int64           `json:"ttft_ms"`
	InputTokens         int64           `json:"input_tokens"`
	OutputTokens        int64           `json:"output_tokens"`
	ReasoningTokens     int64           `json:"reasoning_tokens"`
	CachedTokens        int64           `json:"cached_tokens"`
	CacheReadTokens     int64           `json:"cache_read_tokens"`
	CacheCreationTokens int64           `json:"cache_creation_tokens"`
	TotalTokens         int64           `json:"total_tokens"`
	Cost                decimal.Decimal `json:"cost"`
	CreatedAt           time.Time       `json:"created_at"`
	Username            string          `json:"username,omitempty"`
	APIKey              string          `json:"api_key,omitempty"`
	KeyName             string          `json:"key_name,omitempty"`
}

func (u UsageStatistic) ToDTO() UsageStatisticDTO {
	return UsageStatisticDTO{
		ID:                  u.ID,
		RequestID:           u.RequestID,
		GenerationID:        u.GenerationID,
		UserID:              u.UserID,
		APIKeyID:            u.APIKeyID,
		Protocol:            u.Protocol,
		Model:               u.Model,
		ClientIP:            u.ClientIP,
		Failed:              u.Failed,
		FailureStatusCode:   u.FailureStatusCode,
		FailureBody:         ParseJSONOrRaw(u.FailureBody),
		LatencyMs:           u.LatencyMs,
		TTFTMs:              u.TTFTMs,
		InputTokens:         u.InputTokens,
		OutputTokens:        u.OutputTokens,
		ReasoningTokens:     u.ReasoningTokens,
		CachedTokens:        u.CachedTokens,
		CacheReadTokens:     u.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens,
		TotalTokens:         u.TotalTokens,
		Cost:                u.Cost,
		CreatedAt:           u.CreatedAt,
		Username:            u.Username,
		APIKey:              u.APIKey,
		KeyName:             u.KeyName,
	}
}

// UsageStatisticHourly represents aggregated usage by hour.
type UsageStatisticHourly struct {
	ID           uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	StatHour     time.Time       `gorm:"not null;index" json:"stat_hour"`
	UserID       uint64          `gorm:"index;not null" json:"user_id"`
	APIKeyID     uint64          `gorm:"index;not null" json:"api_key_id"`
	Model        string          `gorm:"size:128;not null" json:"model"`
	RequestCount int             `gorm:"not null;default:0" json:"request_count"`
	SuccessCount int             `gorm:"not null;default:0" json:"success_count"`
	FailedCount  int             `gorm:"not null;default:0" json:"failed_count"`
	InputTokens  int64           `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens int64           `gorm:"not null;default:0" json:"output_tokens"`
	TotalTokens  int64           `gorm:"not null;default:0" json:"total_tokens"`
	TotalCost    decimal.Decimal `gorm:"type:decimal(16,8);not null;default:0" json:"total_cost"`
	AvgLatencyMs int             `gorm:"not null;default:0" json:"avg_latency_ms"`
}

func (UsageStatisticHourly) TableName() string { return "usage_statistics_hourly" }

// UsageStatisticDaily represents aggregated usage by day.
type UsageStatisticDaily struct {
	ID           uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	StatDate     string          `gorm:"size:10;not null;index" json:"stat_date"`
	UserID       uint64          `gorm:"index;not null" json:"user_id"`
	APIKeyID     uint64          `gorm:"index;not null" json:"api_key_id"`
	Model        string          `gorm:"size:128;not null" json:"model"`
	RequestCount int             `gorm:"not null;default:0" json:"request_count"`
	SuccessCount int             `gorm:"not null;default:0" json:"success_count"`
	FailedCount  int             `gorm:"not null;default:0" json:"failed_count"`
	InputTokens  int64           `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens int64           `gorm:"not null;default:0" json:"output_tokens"`
	TotalTokens  int64           `gorm:"not null;default:0" json:"total_tokens"`
	TotalCost    decimal.Decimal `gorm:"type:decimal(16,8);not null;default:0" json:"total_cost"`
}

func (UsageStatisticDaily) TableName() string { return "usage_statistics_daily" }

// ContentGeneration represents an AIGC task.
type ContentGeneration struct {
	GenerationID   string                      `gorm:"primaryKey;column:generation_id;size:64" json:"generation_id"`
	RequestID      string                      `gorm:"column:request_id;size:64;index" json:"request_id,omitempty"`
	UserID         uint64                      `gorm:"column:user_id;not null;default:0;index" json:"user_id"`
	Kind           string                      `gorm:"size:32;not null" json:"kind"`
	Model          string                      `gorm:"size:128;not null" json:"model"`
	Prompt         string                      `gorm:"type:text" json:"prompt,omitempty"`
	Status         string                      `gorm:"size:32;not null" json:"status"`
	Stage          string                      `gorm:"size:32;not null" json:"stage"`
	Progress       int                         `gorm:"not null;default:0" json:"progress"`
	Revision       uint64                      `gorm:"not null;default:1" json:"revision"`
	APIKey         string                      `gorm:"size:255" json:"api_key,omitempty"`
	Username       string                      `gorm:"->" json:"username,omitempty"`
	ClientIP       string                      `gorm:"size:64" json:"client_ip,omitempty"`
	Input          string                      `gorm:"type:longtext" json:"input,omitempty"`
	Output         string                      `gorm:"type:longtext" json:"output,omitempty"`
	Provider       string                      `gorm:"size:64" json:"provider,omitempty"`
	ProviderTaskID string                      `gorm:"size:255" json:"provider_task_id,omitempty"`
	ErrorCode      string                      `gorm:"size:64" json:"error_code,omitempty"`
	ErrorMessage   string                      `gorm:"type:text" json:"error_message,omitempty"`
	BillingType    string                      `gorm:"size:32;not null;default:'fixed'" json:"billing_type"`
	BillingStatus  string                      `gorm:"size:32;not null;default:'unbilled';index" json:"billing_status"`
	Cost           decimal.Decimal             `gorm:"type:decimal(18,8);not null;default:0" json:"cost"`
	DurationMs     int64                       `gorm:"column:duration_ms" json:"duration_ms,omitempty"`
	CreatedAt      time.Time                   `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time                   `gorm:"not null" json:"updated_at"`
	CompletedAt    *time.Time                  `gorm:"column:completed_at" json:"completed_at,omitempty"`
	Artifacts      []ContentGenerationArtifact `gorm:"foreignKey:GenerationID;references:GenerationID" json:"artifacts,omitempty"`
}

func (ContentGeneration) TableName() string { return "content_generations" }

// ParseJSONOrRaw parses a string as JSON (object, array, primitive), or returns the raw string if not JSON.
func ParseJSONOrRaw(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v
	}
	return raw
}

// ContentGenerationDTO represents the API view of an AIGC task with native objects for JSON fields.
type ContentGenerationDTO struct {
	GenerationID   string                      `json:"generation_id"`
	RequestID      string                      `json:"request_id,omitempty"`
	UserID         uint64                      `json:"user_id"`
	Kind           string                      `json:"kind"`
	Model          string                      `json:"model"`
	Prompt         string                      `json:"prompt,omitempty"`
	Status         string                      `json:"status"`
	Stage          string                      `json:"stage"`
	Progress       int                         `json:"progress"`
	Revision       uint64                      `json:"revision"`
	APIKey         string                      `json:"api_key,omitempty"`
	Username       string                      `json:"username,omitempty"`
	ClientIP       string                      `json:"client_ip,omitempty"`
	Input          any                         `json:"input,omitempty"`
	Output         any                         `json:"output,omitempty"`
	Provider       string                      `json:"provider,omitempty"`
	ProviderTaskID string                      `json:"provider_task_id,omitempty"`
	ErrorCode      string                      `json:"error_code,omitempty"`
	ErrorMessage   string                      `json:"error_message,omitempty"`
	BillingType    string                      `json:"billing_type"`
	BillingStatus  string                      `json:"billing_status"`
	Cost           decimal.Decimal             `json:"cost"`
	DurationMs     int64                       `json:"duration_ms,omitempty"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
	CompletedAt    *time.Time                  `json:"completed_at,omitempty"`
	Artifacts      []ContentGenerationArtifact `json:"artifacts,omitempty"`
}

func (c ContentGeneration) ToDTO() ContentGenerationDTO {
	return ContentGenerationDTO{
		GenerationID:   c.GenerationID,
		RequestID:      c.RequestID,
		UserID:         c.UserID,
		Kind:           c.Kind,
		Model:          c.Model,
		Prompt:         c.Prompt,
		Status:         c.Status,
		Stage:          c.Stage,
		Progress:       c.Progress,
		Revision:       c.Revision,
		APIKey:         c.APIKey,
		Username:       c.Username,
		ClientIP:       c.ClientIP,
		Input:          ParseJSONOrRaw(c.Input),
		Output:         ParseJSONOrRaw(c.Output),
		Provider:       c.Provider,
		ProviderTaskID: c.ProviderTaskID,
		ErrorCode:      c.ErrorCode,
		ErrorMessage:   c.ErrorMessage,
		BillingType:    c.BillingType,
		BillingStatus:  c.BillingStatus,
		Cost:           c.Cost,
		DurationMs:     c.DurationMs,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
		CompletedAt:    c.CompletedAt,
		Artifacts:      c.Artifacts,
	}
}

// ContentGenerationArtifact represents an artifact produced by an AIGC task.
type ContentGenerationArtifact struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	GenerationID    string    `gorm:"size:64;index;not null" json:"generation_id"`
	ArtifactType    string    `gorm:"size:64;not null" json:"artifact_type"`
	StorageProvider string    `gorm:"size:64;not null" json:"storage_provider"`
	URI             string    `gorm:"size:1024;not null" json:"uri"`
	MIMEType        string    `gorm:"size:64" json:"mime_type,omitempty"`
	SizeBytes       int64     `gorm:"not null;default:0" json:"size_bytes"`
	SHA256          string    `gorm:"size:64" json:"sha256,omitempty"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (ContentGenerationArtifact) TableName() string { return "content_generation_artifacts" }

// ErrorMappingRule represents a dynamic mapping rule for upstream errors.
type ErrorMappingRule struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Provider      string    `gorm:"size:64;not null;default:'*'" json:"provider"`
	MatchType     string    `gorm:"size:32;not null;default:'code_exact'" json:"match_type"`
	MatchPattern  string    `gorm:"size:255;not null" json:"match_pattern"`
	StandardCode  string    `gorm:"size:64;not null" json:"standard_code"`
	StandardType  string    `gorm:"size:64;not null;default:'invalid_request_error'" json:"standard_type"`
	UserMessage   string    `gorm:"size:512;not null" json:"user_message"`
	UserMessageEN string    `gorm:"size:512" json:"user_message_en,omitempty"`
	HTTPStatus    int       `gorm:"not null;default:400" json:"http_status"`
	Priority      int       `gorm:"not null;default:100" json:"priority"`
	Status        int8      `gorm:"not null;default:1" json:"status"`
	Description   string    `gorm:"size:255" json:"description,omitempty"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (ErrorMappingRule) TableName() string { return "error_mapping_rules" }

// DocArticle represents a dynamic documentation article / tab in the API Guide.
type DocArticle struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Slug      string    `gorm:"size:64;uniqueIndex;not null" json:"slug"`
	Title     string    `gorm:"size:128;not null" json:"title"`
	Icon      string    `gorm:"size:64;not null;default:'file-text'" json:"icon"`
	Badge     string    `gorm:"size:32;not null;default:''" json:"badge"`
	Category  string    `gorm:"size:64;not null;default:'general'" json:"category"`
	ContentMD string    `gorm:"type:longtext;not null" json:"content_md"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	Status    int8      `gorm:"not null;default:1" json:"status"` // 1: published, 0: draft
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (DocArticle) TableName() string { return "doc_articles" }

package aigc

import (
	"context"
	"encoding/json"
	"net/http"
)

// GenerationSupportRequest checks whether a driver supports the specified request.
type GenerationSupportRequest struct {
	Kind  ContentKind `json:"kind"`
	Model string      `json:"model"`
}

// GenerationSupportResponse returns support information.
type GenerationSupportResponse struct {
	Supported bool   `json:"supported"`
	Provider  string `json:"provider,omitempty"`
}

// GenerationSubmitInput provides input data to prepare a submit request.
type GenerationSubmitInput struct {
	Generation ContentGeneration `json:"generation"`
}

// GenerationExecutionRequest represents an upstream HTTP/RPC call to be executed by CPA.
type GenerationExecutionRequest struct {
	Method   string         `json:"method"`
	URL      string         `json:"url"`
	Header   http.Header    `json:"header,omitempty"`
	Body     []byte         `json:"body,omitempty"`
	Model    string         `json:"model,omitempty"`
	AuthID   string         `json:"auth_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GenerationExecutionResponse represents the response returned by CPA execution.
type GenerationExecutionResponse struct {
	StatusCode int            `json:"status_code"`
	Header     http.Header    `json:"header,omitempty"`
	Body       []byte         `json:"body,omitempty"`
	AuthID     string         `json:"auth_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// GenerationSubmitResult returns the parsed result of a submit call.
type GenerationSubmitResult struct {
	ProviderTaskID   string                      `json:"provider_task_id"`
	Status           GenerationStatus            `json:"status"`
	Stage            GenerationStage             `json:"stage"`
	Progress         int                         `json:"progress"`
	ProviderRequest  json.RawMessage             `json:"provider_request,omitempty"`
	ProviderResponse json.RawMessage             `json:"provider_response,omitempty"`
	Output           json.RawMessage             `json:"output,omitempty"`
	Artifacts        []ContentGenerationArtifact `json:"artifacts,omitempty"`
	ErrorCode        string                      `json:"error_code,omitempty"`
	ErrorMessage     string                      `json:"error_message,omitempty"`
	Metadata         map[string]any              `json:"metadata,omitempty"`
}

// GenerationPollInput provides data to prepare a poll request.
type GenerationPollInput struct {
	Generation ContentGeneration `json:"generation"`
}

// GenerationPollResult returns the parsed result of a poll call.
type GenerationPollResult struct {
	Status           GenerationStatus            `json:"status"`
	Stage            GenerationStage             `json:"stage"`
	Progress         int                         `json:"progress"`
	ProviderResponse json.RawMessage             `json:"provider_response,omitempty"`
	Output           json.RawMessage             `json:"output,omitempty"`
	Artifacts        []ContentGenerationArtifact `json:"artifacts,omitempty"`
	ErrorCode        string                      `json:"error_code,omitempty"`
	ErrorMessage     string                      `json:"error_message,omitempty"`
	Metadata         map[string]any              `json:"metadata,omitempty"`
}

// GenerationCancelInput provides data to prepare a cancel request.
type GenerationCancelInput struct {
	Generation ContentGeneration `json:"generation"`
}

// ContentGenerationDriver is the interface implemented by provider protocol adapters.
type ContentGenerationDriver interface {
	Supports(ctx context.Context, req GenerationSupportRequest) (GenerationSupportResponse, error)
	PrepareSubmit(ctx context.Context, input GenerationSubmitInput) (GenerationExecutionRequest, error)
	ParseSubmit(ctx context.Context, resp GenerationExecutionResponse) (GenerationSubmitResult, error)
	PreparePoll(ctx context.Context, input GenerationPollInput) (GenerationExecutionRequest, error)
	ParsePoll(ctx context.Context, resp GenerationExecutionResponse) (GenerationPollResult, error)
	PrepareCancel(ctx context.Context, input GenerationCancelInput) (GenerationExecutionRequest, error)
	ParseCancel(ctx context.Context, resp GenerationExecutionResponse) error
}

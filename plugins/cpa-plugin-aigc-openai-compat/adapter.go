package main

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

// SubAdapter defines the standard interface for vendor-specific or protocol-specific AIGC adapters.
type SubAdapter interface {
	// Name returns the unique identifier for this adapter (e.g. "qwen-image", "volcengine-seedance", "volcengine-seedream", "openai-compat").
	Name() string

	// Supports checks if this adapter can handle the given generation request (kind & model).
	Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool

	// PrepareSubmit converts the internal ContentGeneration draft into an upstream HTTP execution request.
	PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error)

	// ParseSubmit parses the upstream HTTP response from Submit into a submit result (completed or async running).
	ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error)

	// PreparePoll creates the HTTP request to check the status of an asynchronous generation task.
	PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error)

	// ParsePoll parses the HTTP response from a status polling check.
	ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error)

	// PrepareCancel creates the HTTP request to cancel a running generation task on the provider.
	PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error)

	// ParseCancel parses the HTTP response from a cancel request.
	ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error
}

package driver

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// Adapter is the common interface implemented by content-generation adapters.
type Adapter interface {
	Name() string
	PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error)
	ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error)
	PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error)
	ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error)
	PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error)
	ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error
	ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error)
}

// FallbackAdapter can accept requests not matched by model prefixes.
type FallbackAdapter interface {
	Adapter
	Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool
}

// PrefixProvider is implemented by adapters that declare supported content kind and model prefixes.
type PrefixProvider interface {
	Kind() aigc.ContentKind
	Prefixes() []string
}

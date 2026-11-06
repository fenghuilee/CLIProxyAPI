package driver

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// PluginVersion identifies the plugin version, injectable via ldflags.
var PluginVersion = "v2026-01-01 (build 00:00:00)"

// Driver routes AIGC generation tasks to matching vendor adapters via prefix tries and fallback.
type Driver struct {
	registry        *Registry
	fallbackAdapter FallbackAdapter
}

// NewDriver creates an AIGC driver from registered adapter factories.
func NewDriver() *Driver {
	factories := CatalogSnapshot()
	d, err := NewDriverWithFactories(factories)
	if err != nil {
		panic(err)
	}
	return d
}

// NewDriverWithFactories builds a Driver from an explicit slice of factories.
func NewDriverWithFactories(factories []Factory) (*Driver, error) {
	reg := NewRegistry()
	var fallback FallbackAdapter
	for _, factory := range factories {
		adapter := factory.New()
		if adapter == nil || adapter.Name() != factory.Name {
			return nil, fmt.Errorf("adapter factory %q returned invalid adapter", factory.Name)
		}
		if factory.Fallback {
			candidate, ok := adapter.(FallbackAdapter)
			if !ok || fallback != nil {
				return nil, fmt.Errorf("adapter factory %q has invalid fallback role", factory.Name)
			}
			fallback = candidate
			continue
		}
		router, ok := adapter.(PrefixProvider)
		if !ok || len(router.Prefixes()) == 0 {
			return nil, fmt.Errorf("adapter factory %q does not declare model prefixes", factory.Name)
		}
		if err := reg.Register(adapter); err != nil {
			return nil, fmt.Errorf("failed to register adapter %s: %w", adapter.Name(), err)
		}
	}
	if fallback == nil {
		return nil, fmt.Errorf("no fallback adapter registered")
	}
	if err := reg.SetFallback(fallback); err != nil {
		return nil, fmt.Errorf("failed to set fallback adapter: %w", err)
	}

	return &Driver{
		registry:        reg,
		fallbackAdapter: fallback,
	}, nil
}

// Registry returns the underlying model routing registry.
func (d *Driver) Registry() *Registry {
	return d.registry
}

// Supports checks whether a generation request is supported via longest-prefix match or fallback.
func (d *Driver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	if adapter, found := d.registry.Lookup(req.Kind, req.Model); found {
		return aigc.GenerationSupportResponse{Supported: true, Provider: adapter.Name()}, nil
	}
	if d.fallbackAdapter != nil && d.fallbackAdapter.Supports(ctx, req) {
		return aigc.GenerationSupportResponse{Supported: true, Provider: d.fallbackAdapter.Name()}, nil
	}
	return aigc.GenerationSupportResponse{Supported: false}, nil
}

func (d *Driver) getAdapterForGeneration(gen aigc.ContentGeneration) Adapter {
	if name, ok := gen.Metadata["aigc_adapter"].(string); ok && name != "" {
		return d.registry.GetAdapterByName(name)
	}
	if gen.Provider != "" {
		if adapter := d.registry.GetAdapterByName(gen.Provider); adapter != nil {
			return adapter
		}
	}
	if adapter, found := d.registry.Lookup(gen.Kind, gen.Model); found {
		return adapter
	}
	req := aigc.GenerationSupportRequest{Kind: gen.Kind, Model: gen.Model}
	if d.fallbackAdapter != nil && d.fallbackAdapter.Supports(context.Background(), req) {
		return d.fallbackAdapter
	}
	return nil
}

// PrepareSubmit validates input parameters and delegates request assembly to the matched adapter.
func (d *Driver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	if err := ValidateOpenAIInput(input.Generation.Kind, input.Generation.Input); err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	adapter := d.getAdapterForGeneration(input.Generation)
	if adapter == nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("no adapter found for model %q (kind: %s)", input.Generation.Model, input.Generation.Kind)
	}
	return adapter.PrepareSubmit(ctx, input)
}

// ParseSubmit delegates submission parsing to the adapter resolved by provider metadata.
func (d *Driver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	provider, ok := resp.Metadata["provider"].(string)
	if !ok || provider == "" {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("missing provider in response metadata")
	}
	adapter := d.registry.GetAdapterByName(provider)
	if adapter == nil {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("unknown provider %q in response metadata", provider)
	}
	return adapter.ParseSubmit(ctx, resp)
}

// PreparePoll delegates poll request assembly to the task's adapter.
func (d *Driver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	adapter := d.getAdapterForGeneration(input.Generation)
	if adapter == nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("no adapter found for poll task %q", input.Generation.ID)
	}
	return adapter.PreparePoll(ctx, input)
}

// ParsePoll delegates poll response parsing to the adapter resolved by provider metadata.
func (d *Driver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	provider, ok := resp.Metadata["provider"].(string)
	if !ok || provider == "" {
		return aigc.GenerationPollResult{}, fmt.Errorf("missing provider in response metadata")
	}
	adapter := d.registry.GetAdapterByName(provider)
	if adapter == nil {
		return aigc.GenerationPollResult{}, fmt.Errorf("unknown provider %q in response metadata", provider)
	}
	return adapter.ParsePoll(ctx, resp)
}

// PrepareCancel delegates cancel request assembly to the task's adapter.
func (d *Driver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	adapter := d.getAdapterForGeneration(input.Generation)
	if adapter == nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("no adapter found for cancel task %q", input.Generation.ID)
	}
	return adapter.PrepareCancel(ctx, input)
}

// ParseCancel delegates cancel response verification to the adapter resolved by provider metadata.
func (d *Driver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	provider, ok := resp.Metadata["provider"].(string)
	if !ok || provider == "" {
		return fmt.Errorf("missing provider in response metadata")
	}
	adapter := d.registry.GetAdapterByName(provider)
	if adapter == nil {
		return fmt.Errorf("unknown provider %q in response metadata", provider)
	}
	return adapter.ParseCancel(ctx, resp)
}

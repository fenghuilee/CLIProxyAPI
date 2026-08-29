package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-aigc-openai-compat/adapters/common"
	"github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-aigc-openai-compat/adapters/image"
	"github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-aigc-openai-compat/adapters/video"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

// Driver manages and dispatches requests across all AIGC sub-adapters.
type Driver struct {
	mu             sync.RWMutex
	cfg            Config
	adaptersOrder  []SubAdapter
	adaptersByName map[string]SubAdapter
}

// NewDriver creates and initializes the composite AIGC driver.
func NewDriver() *Driver {
	d := &Driver{
		adaptersByName: make(map[string]SubAdapter),
	}
	d.SetConfig(DefaultConfig())
	return d
}

// SetConfig updates the driver configuration and rebuilds the adapter dispatch chain.
func (d *Driver) SetConfig(cfg Config) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.cfg = cfg

	// 1. Qwen Image Adapter
	qwenAdapter := image.NewQwenImageAdapter(func() (models []string, endpoint string, pe, et bool) {
		d.mu.RLock()
		defer d.mu.RUnlock()
		c := d.cfg.QwenImage
		models = c.Models
		endpoint = c.Endpoint
		pe = true
		if c.PromptExtend != nil {
			pe = *c.PromptExtend
		}
		et = true
		if c.EnableThinking != nil {
			et = *c.EnableThinking
		}
		return
	})

	// 2. Volcengine Seedance Video Adapter
	seedanceAdapter := video.NewVolcengineSeedanceAdapter(func() (models []string, endpoint string) {
		d.mu.RLock()
		defer d.mu.RUnlock()
		c := d.cfg.VolcengineSeedance
		return c.Models, c.Endpoint
	})

	// 3. Volcengine Seedream Image/Layer Adapter
	seedreamAdapter := image.NewVolcengineSeedreamAdapter(func() (models []string, endpoint string) {
		d.mu.RLock()
		defer d.mu.RUnlock()
		c := d.cfg.VolcengineSeedream
		return c.Models, c.Endpoint
	})

	// 4. OpenAI Compat Universal Adapter
	openaiAdapter := common.NewOpenAICompatAdapter(func() (imgModels, vidModels []string, fallback bool, imgSubmitPath, imgEditPath, vidSubmitPath, vidPollTpl string) {
		d.mu.RLock()
		defer d.mu.RUnlock()
		c := d.cfg.OpenAICompat
		imgModels = c.ImageModels
		vidModels = c.VideoModels
		fallback = true
		if c.Fallback != nil {
			fallback = *c.Fallback
		}
		return imgModels, vidModels, fallback, c.ImageSubmitPath, c.ImageEditPath, c.VideoSubmitPath, c.VideoPollPathTemplate
	})

	var order []SubAdapter

	if cfg.QwenImage.Enabled == nil || *cfg.QwenImage.Enabled {
		order = append(order, qwenAdapter)
	}
	if cfg.VolcengineSeedance.Enabled == nil || *cfg.VolcengineSeedance.Enabled {
		order = append(order, seedanceAdapter)
	}
	if cfg.VolcengineSeedream.Enabled == nil || *cfg.VolcengineSeedream.Enabled {
		order = append(order, seedreamAdapter)
	}
	if cfg.OpenAICompat.Enabled == nil || *cfg.OpenAICompat.Enabled {
		order = append(order, openaiAdapter)
	}

	byName := make(map[string]SubAdapter)
	for _, a := range order {
		byName[a.Name()] = a
	}

	// Always make all adapters available by name for existing provider tasks
	byName[qwenAdapter.Name()] = qwenAdapter
	byName[seedanceAdapter.Name()] = seedanceAdapter
	byName[seedreamAdapter.Name()] = seedreamAdapter
	byName[openaiAdapter.Name()] = openaiAdapter

	d.adaptersOrder = order
	d.adaptersByName = byName
}

// Supports evaluates the ordered adapter chain and short-circuits on the first match.
func (d *Driver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, adapter := range d.adaptersOrder {
		if adapter.Supports(ctx, req) {
			return aigc.GenerationSupportResponse{
				Supported: true,
				Provider:  adapter.Name(),
			}, nil
		}
	}

	return aigc.GenerationSupportResponse{Supported: false}, nil
}

func (d *Driver) getAdapterForGeneration(gen aigc.ContentGeneration) SubAdapter {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 1. Direct O(1) lookup by locked Provider name
	if gen.Provider != "" {
		if a, ok := d.adaptersByName[gen.Provider]; ok {
			return a
		}
	}

	// 2. Fallback matching via priority chain
	req := aigc.GenerationSupportRequest{
		Kind:  gen.Kind,
		Model: gen.Model,
	}
	for _, a := range d.adaptersOrder {
		if a.Supports(context.Background(), req) {
			return a
		}
	}

	return nil
}

// PrepareSubmit validates input according to OpenAI standards and delegates to the matched sub-adapter.
func (d *Driver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	inputBytes := input.Generation.Input
	if err := ValidateOpenAIInput(input.Generation.Kind, inputBytes); err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}

	adapter := d.getAdapterForGeneration(input.Generation)
	if adapter == nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("no adapter found for model %q (kind: %s)", input.Generation.Model, input.Generation.Kind)
	}
	return adapter.PrepareSubmit(ctx, input)
}

// ParseSubmit delegates to the matched sub-adapter.
func (d *Driver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	// If provider is indicated in response or metadata, route accordingly
	d.mu.RLock()
	adapters := d.adaptersOrder
	byName := d.adaptersByName
	d.mu.RUnlock()

	// 1. Direct O(1) lookup if provider is specified in execution metadata
	if provider, ok := resp.Metadata["provider"].(string); ok && provider != "" {
		if a, exists := byName[provider]; exists {
			return a.ParseSubmit(ctx, resp)
		}
	}

	// 2. Try each adapter in priority order
	for _, a := range adapters {
		res, err := a.ParseSubmit(ctx, resp)
		if err == nil && res.Status != "" {
			return res, nil
		}
	}

	if len(adapters) > 0 {
		return adapters[0].ParseSubmit(ctx, resp)
	}

	return aigc.GenerationSubmitResult{}, fmt.Errorf("no adapter available to parse submit response")
}

// PreparePoll delegates to the locked provider adapter.
func (d *Driver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	adapter := d.getAdapterForGeneration(input.Generation)
	if adapter == nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("no adapter found for poll task %q", input.Generation.ID)
	}
	return adapter.PreparePoll(ctx, input)
}

// ParsePoll delegates to the locked provider adapter.
func (d *Driver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	d.mu.RLock()
	adapters := d.adaptersOrder
	byName := d.adaptersByName
	d.mu.RUnlock()

	// 1. Direct O(1) lookup if provider is specified in execution metadata
	if provider, ok := resp.Metadata["provider"].(string); ok && provider != "" {
		if a, exists := byName[provider]; exists {
			return a.ParsePoll(ctx, resp)
		}
	}

	// 2. Try each adapter in priority order
	for _, a := range adapters {
		res, err := a.ParsePoll(ctx, resp)
		if err == nil && res.Status != "" {
			return res, nil
		}
	}

	if len(adapters) > 0 {
		return adapters[0].ParsePoll(ctx, resp)
	}

	return aigc.GenerationPollResult{}, fmt.Errorf("no adapter available to parse poll response")
}

// PrepareCancel delegates to the locked provider adapter.
func (d *Driver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	adapter := d.getAdapterForGeneration(input.Generation)
	if adapter == nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("no adapter found for cancel task %q", input.Generation.ID)
	}
	return adapter.PrepareCancel(ctx, input)
}

// ParseCancel delegates to the locked provider adapter.
func (d *Driver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	d.mu.RLock()
	adapters := d.adaptersOrder
	byName := d.adaptersByName
	d.mu.RUnlock()

	// 1. Direct O(1) lookup if provider is specified in execution metadata
	if provider, ok := resp.Metadata["provider"].(string); ok && provider != "" {
		if a, exists := byName[provider]; exists {
			return a.ParseCancel(ctx, resp)
		}
	}

	for _, a := range adapters {
		if err := a.ParseCancel(ctx, resp); err == nil {
			return nil
		}
	}
	return nil
}

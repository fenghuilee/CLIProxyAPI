package driver_test

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

type mockAdapter struct {
	name string
}

func (m *mockAdapter) Name() string { return m.name }
func (m *mockAdapter) PrepareSubmit(_ context.Context, _ aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": m.name}}, nil
}
func (m *mockAdapter) ParseSubmit(_ context.Context, _ aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{Status: aigc.StatusRunning, ProviderTaskID: "mock-task-1"}, nil
}
func (m *mockAdapter) PreparePoll(_ context.Context, _ aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": m.name}}, nil
}
func (m *mockAdapter) ParsePoll(_ context.Context, _ aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{Status: aigc.StatusSucceeded}, nil
}
func (m *mockAdapter) PrepareCancel(_ context.Context, _ aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": m.name}}, nil
}
func (m *mockAdapter) ParseCancel(_ context.Context, _ aigc.GenerationExecutionResponse) error {
	return nil
}
func (m *mockAdapter) ParseCallback(_ context.Context, _ aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{Status: aigc.StatusSucceeded}, nil
}

type mockFallbackAdapter struct {
	mockAdapter
	supportedModels map[string]bool
}

func (m *mockFallbackAdapter) Supports(_ context.Context, req aigc.GenerationSupportRequest) bool {
	if m.supportedModels == nil {
		return true
	}
	return m.supportedModels[req.Model]
}

func TestAdapterFactoriesRejectDuplicateNamesAndFallbacks(t *testing.T) {
	factories := []driver.Factory{
		{Name: "example", New: func() driver.Adapter { return &mockAdapter{name: "example"} }},
		{Name: "example", New: func() driver.Adapter { return &mockAdapter{name: "example"} }},
		{Name: "fallback", New: func() driver.Adapter { return &mockFallbackAdapter{mockAdapter: mockAdapter{name: "fallback"}} }, Fallback: true},
	}
	if _, err := driver.NewDriverWithFactories(factories); err == nil {
		t.Fatal("duplicate adapter name was accepted")
	}

	doubleFallbacks := []driver.Factory{
		{Name: "fallback-one", New: func() driver.Adapter { return &mockFallbackAdapter{mockAdapter: mockAdapter{name: "fallback-one"}} }, Fallback: true},
		{Name: "fallback-two", New: func() driver.Adapter { return &mockFallbackAdapter{mockAdapter: mockAdapter{name: "fallback-two"}} }, Fallback: true},
	}
	if _, err := driver.NewDriverWithFactories(doubleFallbacks); err == nil {
		t.Fatal("second fallback was accepted")
	}
}

func TestRegisterFactory_Validation(t *testing.T) {
	if err := driver.RegisterFactory(driver.Factory{Name: ""}); err == nil {
		t.Fatal("expected error for empty factory name")
	}
	if err := driver.RegisterFactory(driver.Factory{Name: "valid", New: nil}); err == nil {
		t.Fatal("expected error for nil constructor")
	}
}

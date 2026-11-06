package aigc

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type metadataDriver struct {
	testDriver
	t                                      *testing.T
	submitParsed, pollParsed, cancelParsed bool
}

func (d *metadataDriver) PrepareSubmit(context.Context, aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": "chinadatapay-seedance"}}, nil
}
func (d *metadataDriver) ParseSubmit(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	d.submitParsed = true
	if resp.Metadata["provider"] != "chinadatapay-seedance" {
		d.t.Errorf("submit metadata lost: %v", resp.Metadata)
	}
	// A synchronous result keeps this regression test free of background polling.
	return aigc.GenerationSubmitResult{Status: aigc.StatusSucceeded, ProviderTaskID: "task-relay"}, nil
}
func (d *metadataDriver) PreparePoll(context.Context, aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": "chinadatapay-seedance"}}, nil
}
func (d *metadataDriver) ParsePoll(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	d.pollParsed = true
	if resp.Metadata["provider"] != "chinadatapay-seedance" {
		d.t.Errorf("poll metadata lost: %v", resp.Metadata)
	}
	return aigc.GenerationPollResult{Status: aigc.StatusRunning}, nil
}
func (d *metadataDriver) PrepareCancel(context.Context, aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": "chinadatapay-seedance"}}, nil
}
func (d *metadataDriver) ParseCancel(_ context.Context, resp aigc.GenerationExecutionResponse) error {
	d.cancelParsed = true
	if resp.Metadata["provider"] != "chinadatapay-seedance" {
		d.t.Errorf("cancel metadata lost: %v", resp.Metadata)
	}
	return nil
}

func TestCoordinatorPreservesAdapterIdentity(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryStore()
	driver := &metadataDriver{testDriver: testDriver{kind: aigc.ContentKindVideo, model: "chinadatapay/doubao-seedance-2-5"}, t: t}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{ID: "store-plugin", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: store}}},
		pluginhost.TestCapabilityRecord{ID: "aigc-openai-compat", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}},
	)
	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})
	gen, err := store.Create(ctx, aigc.GenerationCreateRequest{Draft: aigc.ContentGenerationDraft{ID: "metadata-test", Kind: aigc.ContentKindVideo, Model: driver.model, Metadata: map[string]any{"volcengine-asset": "keep"}}})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.AsyncSubmit(gen.ID)
	gen, err = store.Get(ctx, aigc.GenerationGetRequest{ID: gen.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !driver.submitParsed || gen.Status != aigc.StatusSucceeded || gen.Provider != "aigc-openai-compat" || gen.Metadata["aigc_adapter"] != "chinadatapay-seedance" || gen.Metadata["volcengine-asset"] != "keep" {
		t.Fatalf("adapter identity not persisted: %+v", gen)
	}
	gen, err = store.Patch(ctx, aigc.GenerationPatchRequest{ID: gen.ID, ExpectedRevision: gen.Revision, Set: map[string]any{"status": aigc.StatusRunning}})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.pollGeneration(ctx, store, driver, gen)
	if _, err := coordinator.CancelGeneration(ctx, gen.ID); err != nil {
		t.Fatal(err)
	}
	if !driver.pollParsed || !driver.cancelParsed {
		t.Fatal("poll/cancel parsers not exercised")
	}
}

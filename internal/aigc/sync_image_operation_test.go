package aigc

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type explicitOperationDriver struct {
	syncSuccessDriver
	operation string
}

func (d *explicitOperationDriver) PrepareSubmit(_ context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	d.operation, _ = input.Generation.Metadata["image_operation"].(string)
	return aigc.GenerationExecutionRequest{}, errors.New("test stopped before execution")
}

type replaceOperationMetadataMutator struct{}

func (*replaceOperationMetadataMutator) MutateContentGeneration(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Phase == aigc.PhaseBeforeCreate {
		draft := *req.Draft
		draft.Metadata = map[string]any{"image_operation": "generations"}
		return aigc.GenerationMutationResponse{Draft: &draft}, nil
	}
	if req.Phase == aigc.PhaseBeforeSubmit {
		gen := req.Generation
		gen.Metadata = nil
		return aigc.GenerationMutationResponse{Generation: &gen}, nil
	}
	return aigc.GenerationMutationResponse{}, nil
}

func TestSyncImageOperationSurvivesMetadataReplacement(t *testing.T) {
	driver := &explicitOperationDriver{syncSuccessDriver: syncSuccessDriver{kind: aigc.ContentKindImage, model: "gpt-image-2.6"}}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{ID: "driver", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}},
		pluginhost.TestCapabilityRecord{ID: "mutator", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationMutator: &replaceOperationMetadataMutator{}}}},
	)
	_, errMsg := NewSyncPipeline(host, nil).Execute(context.Background(), SyncImageRequest{
		Model: "gpt-image-2.6", Input: []byte(`{"prompt":"edit","images":["https://example.com/a.png"]}`),
		Metadata: map[string]any{"image_operation": "edits"},
	})
	if errMsg == nil || driver.operation != "edits" {
		t.Fatalf("trusted operation lost: operation=%q error=%v", driver.operation, errMsg)
	}
}

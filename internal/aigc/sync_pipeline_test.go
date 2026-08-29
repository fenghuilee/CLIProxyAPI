package aigc

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type testSyncImageExecutor struct {
	calls     int
	executeFn func(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage)
}

func (e *testSyncImageExecutor) ExecuteImageWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage) {
	e.calls++
	if e.executeFn != nil {
		return e.executeFn(ctx, handlerType, modelName, rawJSON, alt)
	}
	return []byte(`{"created":123456,"data":[{"url":"https://example.com/generated.png"}]}`), http.Header{}, nil
}

func TestSyncPipeline_ExecuteSuccess(t *testing.T) {
	store := newInMemoryStore()
	driver := &syncSuccessDriver{
		kind:  aigc.ContentKindImage,
		model: "dall-e-3",
	}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
		pluginhost.TestCapabilityRecord{
			ID:       "driver-plugin",
			Priority: 50,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver},
			},
		},
	)

	exec := &testSyncImageExecutor{}
	pipeline := NewSyncPipeline(host, nil)
	pipeline.SetImageExecutor(exec)

	req := SyncImageRequest{
		Model: "dall-e-3",
		Input: []byte(`{"prompt":"a red apple"}`),
	}

	result, errMsg := pipeline.Execute(context.Background(), req)
	if errMsg != nil {
		t.Fatalf("pipeline.Execute failed: %v", errMsg.Error)
	}

	if result.Model != "dall-e-3" {
		t.Errorf("got model %s, want dall-e-3", result.Model)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].URI != "https://example.com/sync.png" {
		t.Errorf("unexpected artifacts: %#v", result.Artifacts)
	}
	if exec.calls != 1 {
		t.Errorf("executor calls = %d, want 1", exec.calls)
	}

	// Verify store record
	gen, errGet := store.Get(context.Background(), aigc.GenerationGetRequest{ID: result.ID})
	if errGet != nil {
		t.Fatalf("failed to get stored gen: %v", errGet)
	}
	if gen.Status != aigc.StatusSucceeded {
		t.Errorf("gen.Status = %v, want succeeded", gen.Status)
	}
}

func TestSyncPipeline_BeforeCreateReject(t *testing.T) {
	mutator := &testRejectMutator{
		phaseToReject: aigc.PhaseBeforeCreate,
		errorCode:     "PromptPolicyViolation",
		errorMessage:  "prompt contains blocked keywords",
	}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "mutator-plugin",
			Priority: 80,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutator},
			},
		},
	)

	pipeline := NewSyncPipeline(host, nil)
	result, errMsg := pipeline.Execute(context.Background(), SyncImageRequest{
		Model: "dall-e-3",
		Input: json.RawMessage(`{"prompt":"blocked"}`),
	})

	if errMsg == nil {
		t.Fatalf("expected error when BeforeCreate rejects, got result: %+v", result)
	}
	if errMsg.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", errMsg.StatusCode)
	}
}

type testBeforeCompleteMutator struct{}

func (m *testBeforeCompleteMutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Phase == aigc.PhaseBeforeComplete {
		updatedGen := req.Generation
		updatedGen.Output = json.RawMessage(`[{"artifact_type":"output_image","uri":"https://tos.example.com/out.png"}]`)
		if updatedGen.Metadata == nil {
			updatedGen.Metadata = make(map[string]any)
		}
		updatedGen.Metadata["tos"] = "uploaded"
		arts := []aigc.ContentGenerationArtifact{
			{
				ArtifactType:    "output_image",
				StorageProvider: "tos",
				URI:             "https://tos.example.com/out.png",
			},
		}
		return aigc.GenerationMutationResponse{
			Generation: &updatedGen,
			Artifacts:  arts,
		}, nil
	}
	return aigc.GenerationMutationResponse{
		Draft:      req.Draft,
		Generation: &req.Generation,
		Artifacts:  req.Generation.Artifacts,
	}, nil
}

func TestSyncPipeline_BeforeCompleteMutation(t *testing.T) {
	store := newInMemoryStore()
	driver := &syncSuccessDriver{
		kind:  aigc.ContentKindImage,
		model: "dall-e-3",
	}
	mutator := &testBeforeCompleteMutator{}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
		pluginhost.TestCapabilityRecord{
			ID:       "driver-plugin",
			Priority: 50,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver},
			},
		},
		pluginhost.TestCapabilityRecord{
			ID:       "mutator-plugin",
			Priority: 80,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutator},
			},
		},
	)

	exec := &testSyncImageExecutor{}
	pipeline := NewSyncPipeline(host, nil)
	pipeline.SetImageExecutor(exec)

	req := SyncImageRequest{
		Model: "dall-e-3",
		Input: []byte(`{"prompt":"a blue sky"}`),
	}

	result, errMsg := pipeline.Execute(context.Background(), req)
	if errMsg != nil {
		t.Fatalf("pipeline.Execute failed: %v", errMsg.Error)
	}

	if len(result.Artifacts) != 1 || result.Artifacts[0].URI != "https://tos.example.com/out.png" {
		t.Fatalf("expected mutated artifact TOS URI, got: %+v", result.Artifacts)
	}

	// Verify store was patched with mutated output and metadata
	gen, errGet := store.Get(context.Background(), aigc.GenerationGetRequest{ID: result.ID})
	if errGet != nil {
		t.Fatalf("failed to get stored gen: %v", errGet)
	}
	if string(gen.Output) != `[{"artifact_type":"output_image","uri":"https://tos.example.com/out.png"}]` {
		t.Errorf("expected mutated output in store, got: %s", string(gen.Output))
	}
	if gen.Metadata == nil || gen.Metadata["tos"] != "uploaded" {
		t.Errorf("expected mutated metadata in store, got: %+v", gen.Metadata)
	}
}

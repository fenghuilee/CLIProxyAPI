package aigc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
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

type metadataSyncMutator struct {
	onMutate func(aigc.GenerationMutationRequest)
}

func (m *metadataSyncMutator) MutateContentGeneration(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	m.onMutate(req)
	return aigc.GenerationMutationResponse{}, nil
}

type metadataSyncDriver struct {
	syncSuccessDriver
	metadata map[string]any
	parseFn  func(context.Context, aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error)
}

func (d *metadataSyncDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	req, err := d.syncSuccessDriver.PrepareSubmit(ctx, input)
	req.Metadata = d.metadata
	return req, err
}

func (d *metadataSyncDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return d.parseFn(ctx, resp)
}

func TestSyncPipeline_ExecutionMetadata(t *testing.T) {
	for _, outcome := range []string{"success", "parse_error", "failed_status"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			store := newInMemoryStore()
			metadata := map[string]any{
				"provider": "prepared-adapter",
				"kind":     "image",
				"options":  map[string]any{"marker": "execution-only"},
			}
			driver := &metadataSyncDriver{
				syncSuccessDriver: syncSuccessDriver{kind: aigc.ContentKindImage, model: "dall-e-3"},
				metadata:          metadata,
			}
			parseCalls := 0
			driver.parseFn = func(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
				parseCalls++
				if !reflect.DeepEqual(resp.Metadata, metadata) {
					return aigc.GenerationSubmitResult{}, fmt.Errorf("execution metadata = %#v, want %#v", resp.Metadata, metadata)
				}
				if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Test-Response") != "upstream" || !strings.Contains(string(resp.Body), "generated.png") {
					t.Errorf("unexpected execution response: %+v", resp)
				}
				switch outcome {
				case "parse_error":
					return aigc.GenerationSubmitResult{}, fmt.Errorf("test parse failure")
				case "failed_status":
					return aigc.GenerationSubmitResult{Status: aigc.StatusFailed, ErrorCode: "TestProviderFailure", ErrorMessage: "test provider failure"}, nil
				default:
					return driver.syncSuccessDriver.ParseSubmit(ctx, resp)
				}
			}
			successHooks := 0
			mutator := &metadataSyncMutator{onMutate: func(req aigc.GenerationMutationRequest) {
				if req.Phase == aigc.PhaseBeforeComplete || req.Phase == aigc.PhaseOnSucceeded {
					successHooks++
				}
			}}
			host := pluginhost.NewTestHost(
				pluginhost.TestCapabilityRecord{ID: "store-plugin", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: store}}},
				pluginhost.TestCapabilityRecord{ID: "driver-plugin", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}},
				pluginhost.TestCapabilityRecord{ID: "mutator-plugin", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutator}}},
			)
			exec := &testSyncImageExecutor{executeFn: func(context.Context, string, string, []byte, string) ([]byte, http.Header, *interfaces.ErrorMessage) {
				return []byte(`{"data":[{"url":"https://example.com/generated.png"}]}`), http.Header{"X-Test-Response": []string{"upstream"}}, nil
			}}
			pipeline := NewSyncPipeline(host, nil)
			pipeline.SetImageExecutor(exec)
			result, errMsg := pipeline.Execute(ctx, SyncImageRequest{
				ID: "cg_metadata", Model: "dall-e-3", Input: []byte(`{"prompt":"a red apple"}`),
				Metadata: map[string]any{"provider": "request-provider"},
			})
			if exec.calls != 1 || parseCalls != 1 {
				t.Fatalf("executor calls = %d, parser calls = %d, want 1 each", exec.calls, parseCalls)
			}
			gen, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: "cg_metadata"})
			if errGet != nil {
				t.Fatal(errGet)
			}
			if outcome == "success" {
				if errMsg != nil {
					t.Fatalf("pipeline.Execute failed: %v", errMsg.Error)
				}
				if len(result.Artifacts) != 1 || result.Artifacts[0].URI != "https://example.com/sync.png" || !strings.Contains(string(result.RawBody), "generated.png") {
					t.Errorf("unexpected result: %+v", result)
				}
				if gen.Status != aigc.StatusSucceeded || gen.Stage != aigc.StageCompleted || gen.Progress != 100 || len(gen.Artifacts) != 1 || successHooks != 2 {
					t.Errorf("unexpected success state: %+v, hooks = %d", gen, successHooks)
				}
				return
			}
			wantCode, wantMessage := aigc.ErrCodeInternalServerError, "test parse failure"
			if outcome == "failed_status" {
				wantCode, wantMessage = "TestProviderFailure", "test provider failure"
			}
			if errMsg == nil || errMsg.StatusCode != http.StatusBadGateway || !strings.Contains(errMsg.Error.Error(), wantMessage) {
				t.Fatalf("expected 502 containing %q, got %+v", wantMessage, errMsg)
			}
			if gen.Status != aigc.StatusFailed || gen.Stage != aigc.StageFailed || gen.ErrorCode != wantCode || !strings.Contains(gen.ErrorMessage, wantMessage) || len(gen.Artifacts) != 0 || successHooks != 0 {
				t.Errorf("unexpected failure state: %+v, hooks = %d", gen, successHooks)
			}
		})
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

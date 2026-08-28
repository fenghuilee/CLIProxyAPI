package pluginhost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type mockStore struct {
	createdGen aigc.ContentGeneration
	getGen     aigc.ContentGeneration
	patchGen   aigc.ContentGeneration
}

func (m *mockStore) Create(ctx context.Context, req aigc.GenerationCreateRequest) (aigc.ContentGeneration, error) {
	return m.createdGen, nil
}
func (m *mockStore) Get(ctx context.Context, req aigc.GenerationGetRequest) (aigc.ContentGeneration, error) {
	return m.getGen, nil
}
func (m *mockStore) Patch(ctx context.Context, req aigc.GenerationPatchRequest) (aigc.ContentGeneration, error) {
	return m.patchGen, nil
}
func (m *mockStore) Claim(ctx context.Context, req aigc.GenerationClaimRequest) ([]aigc.ContentGeneration, error) {
	return []aigc.ContentGeneration{m.getGen}, nil
}
func (m *mockStore) Release(ctx context.Context, req aigc.GenerationReleaseRequest) error {
	return nil
}
func (m *mockStore) DeleteExpired(ctx context.Context, req aigc.GenerationDeleteExpiredRequest) (int64, error) {
	return 5, nil
}

type mockMutator struct {
	mutateFn func(context.Context, aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error)
}

func (m *mockMutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if m.mutateFn != nil {
		return m.mutateFn(ctx, req)
	}
	return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
}

type mockDriver struct {
	supportsFn func(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error)
}

func (d *mockDriver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	if d.supportsFn != nil {
		return d.supportsFn(ctx, req)
	}
	return aigc.GenerationSupportResponse{Supported: true}, nil
}
func (d *mockDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{URL: "https://api.example.com/submit"}, nil
}
func (d *mockDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{ProviderTaskID: "task-1"}, nil
}
func (d *mockDriver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{URL: "https://api.example.com/poll"}, nil
}
func (d *mockDriver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{Status: aigc.StatusSucceeded, Stage: aigc.StageCompleted, Progress: 100}, nil
}
func (d *mockDriver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{URL: "https://api.example.com/cancel"}, nil
}
func (d *mockDriver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

type mockObserver struct {
	events []aigc.ContentGenerationEvent
}

func (o *mockObserver) OnContentGenerationEvent(ctx context.Context, event aigc.ContentGenerationEvent) error {
	o.events = append(o.events, event)
	return nil
}

func TestContentGenerationStoreExclusivityAndPriority(t *testing.T) {
	storeLow := &mockStore{createdGen: aigc.ContentGeneration{ID: "low-store"}}
	storeHigh := &mockStore{createdGen: aigc.ContentGeneration{ID: "high-store"}}

	host := newHostWithRecords(
		capabilityRecord{
			id:       "store-low",
			priority: 10,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: storeLow}},
		},
		capabilityRecord{
			id:       "store-high",
			priority: 100,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: storeHigh}},
		},
	)

	store, pluginID, ok := host.ContentGenerationStore()
	if !ok {
		t.Fatalf("expected store to be found")
	}
	if pluginID != "store-high" {
		t.Errorf("got pluginID %q, want store-high", pluginID)
	}

	created, err := store.Create(context.Background(), aigc.GenerationCreateRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID != "high-store" {
		t.Errorf("got created ID %q, want high-store", created.ID)
	}
}

func TestContentGenerationMutatorPriorityAndChaining(t *testing.T) {
	callOrder := make([]string, 0)
	mutatorHigh := &mockMutator{
		mutateFn: func(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
			callOrder = append(callOrder, "high")
			draft := req.Draft
			draft.Input = json.RawMessage(`{"prompt":"transformed-by-high"}`)
			return aigc.GenerationMutationResponse{Draft: draft}, nil
		},
	}
	mutatorLow := &mockMutator{
		mutateFn: func(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
			callOrder = append(callOrder, "low")
			if string(req.Draft.Input) != `{"prompt":"transformed-by-high"}` {
				t.Errorf("low mutator did not receive high mutator output: %s", string(req.Draft.Input))
			}
			draft := req.Draft
			draft.Input = json.RawMessage(`{"prompt":"transformed-by-low"}`)
			return aigc.GenerationMutationResponse{Draft: draft}, nil
		},
	}

	host := newHostWithRecords(
		capabilityRecord{
			id:       "mut-low",
			priority: 10,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutatorLow}},
		},
		capabilityRecord{
			id:       "mut-high",
			priority: 200,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutatorHigh}},
		},
	)

	initialDraft := &aigc.ContentGenerationDraft{
		ID:    "cg_test",
		Input: json.RawMessage(`{"prompt":"original"}`),
	}

	resp, err := host.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: initialDraft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(callOrder) != 2 || callOrder[0] != "high" || callOrder[1] != "low" {
		t.Errorf("unexpected call order: %v", callOrder)
	}
	if string(resp.Draft.Input) != `{"prompt":"transformed-by-low"}` {
		t.Errorf("unexpected final draft input: %s", string(resp.Draft.Input))
	}
}

func TestContentGenerationMutatorRejection(t *testing.T) {
	mutatorReject := &mockMutator{
		mutateFn: func(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
			return aigc.GenerationMutationResponse{
				Reject:       true,
				ErrorCode:    "BlockedByPolicy",
				ErrorMessage: "prompt violates policy",
			}, nil
		},
	}
	mutatorLow := &mockMutator{
		mutateFn: func(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
			t.Errorf("lower mutator should not be called when high mutator rejects")
			return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
		},
	}

	host := newHostWithRecords(
		capabilityRecord{
			id:       "mut-low",
			priority: 10,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutatorLow}},
		},
		capabilityRecord{
			id:       "mut-high",
			priority: 200,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutatorReject}},
		},
	)

	resp, err := host.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: &aigc.ContentGenerationDraft{ID: "cg_test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Reject {
		t.Errorf("expected reject to be true")
	}
	if resp.ErrorCode != "BlockedByPolicy" {
		t.Errorf("got error code %q, want BlockedByPolicy", resp.ErrorCode)
	}
}

func TestContentGenerationDriverSelection(t *testing.T) {
	videoDriver := &mockDriver{
		supportsFn: func(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
			if req.Kind == aigc.ContentKindVideo && req.Model == "doubao-seedance" {
				return aigc.GenerationSupportResponse{Supported: true, Provider: "volcengine"}, nil
			}
			return aigc.GenerationSupportResponse{Supported: false}, nil
		},
	}
	imageDriver := &mockDriver{
		supportsFn: func(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
			if req.Kind == aigc.ContentKindImage && req.Model == "grok-2-image" {
				return aigc.GenerationSupportResponse{Supported: true, Provider: "xai"}, nil
			}
			return aigc.GenerationSupportResponse{Supported: false}, nil
		},
	}

	host := newHostWithRecords(
		capabilityRecord{
			id:       "driver-video",
			priority: 50,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: videoDriver}},
		},
		capabilityRecord{
			id:       "driver-image",
			priority: 60,
			plugin:   pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: imageDriver}},
		},
	)

	_, pluginID, ok := host.ContentGenerationDriverFor(context.Background(), aigc.ContentKindVideo, "doubao-seedance")
	if !ok || pluginID != "driver-video" {
		t.Errorf("expected driver-video, got %q, ok=%v", pluginID, ok)
	}

	_, pluginID, ok = host.ContentGenerationDriverFor(context.Background(), aigc.ContentKindImage, "grok-2-image")
	if !ok || pluginID != "driver-image" {
		t.Errorf("expected driver-image, got %q, ok=%v", pluginID, ok)
	}

	_, _, ok = host.ContentGenerationDriverFor(context.Background(), aigc.ContentKindAudio, "unknown")
	if ok {
		t.Errorf("expected unsupported for unknown model")
	}
}

func TestContentGenerationRPCRoundTrip(t *testing.T) {
	storeMock := &mockStore{
		createdGen: aigc.ContentGeneration{ID: "rpc-gen-1", Status: aigc.StatusAccepted},
		getGen:     aigc.ContentGeneration{ID: "rpc-gen-1", Status: aigc.StatusRunning, Progress: 50},
	}
	mutatorMock := &mockMutator{
		mutateFn: func(_ context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
			draft := req.Draft
			draft.Model = "transformed-model"
			return aigc.GenerationMutationResponse{Draft: draft}, nil
		},
	}

	lookup := newTestSymbolLookup(&testPlugin{
		registerResult: pluginapi.Plugin{
			Metadata: pluginapi.Metadata{
				Name:             "aigc-test-plugin",
				Version:          "1.0.0",
				Author:           "Test",
				GitHubRepository: "https://github.com/example/plugin",
			},
			SchemaVersion: pluginabi.SchemaVersionAIGC,
			Capabilities: pluginapi.Capabilities{
				ContentGenerationStore:   storeMock,
				ContentGenerationMutator: mutatorMock,
			},
		},
	})

	plugin, err := registerRPCPlugin(context.Background(), nil, "aigc-test", lookup, pluginabi.MethodPluginRegister, nil)
	if err != nil {
		t.Fatalf("registerRPCPlugin failed: %v", err)
	}

	if plugin.Capabilities.ContentGenerationStore == nil {
		t.Fatalf("expected ContentGenerationStore capability")
	}
	if plugin.Capabilities.ContentGenerationMutator == nil {
		t.Fatalf("expected ContentGenerationMutator capability")
	}

	created, err := plugin.Capabilities.ContentGenerationStore.Create(context.Background(), aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{ID: "rpc-gen-1"},
	})
	if err != nil {
		t.Fatalf("store.Create failed: %v", err)
	}
	if created.ID != "rpc-gen-1" || created.Status != aigc.StatusAccepted {
		t.Errorf("unexpected created gen: %+v", created)
	}

	mutResp, err := plugin.Capabilities.ContentGenerationMutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: &aigc.ContentGenerationDraft{ID: "draft-1", Model: "original-model"},
	})
	if err != nil {
		t.Fatalf("mutate failed: %v", err)
	}
	if mutResp.Draft.Model != "transformed-model" {
		t.Errorf("got model %q, want transformed-model", mutResp.Draft.Model)
	}
}

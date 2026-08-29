package aigc

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type inMemoryStore struct {
	mu          sync.Mutex
	generations map[string]aigc.ContentGeneration
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{generations: make(map[string]aigc.ContentGeneration)}
}

func (s *inMemoryStore) Create(ctx context.Context, req aigc.GenerationCreateRequest) (aigc.ContentGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen := aigc.ContentGeneration{
		ID:        req.Draft.ID,
		Kind:      req.Draft.Kind,
		Model:     req.Draft.Model,
		Status:    aigc.StatusAccepted,
		Stage:     aigc.StageCreated,
		Progress:  0,
		Revision:  1,
		APIKey:    req.Draft.APIKey,
		ClientIP:  req.Draft.ClientIP,
		Input:     req.Draft.Input,
		Metadata:  req.Draft.Metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.generations[gen.ID] = gen
	return gen, nil
}

func (s *inMemoryStore) Get(ctx context.Context, req aigc.GenerationGetRequest) (aigc.ContentGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen, ok := s.generations[req.ID]
	if !ok {
		return aigc.ContentGeneration{}, aigc.ErrNotFound
	}
	return gen, nil
}

func (s *inMemoryStore) Patch(ctx context.Context, req aigc.GenerationPatchRequest) (aigc.ContentGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen, ok := s.generations[req.ID]
	if !ok {
		return aigc.ContentGeneration{}, aigc.ErrNotFound
	}
	if req.ExpectedRevision > 0 && gen.Revision != req.ExpectedRevision {
		return aigc.ContentGeneration{}, aigc.ErrRevisionConflict
	}
	for k, v := range req.Set {
		switch k {
		case "status":
			gen.Status = v.(aigc.GenerationStatus)
		case "stage":
			gen.Stage = v.(aigc.GenerationStage)
		case "progress":
			gen.Progress = v.(int)
		case "provider":
			gen.Provider = v.(string)
		case "provider_task_id":
			gen.ProviderTaskID = v.(string)
		case "error_code":
			gen.ErrorCode = v.(string)
		case "error_message":
			gen.ErrorMessage = v.(string)
		case "output":
			if raw, ok := v.(json.RawMessage); ok {
				gen.Output = raw
			} else if b, ok := v.([]byte); ok {
				gen.Output = b
			}
		case "metadata":
			if m, ok := v.(map[string]any); ok {
				gen.Metadata = m
			}
		}
	}
	if len(req.Artifacts) > 0 {
		gen.Artifacts = append(gen.Artifacts, req.Artifacts...)
	}
	gen.Revision++
	gen.UpdatedAt = time.Now()
	s.generations[gen.ID] = gen
	return gen, nil
}

func (s *inMemoryStore) Claim(ctx context.Context, req aigc.GenerationClaimRequest) ([]aigc.ContentGeneration, error) {
	return nil, nil
}
func (s *inMemoryStore) Release(ctx context.Context, req aigc.GenerationReleaseRequest) error {
	return nil
}
func (s *inMemoryStore) DeleteExpired(ctx context.Context, req aigc.GenerationDeleteExpiredRequest) (int64, error) {
	return 0, nil
}

type testDriver struct {
	kind     aigc.ContentKind
	model    string
	submitFn func(aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error)
	pollFn   func(aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error)
}

func (d *testDriver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	if req.Kind == d.kind && req.Model == d.model {
		return aigc.GenerationSupportResponse{Supported: true, Provider: "test-provider"}, nil
	}
	return aigc.GenerationSupportResponse{Supported: false}, nil
}

func (d *testDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	if d.submitFn != nil {
		return d.submitFn(input)
	}
	return aigc.GenerationExecutionRequest{Body: []byte(`{"task":"submit"}`)}, nil
}

func (d *testDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{
		ProviderTaskID: "prov-task-123",
		Status:         aigc.StatusRunning,
		Stage:          aigc.StageProviderRunning,
		Progress:       10,
	}, nil
}

func (d *testDriver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	if d.pollFn != nil {
		return d.pollFn(input)
	}
	return aigc.GenerationExecutionRequest{Body: []byte(`{"task":"poll"}`)}, nil
}

func (d *testDriver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{
		Status:   aigc.StatusSucceeded,
		Stage:    aigc.StageCompleted,
		Progress: 100,
		Artifacts: []aigc.ContentGenerationArtifact{
			{ArtifactType: "output_video", URI: "https://example.com/result.mp4"},
		},
	}, nil
}

func (d *testDriver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Body: []byte(`{"task":"cancel"}`)}, nil
}

func (d *testDriver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

type testExecutor struct {
	mu        sync.Mutex
	calls     int
	executeFn func(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage)
}

func (e *testExecutor) ExecuteWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, http.Header, *interfaces.ErrorMessage) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	if e.executeFn != nil {
		return e.executeFn(ctx, handlerType, modelName, rawJSON, alt)
	}
	return []byte(`{"ok":true}`), http.Header{}, nil
}

func TestCoordinatorCreateAndGet(t *testing.T) {
	store := newInMemoryStore()
	driver := &testDriver{
		kind:  aigc.ContentKindVideo,
		model: "doubao-seedance-2-0-mini",
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

	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})

	draft := aigc.ContentGenerationDraft{
		Model: "doubao-seedance-2-0-mini",
		Input: json.RawMessage(`{"prompt":"a cinematic drone shot"}`),
	}

	created, err := coordinator.CreateGeneration(context.Background(), aigc.ContentKindVideo, draft)
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}
	if created.Status != aigc.StatusAccepted {
		t.Errorf("status = %v, want %v", created.Status, aigc.StatusAccepted)
	}
	if created.Kind != aigc.ContentKindVideo {
		t.Errorf("kind = %v, want %v", created.Kind, aigc.ContentKindVideo)
	}

	// Wait for async submit to finish
	time.Sleep(50 * time.Millisecond)

	// Fetch generation -> should trigger poll
	polled, err := coordinator.GetGeneration(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetGeneration failed: %v", err)
	}

	if polled.Status != aigc.StatusSucceeded {
		t.Errorf("status = %v, want %v", polled.Status, aigc.StatusSucceeded)
	}
	if polled.Progress != 100 {
		t.Errorf("progress = %v, want 100", polled.Progress)
	}
}

func TestCoordinatorCancel(t *testing.T) {
	store := newInMemoryStore()
	driver := &testDriver{
		kind:  aigc.ContentKindVideo,
		model: "doubao-seedance-2-0-mini",
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

	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})

	created, err := coordinator.CreateGeneration(context.Background(), aigc.ContentKindVideo, aigc.ContentGenerationDraft{
		Model: "doubao-seedance-2-0-mini",
	})
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}

	canceled, err := coordinator.CancelGeneration(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("CancelGeneration failed: %v", err)
	}
	if canceled.Status != aigc.StatusCanceled {
		t.Errorf("status = %v, want %v", canceled.Status, aigc.StatusCanceled)
	}
}

func TestCoordinator_AutoPollingWithoutGet(t *testing.T) {
	store := newInMemoryStore()
	driver := &testDriver{
		kind:  aigc.ContentKindVideo,
		model: "doubao-seedance-2-0-mini",
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

	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})
	coordinator.SetPollInterval(10 * time.Millisecond)

	draft := aigc.ContentGenerationDraft{
		Model: "doubao-seedance-2-0-mini",
		Input: json.RawMessage(`{"prompt":"a cinematic drone shot"}`),
	}

	created, err := coordinator.CreateGeneration(context.Background(), aigc.ContentKindVideo, draft)
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}
	if created.Status != aigc.StatusAccepted {
		t.Fatalf("status = %v, want %v", created.Status, aigc.StatusAccepted)
	}

	// Do NOT call coordinator.GetGeneration! Instead, wait and verify that background polling
	// updates the store directly.
	var finalGen aigc.ContentGeneration
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gen, errGet := store.Get(context.Background(), aigc.GenerationGetRequest{ID: created.ID})
		if errGet == nil && gen.Status == aigc.StatusSucceeded {
			finalGen = gen
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalGen.Status != aigc.StatusSucceeded {
		t.Fatalf("expected generation to be auto-completed to succeeded by background polling, got status=%v", finalGen.Status)
	}
	if finalGen.Progress != 100 {
		t.Errorf("progress = %v, want 100", finalGen.Progress)
	}
}

type syncSuccessDriver struct {
	kind  aigc.ContentKind
	model string
}

func (d *syncSuccessDriver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	if req.Kind == d.kind && req.Model == d.model {
		return aigc.GenerationSupportResponse{Supported: true, Provider: "sync-provider"}, nil
	}
	return aigc.GenerationSupportResponse{Supported: false}, nil
}

func (d *syncSuccessDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    "/services/aigc/multimodal-generation/generation",
		Body:   []byte(`{"task":"sync-image"}`),
	}, nil
}

func (d *syncSuccessDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{
		Status:   aigc.StatusSucceeded,
		Stage:    aigc.StageCompleted,
		Progress: 100,
		Output:   json.RawMessage(`[{"type":"output_image","url":"https://example.com/sync.png"}]`),
		Artifacts: []aigc.ContentGenerationArtifact{
			{ArtifactType: "output_image", URI: "https://example.com/sync.png"},
		},
	}, nil
}

func (d *syncSuccessDriver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, nil
}

func (d *syncSuccessDriver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{}, nil
}

func (d *syncSuccessDriver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, nil
}

func (d *syncSuccessDriver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func TestCoordinator_SubmitDirectSucceeded(t *testing.T) {
	store := newInMemoryStore()
	driver := &syncSuccessDriver{
		kind:  aigc.ContentKindImage,
		model: "qwen/qwen-image-3.0-pro",
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

	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})

	draft := aigc.ContentGenerationDraft{
		Model: "qwen/qwen-image-3.0-pro",
		Input: json.RawMessage(`{"prompt":"a beautiful painting"}`),
	}

	created, err := coordinator.CreateGeneration(context.Background(), aigc.ContentKindImage, draft)
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}
	if created.Status != aigc.StatusAccepted {
		t.Fatalf("status = %v, want %v", created.Status, aigc.StatusAccepted)
	}

	var finalGen aigc.ContentGeneration
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gen, errGet := store.Get(context.Background(), aigc.GenerationGetRequest{ID: created.ID})
		if errGet == nil && gen.Status == aigc.StatusSucceeded {
			finalGen = gen
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalGen.Status != aigc.StatusSucceeded {
		t.Fatalf("expected generation to be direct-completed to succeeded, got status=%v", finalGen.Status)
	}
	if finalGen.Progress != 100 {
		t.Errorf("progress = %v, want 100", finalGen.Progress)
	}
	if len(finalGen.Artifacts) != 1 || finalGen.Artifacts[0].URI != "https://example.com/sync.png" {
		t.Errorf("unexpected artifacts: %#v", finalGen.Artifacts)
	}
}

type testRejectMutator struct {
	phaseToReject aigc.GenerationLifecyclePhase
	errorCode     string
	errorMessage  string
}

func (m *testRejectMutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Phase == m.phaseToReject {
		return aigc.GenerationMutationResponse{
			Reject:       true,
			ErrorCode:    m.errorCode,
			ErrorMessage: m.errorMessage,
		}, nil
	}
	return aigc.GenerationMutationResponse{Draft: req.Draft, Generation: &req.Generation}, nil
}

type testModifyingMutator struct {
	modifiedModel string
}

func (m *testModifyingMutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Phase == aigc.PhaseBeforeSubmit {
		mutated := req.Generation
		mutated.Model = m.modifiedModel
		return aigc.GenerationMutationResponse{Generation: &mutated}, nil
	}
	return aigc.GenerationMutationResponse{Draft: req.Draft, Generation: &req.Generation}, nil
}

func TestCoordinator_BeforeSubmit_RejectedByMutator(t *testing.T) {
	store := newInMemoryStore()
	driver := &testDriver{
		kind:  aigc.ContentKindVideo,
		model: "volcengine/doubao-seedance-1-5-pro",
	}
	mutator := &testRejectMutator{
		phaseToReject: aigc.PhaseBeforeSubmit,
		errorCode:     "AssetProcessingTimeout",
		errorMessage:  "timed out waiting for assets",
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
		pluginhost.TestCapabilityRecord{
			ID:       "mutator-plugin",
			Priority: 80,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutator},
			},
		},
	)

	exec := &testExecutor{}
	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(exec)

	draft := aigc.ContentGenerationDraft{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Input: json.RawMessage(`{"prompt":"animate"}`),
	}

	created, err := coordinator.CreateGeneration(context.Background(), aigc.ContentKindVideo, draft)
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}

	var finalGen aigc.ContentGeneration
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gen, errGet := store.Get(context.Background(), aigc.GenerationGetRequest{ID: created.ID})
		if errGet == nil && gen.Status == aigc.StatusFailed {
			finalGen = gen
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalGen.Status != aigc.StatusFailed {
		t.Fatalf("expected generation to fail when BeforeSubmit rejects, got status=%v", finalGen.Status)
	}
	if finalGen.ErrorCode != "AssetProcessingTimeout" {
		t.Errorf("error_code = %q, want AssetProcessingTimeout", finalGen.ErrorCode)
	}
	if finalGen.ErrorMessage != "timed out waiting for assets" {
		t.Errorf("error_message = %q, want timed out waiting for assets", finalGen.ErrorMessage)
	}
	if exec.calls != 0 {
		t.Errorf("executor was called %d times, want 0 when BeforeSubmit rejects", exec.calls)
	}
}

func TestCoordinator_BeforeSubmit_ModifiesGenerationBeforePrepareSubmit(t *testing.T) {
	store := newInMemoryStore()
	driver := &testDriver{
		kind:  aigc.ContentKindVideo,
		model: "mutated-model",
	}
	mutator := &testModifyingMutator{
		modifiedModel: "mutated-model",
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
		pluginhost.TestCapabilityRecord{
			ID:       "mutator-plugin",
			Priority: 80,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationMutator: mutator},
			},
		},
	)

	exec := &testExecutor{}
	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(exec)

	draft := aigc.ContentGenerationDraft{
		Model: "original-model",
		Input: json.RawMessage(`{"prompt":"animate"}`),
	}

	created, err := coordinator.CreateGeneration(context.Background(), aigc.ContentKindVideo, draft)
	if err != nil {
		t.Fatalf("CreateGeneration failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var finalGen aigc.ContentGeneration
	for time.Now().Before(deadline) {
		gen, errGet := store.Get(context.Background(), aigc.GenerationGetRequest{ID: created.ID})
		if errGet == nil && gen.Status == aigc.StatusRunning {
			finalGen = gen
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalGen.Status != aigc.StatusRunning {
		t.Fatalf("expected running generation, got status=%v", finalGen.Status)
	}
	if exec.calls != 1 {
		t.Errorf("executor calls = %d, want 1", exec.calls)
	}
}

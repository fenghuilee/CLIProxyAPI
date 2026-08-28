package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type testAIGCStore struct {
	mu          sync.Mutex
	generations map[string]aigc.ContentGeneration
}

func newTestAIGCStore() *testAIGCStore {
	return &testAIGCStore{generations: make(map[string]aigc.ContentGeneration)}
}

func (s *testAIGCStore) Create(ctx context.Context, req aigc.GenerationCreateRequest) (aigc.ContentGeneration, error) {
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
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.generations[gen.ID] = gen
	return gen, nil
}

func (s *testAIGCStore) Get(ctx context.Context, req aigc.GenerationGetRequest) (aigc.ContentGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen, ok := s.generations[req.ID]
	if !ok {
		return aigc.ContentGeneration{}, aigc.ErrNotFound
	}
	return gen, nil
}

func (s *testAIGCStore) Patch(ctx context.Context, req aigc.GenerationPatchRequest) (aigc.ContentGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen, ok := s.generations[req.ID]
	if !ok {
		return aigc.ContentGeneration{}, aigc.ErrNotFound
	}
	for k, v := range req.Set {
		switch k {
		case "status":
			gen.Status = v.(aigc.GenerationStatus)
		case "stage":
			gen.Stage = v.(aigc.GenerationStage)
		case "progress":
			gen.Progress = v.(int)
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

func (s *testAIGCStore) Claim(ctx context.Context, req aigc.GenerationClaimRequest) ([]aigc.ContentGeneration, error) {
	return nil, nil
}
func (s *testAIGCStore) Release(ctx context.Context, req aigc.GenerationReleaseRequest) error {
	return nil
}
func (s *testAIGCStore) DeleteExpired(ctx context.Context, req aigc.GenerationDeleteExpiredRequest) (int64, error) {
	return 0, nil
}

type testAIGCDriver struct{}

func (d *testAIGCDriver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	return aigc.GenerationSupportResponse{Supported: true, Provider: "test-provider"}, nil
}
func (d *testAIGCDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Method: "POST", URL: "/test/submit", Body: []byte(`{}`)}, nil
}
func (d *testAIGCDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{Status: aigc.StatusRunning, ProviderTaskID: "task_1"}, nil
}
func (d *testAIGCDriver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Method: "GET", URL: "/test/poll"}, nil
}
func (d *testAIGCDriver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{Status: aigc.StatusRunning}, nil
}
func (d *testAIGCDriver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Method: "POST", URL: "/test/cancel"}, nil
}
func (d *testAIGCDriver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func TestAIGCVideoEndpoints(t *testing.T) {
	store := newTestAIGCStore()
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{
					ContentGenerationStore:  store,
					ContentGenerationDriver: &testAIGCDriver{},
				},
			},
		},
	)

	server := newTestServerWithOptions(t, WithPluginHost(host))

	// 1. Create video generation
	createPayload := []byte(`{"model":"doubao-seedance-2-0-mini","prompt":"a mountain landscape"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/videos", bytes.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer test-key")
	createReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"16:9","duration":5}`)
	createRR := httptest.NewRecorder()
	server.engine.ServeHTTP(createRR, createReq)

	if createRR.Code != http.StatusAccepted {
		t.Fatalf("POST /aigc/v1/videos code = %d, want %d, body = %s", createRR.Code, http.StatusAccepted, createRR.Body.String())
	}

	var createResp aigcResponse
	if err := json.Unmarshal(createRR.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	if createResp.ID == "" || createResp.Status != "accepted" || createResp.Kind != "video" {
		t.Errorf("unexpected create response: %+v", createResp)
	}

	// 2. Get video generation
	getReq := httptest.NewRequest(http.MethodGet, "/aigc/v1/videos/"+createResp.ID, nil)
	getReq.Header.Set("Authorization", "Bearer test-key")
	getRR := httptest.NewRecorder()
	server.engine.ServeHTTP(getRR, getReq)

	if getRR.Code != http.StatusOK {
		t.Fatalf("GET /aigc/v1/videos/:id code = %d, want %d, body = %s", getRR.Code, http.StatusOK, getRR.Body.String())
	}

	var getResp aigcResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if getResp.ID != createResp.ID {
		t.Errorf("get ID = %q, want %q", getResp.ID, createResp.ID)
	}

	// 2.1 Verify artifacts formatting when Artifacts field is populated
	store.mu.Lock()
	gOutput := store.generations[createResp.ID]
	gOutput.Status = aigc.StatusSucceeded
	gOutput.Stage = aigc.StageCompleted
	gOutput.Progress = 100
	gOutput.Artifacts = []aigc.ContentGenerationArtifact{
		{ArtifactType: "output_video", URI: "https://example.com/video.mp4"},
		{ArtifactType: "last_frame", URI: "https://example.com/last_frame.png"},
	}
	store.generations[createResp.ID] = gOutput
	store.mu.Unlock()

	getReqArtifacts := httptest.NewRequest(http.MethodGet, "/aigc/v1/videos/"+createResp.ID, nil)
	getReqArtifacts.Header.Set("Authorization", "Bearer test-key")
	getRRArtifacts := httptest.NewRecorder()
	server.engine.ServeHTTP(getRRArtifacts, getReqArtifacts)

	var getArtifactsResp aigcResponse
	if err := json.Unmarshal(getRRArtifacts.Body.Bytes(), &getArtifactsResp); err != nil {
		t.Fatalf("unmarshal get artifacts response: %v", err)
	}
	if len(getArtifactsResp.Artifacts) != 2 || getArtifactsResp.Artifacts[0].Type != "output_video" || getArtifactsResp.Artifacts[1].Type != "last_frame" {
		t.Errorf("unexpected artifacts response: %+v", getArtifactsResp.Artifacts)
	}

	// 3. Cancel video generation
	store.mu.Lock()
	g := store.generations[createResp.ID]
	g.Status = aigc.StatusRunning
	store.generations[createResp.ID] = g
	store.mu.Unlock()

	cancelReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/videos/"+createResp.ID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer test-key")
	cancelRR := httptest.NewRecorder()
	server.engine.ServeHTTP(cancelRR, cancelReq)

	if cancelRR.Code != http.StatusOK {
		t.Fatalf("POST /aigc/v1/videos/:id/cancel code = %d, want %d, body = %s", cancelRR.Code, http.StatusOK, cancelRR.Body.String())
	}

	var cancelResp aigcResponse
	if err := json.Unmarshal(cancelRR.Body.Bytes(), &cancelResp); err != nil {
		t.Fatalf("unmarshal cancel response: %v", err)
	}
	if cancelResp.Status != "canceled" {
		t.Errorf("cancel status = %q, want canceled", cancelResp.Status)
	}
}

func TestAIGCImageEndpoints(t *testing.T) {
	store := newTestAIGCStore()
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
	)

	server := newTestServerWithOptions(t, WithPluginHost(host))

	// Create image generation
	createPayload := []byte(`{"model":"grok-2-image","prompt":"a cute robotic puppy"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/images", bytes.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer test-key")
	createReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"1:1","width":1024,"height":1024}`)
	createRR := httptest.NewRecorder()
	server.engine.ServeHTTP(createRR, createReq)

	if createRR.Code != http.StatusAccepted {
		t.Fatalf("POST /aigc/v1/images code = %d, want %d, body = %s", createRR.Code, http.StatusAccepted, createRR.Body.String())
	}

	var createResp aigcResponse
	if err := json.Unmarshal(createRR.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	if createResp.ID == "" || createResp.Status != "accepted" || createResp.Kind != "image" {
		t.Errorf("unexpected create response: %+v", createResp)
	}
}

type testQwenImageDriver struct{}

func (d *testQwenImageDriver) Supports(ctx context.Context, req aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	if req.Kind == aigc.ContentKindImage && (req.Model == "qwen/qwen-image-3.0-pro" || req.Model == "qwen-image-3.0-pro") {
		return aigc.GenerationSupportResponse{Supported: true, Provider: "qwen-image"}, nil
	}
	return aigc.GenerationSupportResponse{Supported: false}, nil
}

func (d *testQwenImageDriver) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    "/services/aigc/multimodal-generation/generation",
		Body:   []byte(`{"model":"qwen-image-3.0-pro"}`),
	}, nil
}

func (d *testQwenImageDriver) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{
		Status:   aigc.StatusSucceeded,
		Stage:    aigc.StageCompleted,
		Progress: 100,
		Artifacts: []aigc.ContentGenerationArtifact{
			{
				ArtifactType: "output_image",
				URI:          "https://dashscope-result.example.com/sync_output.png",
			},
		},
	}, nil
}

func (d *testQwenImageDriver) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, nil
}

func (d *testQwenImageDriver) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{}, nil
}

func (d *testQwenImageDriver) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, nil
}

func (d *testQwenImageDriver) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

type testImageExecutor struct{}

func (e *testImageExecutor) Identifier() string { return "openai" }

func (e *testImageExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{
		Payload: []byte(`{"output":{"choices":[{"message":{"content":[{"image":"https://dashscope-result.example.com/sync_output.png"}]}}]},"usage":{"input_tokens":88,"output_tokens":158,"total_tokens":246}}`),
		Headers: http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func (e *testImageExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e *testImageExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

func (e *testImageExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *testImageExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestAIGCImageSyncEndpoints(t *testing.T) {
	store := newTestAIGCStore()
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
		pluginhost.TestCapabilityRecord{
			ID:       "qwen-image-plugin",
			Priority: 50,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationDriver: &testQwenImageDriver{}},
			},
		},
	)

	server := newTestServerWithOptions(t, WithPluginHost(host))
	server.handlers.AuthManager.RegisterExecutor(&testImageExecutor{})

	auth := &cliproxyauth.Auth{
		ID:       "auth-test-qwen",
		Provider: "openai",
		Status:   cliproxyauth.StatusActive,
		Attributes: map[string]string{
			cliproxyauth.AttributeAPIKey: "sk-dashscope-test",
		},
	}
	if _, err := server.handlers.AuthManager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{
		{ID: "qwen/qwen-image-3.0-pro"},
		{ID: "qwen-image-3.0-pro"},
	})

	// 1. Test POST /aigc/v1/images/generations (Synchronous)
	genPayload := []byte(`{"model":"qwen/qwen-image-3.0-pro","prompt":"a cute golden retriever on the beach","size":"1024x1024","n":1}`)
	genReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/images/generations", bytes.NewReader(genPayload))
	genReq.Header.Set("Content-Type", "application/json")
	genReq.Header.Set("Authorization", "Bearer test-key")
	genReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"1:1","width":1024,"height":1024}`)
	genRR := httptest.NewRecorder()
	server.engine.ServeHTTP(genRR, genReq)

	if genRR.Code != http.StatusOK {
		t.Fatalf("POST /aigc/v1/images/generations code = %d, want %d, body = %s", genRR.Code, http.StatusOK, genRR.Body.String())
	}

	var genResp openAIImageResponse
	if err := json.Unmarshal(genRR.Body.Bytes(), &genResp); err != nil {
		t.Fatalf("unmarshal sync image response: %v", err)
	}

	if len(genResp.Data) != 1 || genResp.Data[0].URL != "https://dashscope-result.example.com/sync_output.png" {
		t.Errorf("unexpected sync image response data: %+v", genResp)
	}
	if genResp.Usage == nil || genResp.Usage.InputTokens != 88 || genResp.Usage.OutputTokens != 158 || genResp.Usage.TotalTokens != 246 {
		t.Errorf("unexpected sync image response usage: %+v", genResp.Usage)
	}
	if genResp.Usage.OutputTokensDetails == nil || genResp.Usage.OutputTokensDetails.ImageTokens != 158 {
		t.Errorf("unexpected output tokens details: %+v", genResp.Usage.OutputTokensDetails)
	}

	// Verify that generation record and artifacts were persisted to store
	if len(store.generations) < 1 {
		t.Fatalf("expected at least 1 generation in store, got %d", len(store.generations))
	}
	var lastGen aigc.ContentGeneration
	for _, g := range store.generations {
		lastGen = g
	}
	if lastGen.Status != aigc.StatusSucceeded || len(lastGen.Artifacts) != 1 {
		t.Fatalf("persisted generation = %+v", lastGen)
	}

	// 2. Test POST /aigc/v1/images/edits (Synchronous)
	editPayload := []byte(`{"model":"qwen/qwen-image-3.0-pro","prompt":"make it watercolor style","image":"https://example.com/input.png"}`)
	editReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/images/edits", bytes.NewReader(editPayload))
	editReq.Header.Set("Content-Type", "application/json")
	editReq.Header.Set("Authorization", "Bearer test-key")
	editReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"1:1","width":1024,"height":1024}`)
	editRR := httptest.NewRecorder()
	server.engine.ServeHTTP(editRR, editReq)

	if editRR.Code != http.StatusOK {
		t.Fatalf("POST /aigc/v1/images/edits code = %d, want %d, body = %s", editRR.Code, http.StatusOK, editRR.Body.String())
	}

	var editResp openAIImageResponse
	if err := json.Unmarshal(editRR.Body.Bytes(), &editResp); err != nil {
		t.Fatalf("unmarshal sync edit response: %v", err)
	}

	if len(editResp.Data) != 1 || editResp.Data[0].URL != "https://dashscope-result.example.com/sync_output.png" {
		t.Errorf("unexpected sync edit response data: %+v", editResp)
	}

	// 3. Test POST /aigc/v1/images/generations with response_format="b64_json"
	b64ReqPayload := []byte(`{"model":"qwen/qwen-image-3.0-pro","prompt":"cat","response_format":"b64_json"}`)
	b64Req := httptest.NewRequest(http.MethodPost, "/aigc/v1/images/generations", bytes.NewReader(b64ReqPayload))
	b64Req.Header.Set("Content-Type", "application/json")
	b64Req.Header.Set("Authorization", "Bearer test-key")
	b64Req.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"1:1","width":1024,"height":1024}`)
	b64RR := httptest.NewRecorder()
	server.engine.ServeHTTP(b64RR, b64Req)

	if b64RR.Code != http.StatusOK {
		t.Fatalf("POST /aigc/v1/images/generations (b64) code = %d, want %d, body = %s", b64RR.Code, http.StatusOK, b64RR.Body.String())
	}
	var b64Resp openAIImageResponse
	if err := json.Unmarshal(b64RR.Body.Bytes(), &b64Resp); err != nil {
		t.Fatalf("unmarshal b64 response: %v", err)
	}
	if len(b64Resp.Data) != 1 {
		t.Fatalf("b64 response data count = %d, want 1", len(b64Resp.Data))
	}

	// 4. Test POST /aigc/v1/image/generations (Singular route alias)
	singularReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/image/generations", bytes.NewReader(genPayload))
	singularReq.Header.Set("Content-Type", "application/json")
	singularReq.Header.Set("Authorization", "Bearer test-key")
	singularReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"1:1","width":1024,"height":1024}`)
	singularRR := httptest.NewRecorder()
	server.engine.ServeHTTP(singularRR, singularReq)

	if singularRR.Code != http.StatusOK {
		t.Fatalf("POST /aigc/v1/image/generations code = %d, want %d, body = %s", singularRR.Code, http.StatusOK, singularRR.Body.String())
	}

	// 5. Test POST /aigc/v1/image/edits (Singular route alias)
	singularEditReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/image/edits", bytes.NewReader(editPayload))
	singularEditReq.Header.Set("Content-Type", "application/json")
	singularEditReq.Header.Set("Authorization", "Bearer test-key")
	singularEditReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"1:1","width":1024,"height":1024}`)
	singularEditRR := httptest.NewRecorder()
	server.engine.ServeHTTP(singularEditRR, singularEditReq)

	if singularEditRR.Code != http.StatusOK {
		t.Fatalf("POST /aigc/v1/image/edits code = %d, want %d, body = %s", singularEditRR.Code, http.StatusOK, singularEditRR.Body.String())
	}
}

func TestAIGCForwardedUserAPIKey(t *testing.T) {
	store := newTestAIGCStore()
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
	)

	server := newTestServerWithOptions(t, WithPluginHost(host))

	createPayload := []byte(`{"model":"seedance","prompt":"a mountain landscape"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/aigc/v1/videos", bytes.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer test-key")
	createReq.Header.Set("X-User-API-Key", "sk-real-user-12345")
	createReq.Header.Set("X-Canonical-Body", `{"resolution":"720p","ratio":"16:9","duration":5}`)
	createRR := httptest.NewRecorder()
	server.engine.ServeHTTP(createRR, createReq)

	if createRR.Code != http.StatusAccepted {
		t.Fatalf("POST /aigc/v1/videos code = %d, want %d, body = %s", createRR.Code, http.StatusAccepted, createRR.Body.String())
	}

	var createResp aigcResponse
	if err := json.Unmarshal(createRR.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	store.mu.Lock()
	savedGen, ok := store.generations[createResp.ID]
	store.mu.Unlock()

	if !ok {
		t.Fatalf("expected generation %s to be saved in store", createResp.ID)
	}
	if savedGen.APIKey != "sk-real-user-12345" {
		t.Errorf("saved generation APIKey = %q, want 'sk-real-user-12345'", savedGen.APIKey)
	}

	// Verify get request with matching user key succeeds
	getReq := httptest.NewRequest(http.MethodGet, "/aigc/v1/videos/"+createResp.ID, nil)
	getReq.Header.Set("Authorization", "Bearer test-key")
	getReq.Header.Set("X-User-API-Key", "sk-real-user-12345")
	getRR := httptest.NewRecorder()
	server.engine.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Errorf("GET /aigc/v1/videos/:id with matching key code = %d, want 200", getRR.Code)
	}

	// Verify get request with mismatched user key is forbidden
	getMismatchedReq := httptest.NewRequest(http.MethodGet, "/aigc/v1/videos/"+createResp.ID, nil)
	getMismatchedReq.Header.Set("Authorization", "Bearer test-key")
	getMismatchedReq.Header.Set("X-User-API-Key", "sk-other-user-99999")
	getMismatchedRR := httptest.NewRecorder()
	server.engine.ServeHTTP(getMismatchedRR, getMismatchedReq)
	if getMismatchedRR.Code != http.StatusForbidden {
		t.Errorf("GET /aigc/v1/videos/:id with mismatched key code = %d, want 403", getMismatchedRR.Code)
	}
}

func TestAIGC_MissingCanonicalBodyReturns400(t *testing.T) {
	store := newTestAIGCStore()
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
	)
	server := newTestServerWithOptions(t, WithPluginHost(host))

	// 1. Missing X-Canonical-Body on async video creation -> 400 Bad Request
	reqVideo := httptest.NewRequest(http.MethodPost, "/aigc/v1/videos", bytes.NewReader([]byte(`{"model":"seedance","prompt":"test"}`)))
	reqVideo.Header.Set("Content-Type", "application/json")
	reqVideo.Header.Set("Authorization", "Bearer test-key")
	rrVideo := httptest.NewRecorder()
	server.engine.ServeHTTP(rrVideo, reqVideo)
	if rrVideo.Code != http.StatusBadRequest {
		t.Errorf("POST /aigc/v1/videos without canonical body code = %d, want 400", rrVideo.Code)
	}

	// 2. Invalid X-Canonical-Body on async video creation -> 400 Bad Request
	reqInvalid := httptest.NewRequest(http.MethodPost, "/aigc/v1/videos", bytes.NewReader([]byte(`{"model":"seedance","prompt":"test"}`)))
	reqInvalid.Header.Set("Content-Type", "application/json")
	reqInvalid.Header.Set("Authorization", "Bearer test-key")
	reqInvalid.Header.Set("X-Canonical-Body", "not-a-json")
	rrInvalid := httptest.NewRecorder()
	server.engine.ServeHTTP(rrInvalid, reqInvalid)
	if rrInvalid.Code != http.StatusBadRequest {
		t.Errorf("POST /aigc/v1/videos with invalid canonical body code = %d, want 400", rrInvalid.Code)
	}

	// 3. Missing X-Canonical-Body on sync image generation -> 400 Bad Request
	reqSync := httptest.NewRequest(http.MethodPost, "/aigc/v1/images/generations", bytes.NewReader([]byte(`{"model":"qwen","prompt":"test"}`)))
	reqSync.Header.Set("Content-Type", "application/json")
	reqSync.Header.Set("Authorization", "Bearer test-key")
	rrSync := httptest.NewRecorder()
	server.engine.ServeHTTP(rrSync, reqSync)
	if rrSync.Code != http.StatusBadRequest {
		t.Errorf("POST /aigc/v1/images/generations without canonical body code = %d, want 400", rrSync.Code)
	}
}

func TestNormalizeImageUsage(t *testing.T) {
	t.Run("GPT-Image-2 91model and zeroapi format", func(t *testing.T) {
		payload := []byte(`{
			"background": "auto",
			"output_format": "png",
			"quality": "auto",
			"size": "3072x1280",
			"model": "gpt-image-2",
			"usage": {
				"input_tokens": 88,
				"input_tokens_details": {
					"image_tokens": 0,
					"text_tokens": 88
				},
				"output_tokens": 158,
				"output_tokens_details": {
					"image_tokens": 158,
					"text_tokens": 0
				},
				"total_tokens": 246
			}
		}`)

		usage := normalizeImageUsage(payload)
		if usage == nil {
			t.Fatalf("expected non-nil normalized usage")
		}
		if usage.InputTokens != 88 || usage.OutputTokens != 158 || usage.TotalTokens != 246 {
			t.Errorf("unexpected tokens: input=%d, output=%d, total=%d", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		}
		if usage.InputTokensDetails == nil || usage.InputTokensDetails.TextTokens != 88 || usage.InputTokensDetails.ImageTokens != 0 {
			t.Errorf("unexpected input details: %+v", usage.InputTokensDetails)
		}
		if usage.OutputTokensDetails == nil || usage.OutputTokensDetails.ImageTokens != 158 || usage.OutputTokensDetails.TextTokens != 0 {
			t.Errorf("unexpected output details: %+v", usage.OutputTokensDetails)
		}
	})

	t.Run("Volcengine Seedream format", func(t *testing.T) {
		payload := []byte(`{
			"created": 1787536336,
			"data": [
				{"url": "https://example.com/seedream.jpeg"}
			],
			"usage": {
				"input_images": 0,
				"generated_images": 1,
				"output_tokens": 15360,
				"total_tokens": 15360
			}
		}`)

		usage := normalizeImageUsage(payload)
		if usage == nil {
			t.Fatalf("expected non-nil normalized usage")
		}
		if usage.InputTokens != 0 || usage.OutputTokens != 15360 || usage.TotalTokens != 15360 {
			t.Errorf("unexpected tokens: input=%d, output=%d, total=%d", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		}
		if usage.OutputTokensDetails == nil || usage.OutputTokensDetails.ImageTokens != 15360 {
			t.Errorf("unexpected output details: %+v", usage.OutputTokensDetails)
		}
	})

	t.Run("Standard OpenAI format for image endpoint", func(t *testing.T) {
		payload := []byte(`{
			"created": 1787536336,
			"data": [{"url": "https://example.com/img.png"}],
			"usage": {
				"prompt_tokens": 50,
				"completion_tokens": 100,
				"total_tokens": 150
			}
		}`)

		usage := normalizeImageUsage(payload)
		if usage == nil {
			t.Fatalf("expected non-nil normalized usage")
		}
		if usage.InputTokens != 50 || usage.OutputTokens != 100 || usage.TotalTokens != 150 {
			t.Errorf("unexpected tokens: input=%d, output=%d, total=%d", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		}
		if usage.InputTokensDetails == nil || usage.InputTokensDetails.TextTokens != 50 {
			t.Errorf("unexpected input details: %+v", usage.InputTokensDetails)
		}
		if usage.OutputTokensDetails == nil || usage.OutputTokensDetails.ImageTokens != 100 {
			t.Errorf("unexpected output details: %+v", usage.OutputTokensDetails)
		}
	})
}

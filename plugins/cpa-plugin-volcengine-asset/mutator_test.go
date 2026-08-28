package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

type mockDBClient struct {
	mu   sync.Mutex
	rows []map[string]any
}

func (m *mockDBClient) Query(_ context.Context, query string, args ...any) ([]map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []map[string]any
	if len(args) > 0 {
		argStr := fmt.Sprintf("%v", args[0])
		for _, row := range m.rows {
			if row["url_sha256"] == argStr || row["asset_id"] == argStr {
				result = append(result, row)
			}
		}
	}
	return result, nil
}

func (m *mockDBClient) Exec(_ context.Context, query string, args ...any) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.Contains(query, "INSERT INTO volcengine_assets") {
		// args: url_sha256, original_url, asset_id, file_type, status, error_message
		hash := args[0].(string)
		orig := args[1].(string)
		assetID := args[2].(string)
		fileType := args[3].(string)
		status := args[4].(string)
		errMsg := args[5].(string)

		found := false
		for _, row := range m.rows {
			if row["url_sha256"] == hash {
				row["asset_id"] = assetID
				row["file_type"] = fileType
				row["status"] = status
				row["error_message"] = errMsg
				found = true
				break
			}
		}
		if !found {
			m.rows = append(m.rows, map[string]any{
				"id":            int64(len(m.rows) + 1),
				"url_sha256":    hash,
				"original_url":  orig,
				"asset_id":      assetID,
				"file_type":     fileType,
				"status":        status,
				"error_message": errMsg,
			})
		}
		return 1, int64(len(m.rows)), nil
	}

	if strings.Contains(query, "UPDATE volcengine_assets") {
		// args: status, error_message, asset_id
		status := args[0].(string)
		errMsg := args[1].(string)
		assetID := args[2].(string)

		for _, row := range m.rows {
			if row["asset_id"] == assetID {
				row["status"] = status
				row["error_message"] = errMsg
			}
		}
		return 1, 0, nil
	}

	return 0, 0, nil
}

type mockArkClient struct {
	mu           sync.Mutex
	createCalls  int
	getCalls     map[string]int
	createStatus string
	queryStatus  map[string][]string // list of statuses to return per call
}

func (m *mockArkClient) CreateAsset(_ context.Context, name, fileType, urlStr string) (AssetItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls++

	id := fmt.Sprintf("asset-mock-%d", m.createCalls)
	status := m.createStatus
	if status == "" {
		status = "Processing"
	}
	return AssetItem{
		ID:       id,
		Name:     name,
		FileType: fileType,
		Status:   status,
	}, nil
}

func (m *mockArkClient) GetAsset(_ context.Context, assetID string) (AssetItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getCalls == nil {
		m.getCalls = make(map[string]int)
	}
	callIdx := m.getCalls[assetID]
	m.getCalls[assetID]++

	status := "Active"
	if statuses, ok := m.queryStatus[assetID]; ok && len(statuses) > 0 {
		if callIdx < len(statuses) {
			status = statuses[callIdx]
		} else {
			status = statuses[len(statuses)-1]
		}
	}

	var errMsg string
	if status == "Failed" {
		errMsg = "unsupported image resolution"
	}

	return AssetItem{
		ID:           assetID,
		Status:       status,
		ErrorMessage: errMsg,
	}, nil
}

func TestMutator_NonSeedanceModelSkipped(t *testing.T) {
	cfg := DefaultConfig()
	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{}
	store := NewAssetStore(mockDB, 1*time.Hour)
	mutator := NewMutator(cfg, mockArk, store)

	unmatchedModels := []string{
		"kling/v1.5",
		"other-vendor/doubao-seedance-1-5-pro",
		"doubao-seedance-1-5-pro", // without volcengine/ prefix
		"sora-1.0",
	}

	for _, m := range unmatchedModels {
		inputJSON := []byte(fmt.Sprintf(`{"model":%q,"first_frame":"https://example.com/image.png"}`, m))
		draft := &aigc.ContentGenerationDraft{
			Model: m,
			Input: inputJSON,
		}

		resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
			Phase: aigc.PhaseBeforeCreate,
			Draft: draft,
		})
		if err != nil {
			t.Fatalf("unexpected error for model %s: %v", m, err)
		}

		if string(resp.Draft.Input) != string(inputJSON) {
			t.Fatalf("expected input for model %s to be untouched, got %s", m, string(resp.Draft.Input))
		}
	}

	if mockArk.createCalls != 0 {
		t.Fatalf("expected 0 create calls for unmatched models, got %d", mockArk.createCalls)
	}
}

func TestMutator_BeforeCreate_FastTransformation(t *testing.T) {
	cfg := DefaultConfig()
	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{}
	store := NewAssetStore(mockDB, 1*time.Hour)
	mutator := NewMutator(cfg, mockArk, store)

	inputJSON := []byte(`{
		"model": "volcengine/doubao-seedance-1-5-pro",
		"prompt": "animate picture",
		"first_frame": "https://example.com/first.png",
		"content": [
			{"type": "image_url", "image_url": {"url": "https://example.com/ref.jpg"}, "role": "reference_image"}
		]
	}`)

	draft := &aigc.ContentGenerationDraft{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Input: inputJSON,
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockArk.createCalls != 2 {
		t.Fatalf("expected 2 create calls, got %d", mockArk.createCalls)
	}

	mutatedInput := resp.Draft.Input
	firstFrame := gjson.GetBytes(mutatedInput, "first_frame").String()
	if !strings.HasPrefix(firstFrame, "asset://asset-mock-") {
		t.Fatalf("expected first_frame to be asset://asset-mock-*, got %s", firstFrame)
	}

	refURL := gjson.GetBytes(mutatedInput, "content.0.image_url.url").String()
	if !strings.HasPrefix(refURL, "asset://asset-mock-") {
		t.Fatalf("expected ref image to be asset://asset-mock-*, got %s", refURL)
	}

	meta, ok := resp.Draft.Metadata["volcengine-asset"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata volcengine-asset to exist")
	}
	assetIDs, ok := meta["asset_ids"].([]string)
	if !ok || len(assetIDs) != 2 {
		t.Fatalf("expected 2 asset_ids in metadata, got %v", meta["asset_ids"])
	}
}

func TestMutator_BeforeCreate_CacheHit(t *testing.T) {
	cfg := DefaultConfig()
	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{}
	store := NewAssetStore(mockDB, 1*time.Hour)
	mutator := NewMutator(cfg, mockArk, store)

	// Pre-populate DB with an Active asset
	urlStr := "https://example.com/existing.png"
	hash := sha256Hex(urlStr)
	_ = store.Save(context.Background(), &AssetRecord{
		URLSHA256:   hash,
		OriginalURL: urlStr,
		AssetID:     "asset-existing-123",
		FileType:    "image",
		Status:      "Active",
	})

	inputJSON := []byte(fmt.Sprintf(`{"model":"volcengine/doubao-seedance-1-5-pro","first_frame":"%s"}`, urlStr))
	draft := &aigc.ContentGenerationDraft{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Input: inputJSON,
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockArk.createCalls != 0 {
		t.Fatalf("expected 0 create calls on cache hit, got %d", mockArk.createCalls)
	}

	firstFrame := gjson.GetBytes(resp.Draft.Input, "first_frame").String()
	if firstFrame != "asset://asset-existing-123" {
		t.Fatalf("expected asset://asset-existing-123, got %s", firstFrame)
	}
}

func TestMutator_BeforeSubmit_AsyncWaitActive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollIntervalMs = 20
	cfg.PollTimeoutSeconds = 5

	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{
		queryStatus: map[string][]string{
			"asset-123": {"Processing", "Processing", "Active"},
		},
	}
	store := NewAssetStore(mockDB, 1*time.Hour)
	// Initial state from BeforeCreate
	_ = store.Save(context.Background(), &AssetRecord{
		URLSHA256:   "hash123",
		OriginalURL: "https://example.com/first.png",
		AssetID:     "asset-123",
		FileType:    "image",
		Status:      "Processing",
	})

	mutator := NewMutator(cfg, mockArk, store)

	gen := aigc.ContentGeneration{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Metadata: map[string]any{
			"volcengine-asset": map[string]any{
				"asset_ids": []string{"asset-123"},
			},
		},
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeSubmit,
		Generation: gen,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reject {
		t.Fatalf("expected mutation to succeed, but rejected: %s", resp.ErrorMessage)
	}

	if mockArk.getCalls["asset-123"] < 3 {
		t.Fatalf("expected at least 3 get calls, got %d", mockArk.getCalls["asset-123"])
	}

	// Verify DB status updated to Active
	rec, found, _ := store.GetByID(context.Background(), "asset-123")
	if !found || rec.Status != "Active" {
		t.Fatalf("expected asset-123 to be Active in store, got %v", rec)
	}
}

func TestMutator_BeforeSubmit_FailFast(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollIntervalMs = 20
	cfg.PollTimeoutSeconds = 5

	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{
		queryStatus: map[string][]string{
			"asset-bad": {"Processing", "Failed"},
		},
	}
	store := NewAssetStore(mockDB, 1*time.Hour)
	mutator := NewMutator(cfg, mockArk, store)

	gen := aigc.ContentGeneration{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Metadata: map[string]any{
			"volcengine-asset": map[string]any{
				"asset_ids": []string{"asset-bad"},
			},
		},
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeSubmit,
		Generation: gen,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Reject {
		t.Fatalf("expected mutation to reject on asset failure")
	}
	if resp.ErrorCode != aigc.ErrCodeAssetUploadFailed {
		t.Fatalf("expected %s error code, got %s", aigc.ErrCodeAssetUploadFailed, resp.ErrorCode)
	}
}

func TestMutator_BeforeSubmit_Timeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PollIntervalMs = 10
	cfg.PollTimeoutSeconds = 1 // 1 second timeout

	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{
		queryStatus: map[string][]string{
			"asset-slow": {"Processing"},
		},
	}
	store := NewAssetStore(mockDB, 1*time.Hour)
	mutator := NewMutator(cfg, mockArk, store)

	gen := aigc.ContentGeneration{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Metadata: map[string]any{
			"volcengine-asset": map[string]any{
				"asset_ids": []string{"asset-slow"},
			},
		},
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeSubmit,
		Generation: gen,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Reject {
		t.Fatalf("expected mutation to reject on timeout")
	}
	if resp.ErrorCode != aigc.ErrCodeAssetProcessTimeout {
		t.Fatalf("expected %s error code, got %s", aigc.ErrCodeAssetProcessTimeout, resp.ErrorCode)
	}
}

func TestMutator_CustomModelsConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Models = []string{"custom-model-*"}

	mockDB := &mockDBClient{}
	mockArk := &mockArkClient{}
	store := NewAssetStore(mockDB, 1*time.Hour)
	mutator := NewMutator(cfg, mockArk, store)

	// doubao-seedance should NOT match since Models was explicitly overridden
	draft1 := &aigc.ContentGenerationDraft{
		Model: "volcengine/doubao-seedance-1-5-pro",
		Input: []byte(`{"model":"volcengine/doubao-seedance-1-5-pro","first_frame":"https://example.com/1.png"}`),
	}
	resp1, _ := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft1,
	})
	if mockArk.createCalls != 0 {
		t.Fatalf("expected 0 calls for non-matching model, got %d", mockArk.createCalls)
	}
	if string(resp1.Draft.Input) != string(draft1.Input) {
		t.Fatalf("expected input to be untouched")
	}

	// custom-model-abc SHOULD match
	draft2 := &aigc.ContentGenerationDraft{
		Model: "custom-model-abc",
		Input: []byte(`{"model":"custom-model-abc","first_frame":"https://example.com/1.png"}`),
	}
	resp2, _ := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft2,
	})
	if mockArk.createCalls != 1 {
		t.Fatalf("expected 1 call for matching custom model, got %d", mockArk.createCalls)
	}
	if strings.Contains(string(resp2.Draft.Input), "https://example.com/1.png") {
		t.Fatalf("expected URL to be replaced in custom model input")
	}
}

func TestConfig_NormalizeAndDefaults(t *testing.T) {
	// Test standard fields
	cfg1 := Config{
		AccessKey: "ak-123",
		SecretKey: "sk-456",
		GroupID:   "group-789",
		Project:   "my-project",
		Region:    "cn-shanghai",
	}
	cfg1.Normalize()
	if cfg1.AccessKey != "ak-123" || cfg1.SecretKey != "sk-456" || cfg1.GroupID != "group-789" {
		t.Fatalf("unexpected normalized cfg1: %+v", cfg1)
	}
	if cfg1.Project != "my-project" || cfg1.Region != "cn-shanghai" {
		t.Fatalf("unexpected project/region in cfg1: %+v", cfg1)
	}
	if len(cfg1.Models) == 0 || cfg1.Models[0] != "volcengine/doubao-seedance-*" {
		t.Fatalf("expected default models in cfg1: %+v", cfg1.Models)
	}

	// Test client init validation
	_, err := NewVolcSDKArkAssetClient(Config{})
	if err == nil {
		t.Fatalf("expected error when initializing client without credentials")
	}

	client, errValid := NewVolcSDKArkAssetClient(cfg1)
	if errValid != nil {
		t.Fatalf("unexpected error initializing valid client: %v", errValid)
	}
	if client == nil {
		t.Fatalf("expected client to not be nil")
	}
}

func TestEnsureSchema(t *testing.T) {
	db := &mockDBClient{}
	if err := EnsureSchema(context.Background(), db); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}
}

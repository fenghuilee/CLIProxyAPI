package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

type mockUploader struct {
	mu        sync.Mutex
	uploads   map[string][]byte
	failOnKey string
}

func newMockUploader() *mockUploader {
	return &mockUploader{uploads: make(map[string][]byte)}
}

func (m *mockUploader) Upload(ctx context.Context, key string, mimeType string, data []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOnKey != "" && strings.Contains(key, m.failOnKey) {
		return "", errors.New("tos network error")
	}
	m.uploads[key] = data
	return fmt.Sprintf("https://cdn.example.com/%s", key), nil
}

func TestMutatorSingleImageReplacement(t *testing.T) {
	rawImage := []byte("fake-png-data-bytes")
	b64Image := base64.StdEncoding.EncodeToString(rawImage)
	dataURI := fmt.Sprintf("data:image/png;base64,%s", b64Image)

	uploader := newMockUploader()
	cfg := DefaultConfig()
	cfg.PublicBaseURL = "https://cdn.example.com"
	mutator := NewMutator(cfg, uploader)

	inputJSON := fmt.Sprintf(`{"model":"doubao-seedance","image_url":"%s","prompt":"animate this image"}`, dataURI)
	draft := &aigc.ContentGenerationDraft{
		ID:    "cg_test",
		Input: json.RawMessage(inputJSON),
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft,
	})
	if err != nil {
		t.Fatalf("mutate failed: %v", err)
	}
	if resp.Reject {
		t.Fatalf("unexpected reject: %s (%s)", resp.ErrorMessage, resp.ErrorCode)
	}

	var outputMap map[string]any
	if err := json.Unmarshal(resp.Draft.Input, &outputMap); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	imageURL, ok := outputMap["image_url"].(string)
	if !ok || !strings.HasPrefix(imageURL, "https://cdn.example.com/aigc/inputs/") || !strings.HasSuffix(imageURL, ".png") {
		t.Errorf("image_url was not properly transformed: %s", imageURL)
	}

	if len(uploader.uploads) != 1 {
		t.Errorf("uploader received %d uploads, want 1", len(uploader.uploads))
	}
}

func TestMutatorDedupAndMultipleImages(t *testing.T) {
	raw1 := []byte("image-data-1")
	raw2 := []byte("image-data-2")
	b64_1 := base64.StdEncoding.EncodeToString(raw1)
	b64_2 := base64.StdEncoding.EncodeToString(raw2)

	uri1 := "data:image/jpeg;base64," + b64_1
	uri2 := "data:image/webp;base64," + b64_2
	uri1_dup := uri1 // identical data

	uploader := newMockUploader()
	mutator := NewMutator(DefaultConfig(), uploader)

	inputJSON := fmt.Sprintf(`{
		"first_frame": "%s",
		"last_frame": "%s",
		"reference_images": ["%s", "https://already-a-url.com/image.jpg"]
	}`, uri1, uri2, uri1_dup)

	draft := &aigc.ContentGenerationDraft{
		ID:    "cg_test_multi",
		Input: json.RawMessage(inputJSON),
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft,
	})
	if err != nil {
		t.Fatalf("mutate failed: %v", err)
	}
	if resp.Reject {
		t.Fatalf("unexpected reject: %s", resp.ErrorMessage)
	}

	// Should have uploaded exactly 2 unique objects (uri1 and uri2)
	if len(uploader.uploads) != 2 {
		t.Errorf("expected 2 unique uploads, got %d", len(uploader.uploads))
	}

	var outputMap map[string]any
	_ = json.Unmarshal(resp.Draft.Input, &outputMap)

	firstFrame := outputMap["first_frame"].(string)
	refs := outputMap["reference_images"].([]any)
	ref0 := refs[0].(string)
	ref1 := refs[1].(string)

	if firstFrame != ref0 {
		t.Errorf("deduped URL for uri1 and uri1_dup should match: %s != %s", firstFrame, ref0)
	}
	if ref1 != "https://already-a-url.com/image.jpg" {
		t.Errorf("regular URL was modified: %s", ref1)
	}
}

func TestMutatorSizeLimits(t *testing.T) {
	largeData := make([]byte, 1000)
	b64Large := base64.StdEncoding.EncodeToString(largeData)
	dataURI := "data:image/png;base64," + b64Large

	uploader := newMockUploader()
	cfg := DefaultConfig()
	cfg.MaxImageBytes = 500 // Limit to 500 bytes
	mutator := NewMutator(cfg, uploader)

	inputJSON := fmt.Sprintf(`{"image":"%s"}`, dataURI)
	draft := &aigc.ContentGenerationDraft{
		Input: json.RawMessage(inputJSON),
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !resp.Reject || resp.ErrorCode != aigc.ErrCodeAssetUploadFailed {
		t.Errorf("expected rejection for oversized image, got: %+v", resp)
	}
}

func TestMutatorUploadFailureRejection(t *testing.T) {
	rawImage := []byte("error-image-bytes")
	b64Image := base64.StdEncoding.EncodeToString(rawImage)
	dataURI := "data:image/png;base64," + b64Image

	uploader := newMockUploader()
	uploader.failOnKey = "aigc/inputs"
	mutator := NewMutator(DefaultConfig(), uploader)

	inputJSON := fmt.Sprintf(`{"image":"%s"}`, dataURI)
	draft := &aigc.ContentGenerationDraft{
		Input: json.RawMessage(inputJSON),
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeCreate,
		Draft: draft,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !resp.Reject {
		t.Errorf("expected rejection on upload failure")
	}
}

func TestMutatorEgressArtifactsAndOutput(t *testing.T) {
	rawImage := []byte("generated-cat-image-data")
	b64Image := base64.StdEncoding.EncodeToString(rawImage)

	uploader := newMockUploader()
	cfg := DefaultConfig()
	cfg.PublicBaseURL = "https://cdn.example.com"
	mutator := NewMutator(cfg, uploader)

	gen := aigc.ContentGeneration{
		ID:    "cg_test_egress",
		Model: "gpt-image-2",
		Artifacts: []aigc.ContentGenerationArtifact{
			{
				ArtifactType:    "output_image",
				StorageProvider: "inline",
				URI:             "inline://sha256=12345",
				Metadata: map[string]any{
					"b64_json": b64Image,
				},
			},
			{
				ArtifactType:    "output_image",
				StorageProvider: "openai-compat",
				URI:             "https://already-a-url.com/direct.png",
			},
		},
		Output: json.RawMessage(fmt.Sprintf(`{"data":[{"b64_json":"%s"},{"url":"https://already-a-url.com/direct.png"}]}`, "data:image/png;base64,"+b64Image)),
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeComplete,
		Generation: gen,
	})
	if err != nil {
		t.Fatalf("mutate egress error: %v", err)
	}

	if len(resp.Artifacts) != 2 {
		t.Fatalf("Artifacts count = %d, want 2", len(resp.Artifacts))
	}

	art0 := resp.Artifacts[0]
	if art0.StorageProvider != "tos" {
		t.Errorf("art0 StorageProvider = %s, want tos", art0.StorageProvider)
	}
	if !strings.HasPrefix(art0.URI, "https://cdn.example.com/aigc/outputs/") {
		t.Errorf("art0 URI = %s, want tos url", art0.URI)
	}
	if art0.SizeBytes != int64(len(rawImage)) {
		t.Errorf("art0 SizeBytes = %d, want %d", art0.SizeBytes, len(rawImage))
	}

	art1 := resp.Artifacts[1]
	if art1.StorageProvider != "openai-compat" || art1.URI != "https://already-a-url.com/direct.png" {
		t.Errorf("art1 untouched url failed: %+v", art1)
	}

	if art0.Metadata != nil && art0.Metadata["b64_json"] != nil {
		t.Errorf("art0.Metadata should not contain b64_json after upload, got: %v", art0.Metadata["b64_json"])
	}

	if resp.Generation == nil || resp.Generation.Metadata == nil {
		t.Fatalf("expected generation metadata to be populated")
	}
	tosMeta, ok := resp.Generation.Metadata["data-uri-to-tos"].(map[string]any)
	if !ok || len(tosMeta["objects"].([]string)) == 0 {
		t.Errorf("expected data-uri-to-tos objects in generation metadata, got: %v", resp.Generation.Metadata)
	}
}

func TestMutatorEgressSerializedArtifactList(t *testing.T) {
	rawImage := []byte("large-image-content-bytes")
	b64Image := base64.StdEncoding.EncodeToString(rawImage)

	uploader := newMockUploader()
	cfg := DefaultConfig()
	cfg.PublicBaseURL = "https://cdn.example.com"
	mutator := NewMutator(cfg, uploader)

	artifacts := []aigc.ContentGenerationArtifact{
		{
			ArtifactType:    "output_image",
			StorageProvider: "inline",
			URI:             "inline://sha256=abcdef",
			Metadata: map[string]any{
				"b64_json": b64Image,
				"sha256":   "abcdef",
			},
		},
	}
	outJSON, _ := json.Marshal(artifacts)

	gen := aigc.ContentGeneration{
		ID:        "cg_test_serialized_output",
		Model:     "gpt-image-2",
		Artifacts: artifacts,
		Output:    outJSON,
	}

	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{
		Phase:      aigc.PhaseBeforeComplete,
		Generation: gen,
	})
	if err != nil {
		t.Fatalf("mutate egress error: %v", err)
	}

	if len(resp.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(resp.Artifacts))
	}
	art := resp.Artifacts[0]
	if art.StorageProvider != "tos" {
		t.Errorf("expected storage_provider=tos, got %s", art.StorageProvider)
	}
	if art.Metadata != nil && art.Metadata["b64_json"] != nil {
		t.Errorf("expected b64_json to be stripped from artifact metadata")
	}

	// Verify Output was updated to compact newArtifacts JSON without b64_json
	outputStr := string(resp.Generation.Output)
	if strings.Contains(outputStr, b64Image) {
		t.Errorf("expected generation Output to not contain raw base64 data, got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "https://cdn.example.com/aigc/outputs/") {
		t.Errorf("expected generation Output to contain TOS URL, got: %s", outputStr)
	}
	if !strings.Contains(outputStr, `"storage_provider":"tos"`) {
		t.Errorf("expected generation Output to contain storage_provider tos, got: %s", outputStr)
	}
}

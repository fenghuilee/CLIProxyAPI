package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

type mockTransferer struct {
	shouldTransferFn func(url string) bool
	transferFn       func(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error)
}

func (m *mockTransferer) ShouldTransfer(rawURL string) bool {
	if m.shouldTransferFn != nil {
		return m.shouldTransferFn(rawURL)
	}
	return strings.HasPrefix(rawURL, "http")
}

func (m *mockTransferer) Transfer(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error) {
	if m.transferFn != nil {
		return m.transferFn(ctx, rawURL, defaultMIME)
	}
	return &TransferResult{
		NewURL:    "https://top1ai.tos-cn-guangzhou.volces.com/aigc/outputs/archived.mp4",
		ObjectKey: "aigc/outputs/archived.mp4",
		MIMEType:  "video/mp4",
		SizeBytes: 1024 * 1024,
		SHA256:    "mockhash123",
	}, nil
}

func TestMutator_SuccessfulTransfer(t *testing.T) {
	mockT := &mockTransferer{}
	m := NewMutator(DefaultConfig(), mockT)

	originalURL := "https://dashscope-a717.oss-accelerate.aliyuncs.com/temp/video.mp4?Expires=1795"
	req := aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeComplete,
		Generation: aigc.ContentGeneration{
			ID: "cg_test_success",
			Artifacts: []aigc.ContentGenerationArtifact{
				{
					GenerationID:    "cg_test_success",
					ArtifactType:    "output_video",
					StorageProvider: "dashscope",
					URI:             originalURL,
					MIMEType:        "video/mp4",
				},
			},
			Output: []byte(`{"video_url":"` + originalURL + `"}`),
		},
	}

	resp, err := m.MutateContentGeneration(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(resp.Artifacts))
	}

	art := resp.Artifacts[0]
	if art.StorageProvider != "tos" {
		t.Errorf("expected storage_provider 'tos', got '%s'", art.StorageProvider)
	}
	if !strings.HasPrefix(art.URI, "https://top1ai.tos-cn-guangzhou.volces.com") {
		t.Errorf("expected TOS URL, got '%s'", art.URI)
	}

	// Verify Output JSON was synchronized with new permanent TOS URL
	if strings.Contains(string(resp.Generation.Output), originalURL) {
		t.Errorf("expected original URL replaced in Output JSON, but found it in %s", string(resp.Generation.Output))
	}
	if !strings.Contains(string(resp.Generation.Output), art.URI) {
		t.Errorf("expected new TOS URL in Output JSON, got %s", string(resp.Generation.Output))
	}
}

func TestMutator_GracefulFallbackOnFailure(t *testing.T) {
	mockT := &mockTransferer{
		transferFn: func(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error) {
			return nil, errors.New("simulated network connection timeout to upstream CDN")
		},
	}
	m := NewMutator(DefaultConfig(), mockT)

	originalURL := "https://dashscope-a717.oss-accelerate.aliyuncs.com/temp/video.mp4?Expires=1795"
	req := aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeComplete,
		Generation: aigc.ContentGeneration{
			ID: "cg_test_fallback",
			Artifacts: []aigc.ContentGenerationArtifact{
				{
					GenerationID:    "cg_test_fallback",
					ArtifactType:    "output_video",
					StorageProvider: "dashscope",
					URI:             originalURL,
					MIMEType:        "video/mp4",
				},
			},
			Output: []byte(`{"video_url":"` + originalURL + `"}`),
		},
	}

	// Crucial financial assertion: Must NOT fail the mutation or generation
	resp, err := m.MutateContentGeneration(context.Background(), req)
	if err != nil {
		t.Fatalf("mutator must not return error on transfer failure: %v", err)
	}
	if len(resp.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact preserved, got %d", len(resp.Artifacts))
	}

	art := resp.Artifacts[0]
	// Must preserve original provider and URI as fallback
	if art.StorageProvider != "dashscope" {
		t.Errorf("expected original provider 'dashscope' preserved, got '%s'", art.StorageProvider)
	}
	if art.URI != originalURL {
		t.Errorf("expected original URI preserved, got '%s'", art.URI)
	}
	if !strings.Contains(string(resp.Generation.Output), originalURL) {
		t.Errorf("expected original Output preserved, got %s", string(resp.Generation.Output))
	}
}

func TestMutator_SkipAlreadySelfHosted(t *testing.T) {
	transferred := false
	mockT := &mockTransferer{
		shouldTransferFn: func(rawURL string) bool {
			return false // simulates already self-hosted URL
		},
		transferFn: func(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error) {
			transferred = true
			return nil, nil
		},
	}
	m := NewMutator(DefaultConfig(), mockT)

	selfHostedURL := "https://top1ai.tos-cn-guangzhou.volces.com/aigc/outputs/existing.mp4"
	req := aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeComplete,
		Generation: aigc.ContentGeneration{
			ID: "cg_test_skip",
			Artifacts: []aigc.ContentGenerationArtifact{
				{
					GenerationID:    "cg_test_skip",
					ArtifactType:    "output_video",
					StorageProvider: "tos",
					URI:             selfHostedURL,
					MIMEType:        "video/mp4",
				},
			},
		},
	}

	resp, err := m.MutateContentGeneration(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transferred {
		t.Errorf("self-hosted asset should not trigger re-transfer")
	}
	if resp.Artifacts[0].URI != selfHostedURL {
		t.Errorf("URI changed unexpectedly: %s", resp.Artifacts[0].URI)
	}
}

func TestMutator_SystemOnlyFilter(t *testing.T) {
	transferred := false
	mockT := &mockTransferer{
		transferFn: func(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error) {
			transferred = true
			return &TransferResult{
				NewURL:    "https://top1ai.tos-cn-guangzhou.volces.com/aigc/outputs/archived.mp4",
				ObjectKey: "aigc/outputs/archived.mp4",
				MIMEType:  "video/mp4",
			}, nil
		},
	}

	cfg := DefaultConfig()
	cfg.SystemOnly = true
	m := NewMutator(cfg, mockT)

	externalURL := "https://dashscope-a717.oss-accelerate.aliyuncs.com/temp/video.mp4?Expires=1795"

	// Case 1: Non-system user (UserID = 42) must be skipped immediately
	reqExternalUser := aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeComplete,
		Generation: aigc.ContentGeneration{
			ID:     "cg_user_42",
			UserID: 42,
			Artifacts: []aigc.ContentGenerationArtifact{
				{
					GenerationID:    "cg_user_42",
					ArtifactType:    "output_video",
					StorageProvider: "dashscope",
					URI:             externalURL,
					MIMEType:        "video/mp4",
				},
			},
		},
	}
	respUser, errUser := m.MutateContentGeneration(context.Background(), reqExternalUser)
	if errUser != nil {
		t.Fatalf("unexpected error: %v", errUser)
	}
	if transferred {
		t.Errorf("external user (UserID > 0) should not trigger transfer when SystemOnly is enabled")
	}
	if respUser.Artifacts[0].StorageProvider != "dashscope" || respUser.Artifacts[0].URI != externalURL {
		t.Errorf("artifact should be preserved unmodified for external user")
	}

	// Case 2: System user (UserID = 0) must be transferred to permanent TOS
	transferred = false
	reqSystemUser := aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeComplete,
		Generation: aigc.ContentGeneration{
			ID:     "cg_system_0",
			UserID: 0,
			Artifacts: []aigc.ContentGenerationArtifact{
				{
					GenerationID:    "cg_system_0",
					ArtifactType:    "output_video",
					StorageProvider: "dashscope",
					URI:             externalURL,
					MIMEType:        "video/mp4",
				},
			},
		},
	}
	respSys, errSys := m.MutateContentGeneration(context.Background(), reqSystemUser)
	if errSys != nil {
		t.Fatalf("unexpected error: %v", errSys)
	}
	if !transferred {
		t.Errorf("system user (UserID == 0) must trigger transfer")
	}
	if respSys.Artifacts[0].StorageProvider != "tos" {
		t.Errorf("expected storage_provider 'tos', got %s", respSys.Artifacts[0].StorageProvider)
	}
}

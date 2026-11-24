package bananarouter

import (
	"context"
	"net/http"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestBananaRouterPrepareSubmit_ImageEdit(t *testing.T) {
	adapter := NewBananaRouterAdapter()
	inputJSON := `{
		"images": [
			"https://example.com/img1.jpeg",
			"https://example.com/img2.png"
		],
		"model": "bter3/gpt-image-2.6-sunburst",
		"prompt": "inspire nail art from image 1, pose from image 2",
		"size": "1088x1920",
		"quality": "medium",
		"output_format": "png",
		"response_format": "url",
		"n": 1
	}`

	req, err := adapter.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "bter3/gpt-image-2.6-sunburst",
			Input:    []byte(inputJSON),
			Metadata: map[string]any{"image_operation": "edits"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareSubmit failed: %v", err)
	}

	if req.URL != "/images/edits" {
		t.Errorf("URL = %q, want /images/edits", req.URL)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", req.Header.Get("Content-Type"))
	}

	body := ojson.ParseBytes(req.Body)
	if body.Get("model").String() != "gpt-image-2.6-sunburst" {
		t.Errorf("model = %q, want gpt-image-2.6-sunburst", body.Get("model").String())
	}
	if body.Get("prompt").String() != "inspire nail art from image 1, pose from image 2" {
		t.Errorf("prompt mismatch: %s", body.Get("prompt").String())
	}
	images := body.Get("images").Array()
	if len(images) != 2 {
		t.Fatalf("expected 2 images in JSON, got %d", len(images))
	}
	if images[0].String() != "https://example.com/img1.jpeg" || images[1].String() != "https://example.com/img2.png" {
		t.Errorf("images mismatch: %v", images)
	}
	if body.Get("size").String() != "1088x1920" {
		t.Errorf("size = %q, want 1088x1920", body.Get("size").String())
	}
}

func TestBananaRouterPrepareSubmit_TextToImage(t *testing.T) {
	adapter := NewBananaRouterAdapter()
	inputJSON := `{
		"model": "bter/gpt-image-2.6",
		"prompt": "a cute yellow banana character",
		"size": "1024x1024"
	}`

	req, err := adapter.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:  aigc.ContentKindImage,
			Model: "bter/gpt-image-2.6",
			Input: []byte(inputJSON),
		},
	})
	if err != nil {
		t.Fatalf("PrepareSubmit failed: %v", err)
	}

	if req.URL != "/images/generations" {
		t.Errorf("URL = %q, want /images/generations", req.URL)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", req.Header.Get("Content-Type"))
	}

	body := ojson.ParseBytes(req.Body)
	if body.Get("model").String() != "gpt-image-2.6" {
		t.Errorf("model = %q, want gpt-image-2.6", body.Get("model").String())
	}
}

func TestBananaRouterParseSubmit_SyncCompleted(t *testing.T) {
	adapter := NewBananaRouterAdapter()
	respBody := []byte(`{
		"created": 1795446436,
		"data": [
			{"url": "https://cdn.example.com/generated_nail_art.png"}
		],
		"usage": {
			"input_tokens": 1397,
			"output_tokens": 2855,
			"total_tokens": 4252
		}
	}`)

	result, err := adapter.ParseSubmit(context.Background(), aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       respBody,
	})
	if err != nil {
		t.Fatalf("ParseSubmit failed: %v", err)
	}

	if result.Status != aigc.StatusSucceeded {
		t.Errorf("status = %v, want %v", result.Status, aigc.StatusSucceeded)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts length = %d, want 1", len(result.Artifacts))
	}
	if result.Artifacts[0].URI != "https://cdn.example.com/generated_nail_art.png" {
		t.Errorf("artifact URI mismatch: %s", result.Artifacts[0].URI)
	}
	if result.Usage == nil {
		t.Fatal("expected result.Usage to be populated, got nil")
	}
	if result.Usage.InputTokens != 1397 || result.Usage.OutputTokens != 2855 || result.Usage.TotalTokens != 4252 {
		t.Errorf("unexpected usage: %+v", result.Usage)
	}
}

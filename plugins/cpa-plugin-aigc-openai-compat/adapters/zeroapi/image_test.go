package zeroapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func prepareImage(t *testing.T, model, input, operation string) (aigc.GenerationExecutionRequest, error) {
	t.Helper()
	gen := aigc.ContentGeneration{Kind: aigc.ContentKindImage, Model: model, Input: []byte(input)}
	if operation != "" {
		gen.Metadata = map[string]any{"image_operation": operation}
	}
	return imageAdapter{}.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: gen})
}

func TestImageAdapterPrefixes(t *testing.T) {
	adapter := imageAdapter{}
	if adapter.Kind() != aigc.ContentKindImage {
		t.Fatalf("Kind = %v, want image", adapter.Kind())
	}
	got := strings.Join(adapter.Prefixes(), ",")
	if got != "ze/gpt-image,zeroapi/gpt-image" {
		t.Fatalf("Prefixes = %q", got)
	}
}

func TestImageAdapterEditUsesJSONWithoutMultipart(t *testing.T) {
	req, err := prepareImage(t, "ze/gpt-image-2.6-sunburst", `{
		"prompt": "make it blue",
		"images": ["https://example.com/a.png"],
		"size": "1024x1536",
		"quality": "medium",
		"output_format": "png",
		"response_format": "url",
		"n": 1
	}`, "edits")
	if err != nil {
		t.Fatalf("PrepareSubmit: %v", err)
	}
	if req.URL != "/images/edits" || req.Method != http.MethodPost {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	body := ojson.ParseBytes(req.Body)
	if got := body.Get("model").String(); got != "gpt-image-2.6-sunburst" {
		t.Fatalf("model = %q", got)
	}
	if got := body.Get("images.0").String(); got != "https://example.com/a.png" {
		t.Fatalf("images.0 = %q", got)
	}
	if got := body.Get("image").String(); got != "https://example.com/a.png" {
		t.Fatalf("image = %q", got)
	}
	if got := body.Get("size").String(); got != "1024x1536" {
		t.Fatalf("size = %q", got)
	}
	if got := body.Get("n").Int(); got != 1 {
		t.Fatalf("n = %d", got)
	}
}

func TestImageAdapterEditForwardsMaskAndMultipleImages(t *testing.T) {
	req, err := prepareImage(t, "zeroapi/gpt-image-2", `{
		"prompt": "edit",
		"images": [{"image_url": "https://example.com/a.png"}, "https://example.com/b.png"],
		"mask": {"image_url": "https://example.com/m.png"}
	}`, "")
	if err != nil {
		t.Fatalf("PrepareSubmit: %v", err)
	}
	body := ojson.ParseBytes(req.Body)
	if req.URL != "/images/edits" || len(body.Get("images").Array()) != 2 {
		t.Fatalf("request = %s %s", req.URL, req.Body)
	}
	if body.Get("image").Exists() {
		t.Fatalf("single image field must be omitted for multiple images: %s", req.Body)
	}
	if got := body.Get("mask").String(); got != "https://example.com/m.png" {
		t.Fatalf("mask = %q", got)
	}
}

func TestImageAdapterTextToImage(t *testing.T) {
	req, err := prepareImage(t, "ze/gpt-image-2.6", `{"prompt":"a cat","size":"1024x1024"}`, "generations")
	if err != nil {
		t.Fatalf("PrepareSubmit: %v", err)
	}
	if req.URL != "/images/generations" {
		t.Fatalf("URL = %q", req.URL)
	}
	body := ojson.ParseBytes(req.Body)
	if body.Get("images").Exists() || body.Get("image").Exists() {
		t.Fatalf("generation must not carry reference images: %s", req.Body)
	}
}

func TestImageAdapterRejectsInvalidRequests(t *testing.T) {
	for name, tc := range map[string]struct{ input, operation string }{
		"edit without images":    {`{"prompt":"x"}`, "edits"},
		"edit without prompt":    {`{"images":["https://example.com/a.png"]}`, "edits"},
		"generation with images": {`{"prompt":"x","images":["https://example.com/a.png"]}`, "generations"},
		"images without prompt":  {`{"images":["https://example.com/a.png"]}`, ""},
		"empty prompt":           {`{}`, ""},
		"unknown operation":      {`{"prompt":"x"}`, "variations"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := prepareImage(t, "ze/gpt-image-2.6", tc.input, tc.operation); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestImageAdapterParseSubmit(t *testing.T) {
	adapter := imageAdapter{}
	result, err := adapter.ParseSubmit(context.Background(), aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"data":[{"url":"https://cdn.example.com/out.png"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`),
	})
	if err != nil {
		t.Fatalf("ParseSubmit: %v", err)
	}
	if result.Status != aigc.StatusSucceeded || len(result.Artifacts) != 1 || result.Artifacts[0].URI != "https://cdn.example.com/out.png" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 7 {
		t.Fatalf("usage = %+v", result.Usage)
	}

	failed, err := adapter.ParseSubmit(context.Background(), aigc.GenerationExecutionResponse{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"message":"bad"}}`),
	})
	if err != nil || failed.Status != aigc.StatusFailed {
		t.Fatalf("failed result = %+v, err = %v", failed, err)
	}

	if _, err := adapter.ParseSubmit(context.Background(), aigc.GenerationExecutionResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[]}`)}); err == nil {
		t.Fatal("expected error for response without output")
	}
}

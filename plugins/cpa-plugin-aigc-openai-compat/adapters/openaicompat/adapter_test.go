package openaicompat_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestDriver_OpenAICompatFlow(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. OpenAI Image (DALL-E 3)
	imgInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "dall-e-3",
			Provider: "openai-compat",
			Input:    []byte(`{"prompt":"a cyberpunk skyline","size":"1024x1024","quality":"hd"}`),
		},
	}
	imgReq, errImg := driver.PrepareSubmit(ctx, imgInput)
	if errImg != nil {
		t.Fatalf("PrepareSubmit image error: %v", errImg)
	}
	if imgReq.URL != "/images/generations" {
		t.Fatalf("URL = %q, want /images/generations", imgReq.URL)
	}

	// Sync OpenAI image response
	imgResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body: []byte(`{
			"created": 1724490000,
			"data": [
				{"url": "https://oaidalleapiprodscus.blob.core.windows.net/cyber.png"}
			]
		}`),
	}
	imgRes, errImgParse := driver.ParseSubmit(ctx, imgResp)
	if errImgParse != nil {
		t.Fatalf("ParseSubmit image error: %v", errImgParse)
	}
	if imgRes.Status != aigc.StatusSucceeded || len(imgRes.Artifacts) != 1 {
		t.Fatalf("imgRes = %+v", imgRes)
	}

	// 1.1 OpenAI Image-to-Image (Image reference generation) - routes to /images/edits
	img2imgInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "91/gpt-image-2",
			Provider: "openai-compat",
			Input:    []byte(`{"prompt":"enhance character teeth","images":["https://example.com/portrait.png"],"size":"1024x1024"}`),
		},
	}
	img2imgReq, errImg2img := driver.PrepareSubmit(ctx, img2imgInput)
	if errImg2img != nil {
		t.Fatalf("PrepareSubmit img2img error: %v", errImg2img)
	}
	if img2imgReq.URL != "/images/edits" {
		t.Fatalf("URL = %q, want /images/edits", img2imgReq.URL)
	}
	if img2imgReq.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", img2imgReq.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(img2imgReq.Body), "https://example.com/portrait.png") {
		t.Fatalf("body does not contain image url: %s", string(img2imgReq.Body))
	}
	if ojson.GetBytes(img2imgReq.Body, "images.0.image_url").String() != "https://example.com/portrait.png" {
		t.Fatalf("images.0.image_url = %q, want https://example.com/portrait.png", ojson.GetBytes(img2imgReq.Body, "images.0.image_url").String())
	}

	// 1.2 OpenAI Inpainting with Mask - routes to /images/edits
	inpaintInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "91/gpt-image-2",
			Provider: "openai-compat",
			Input:    []byte(`{"prompt":"add a red hat","image":"https://example.com/portrait.png","mask":"https://example.com/mask.png","size":"1024x1024"}`),
		},
	}
	inpaintReq, errInpaint := driver.PrepareSubmit(ctx, inpaintInput)
	if errInpaint != nil {
		t.Fatalf("PrepareSubmit inpaint error: %v", errInpaint)
	}
	if inpaintReq.URL != "/images/edits" {
		t.Fatalf("URL = %q, want /images/edits", inpaintReq.URL)
	}
	if ojson.GetBytes(inpaintReq.Body, "images.0.image_url").String() != "https://example.com/portrait.png" {
		t.Fatalf("inpaint images.0.image_url = %q, want https://example.com/portrait.png", ojson.GetBytes(inpaintReq.Body, "images.0.image_url").String())
	}
	if ojson.GetBytes(inpaintReq.Body, "mask.image_url").String() != "https://example.com/mask.png" {
		t.Fatalf("mask.image_url = %q, want https://example.com/mask.png", ojson.GetBytes(inpaintReq.Body, "mask.image_url").String())
	}

	// 1.3 OpenAI Variation (Image only without prompt) - routes to /images/variations
	varInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "91/dall-e-2",
			Provider: "openai-compat",
			Input:    []byte(`{"image":"https://example.com/portrait.png","n":2}`),
		},
	}
	varReq, errVar := driver.PrepareSubmit(ctx, varInput)
	if errVar != nil {
		t.Fatalf("PrepareSubmit variation error: %v", errVar)
	}
	if varReq.URL != "/images/variations" {
		t.Fatalf("URL = %q, want /images/variations", varReq.URL)
	}

	// 2. OpenAI Video (Sora / Async Video)
	vidInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "openai/sora-1.0",
			Provider: "openai-compat",
			Input:    []byte(`{"prompt":"underwater coral reef with glowing jellyfish","seconds":10}`),
		},
	}
	vidReq, errVid := driver.PrepareSubmit(ctx, vidInput)
	if errVid != nil {
		t.Fatalf("PrepareSubmit video error: %v", errVid)
	}
	if vidReq.URL != "/videos/generations" {
		t.Fatalf("URL = %q, want /videos/generations", vidReq.URL)
	}

	// Async Video submit response
	vidResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body:       []byte(`{"id":"vid_sora_abc123","status":"processing"}`),
	}
	vidRes, errVidParse := driver.ParseSubmit(ctx, vidResp)
	if errVidParse != nil {
		t.Fatalf("ParseSubmit video error: %v", errVidParse)
	}
	if vidRes.Status != aigc.StatusRunning || vidRes.ProviderTaskID != "vid_sora_abc123" {
		t.Fatalf("vidRes = %+v", vidRes)
	}

	// Poll Video
	pollReq, errVidPoll := driver.PreparePoll(ctx, aigc.GenerationPollInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindVideo,
			Model:          "openai/sora-1.0",
			Provider:       "openai-compat",
			ProviderTaskID: "vid_sora_abc123",
		},
	})
	if errVidPoll != nil {
		t.Fatalf("PreparePoll video error: %v", errVidPoll)
	}
	if pollReq.URL != "/videos/generations/vid_sora_abc123" {
		t.Fatalf("pollReq.URL = %q, want /videos/generations/vid_sora_abc123", pollReq.URL)
	}

	pollResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body: []byte(`{
			"id": "vid_sora_abc123",
			"status": "completed",
			"data": [
				{"url": "https://api.openai.com/v1/videos/vid_sora_abc123.mp4"}
			]
		}`),
	}
	pollRes, errParsePoll := driver.ParsePoll(ctx, pollResp)
	if errParsePoll != nil {
		t.Fatalf("ParsePoll video error: %v", errParsePoll)
	}
	if pollRes.Status != aigc.StatusSucceeded || len(pollRes.Artifacts) != 1 {
		t.Fatalf("pollRes = %+v", pollRes)
	}
	if (pollRes.Artifacts[0].ArtifactType != "video" && pollRes.Artifacts[0].ArtifactType != "output_video") || pollRes.Artifacts[0].URI != "https://api.openai.com/v1/videos/vid_sora_abc123.mp4" {
		t.Fatalf("Artifact = %+v", pollRes.Artifacts[0])
	}
}

func TestDriverImage26OutputControlsReachUpstream(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"gif", `{"prompt":"a looping star","output_format":"gif","frame_count":4,"frame_duration_ms":150,"complexity":"medium","size":"512x512","n":1,"response_format":"url"}`},
		{"svg", `{"prompt":"a vector star","output_format":"svg","complexity":"medium","n":1,"response_format":"url"}`},
		{"transparent_png", `{"prompt":"a star","output_format":"png","background":"transparent","quality":"medium","size":"1024x1024","n":1,"response_format":"url"}`},
		{"jpeg", `{"prompt":"a star","output_format":"jpeg"}`},
		{"webp", `{"prompt":"a star","output_format":"webp"}`},
		{"legacy", `{"prompt":"a star","quality":"low","size":"1024x1024"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver := aigcdriver.NewDriver()
			req, err := driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{
				Generation: aigc.ContentGeneration{
					Kind: aigc.ContentKindImage, Model: "ze/gpt-image-2.6", Provider: "openai-compat", Input: []byte(tc.input),
				},
			})
			if err != nil {
				t.Fatalf("PrepareSubmit: %v", err)
			}
			if req.URL != "/images/generations" || req.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected transport: %s %v", req.URL, req.Header)
			}
			for _, key := range []string{"prompt", "output_format", "frame_count", "frame_duration_ms", "complexity", "background", "n", "response_format", "quality", "size"} {
				want := ojson.Get(tc.input, key)
				got := ojson.GetBytes(req.Body, key)
				if got.Exists() != want.Exists() || got.Raw != want.Raw {
					t.Errorf("%s = %s, want %s", key, got.Raw, want.Raw)
				}
			}
			if got := ojson.GetBytes(req.Body, "model").String(); got != "ze/gpt-image-2.6" {
				t.Errorf("model = %q", got)
			}
		})
	}
}

func TestDriverParseImage26OutputMIME(t *testing.T) {
	for _, tc := range []struct{ body, mime string }{
		{`{"data":[{"url":"https://example.com/star.gif?signature=abc"}]}`, "image/gif"},
		{`{"data":[{"url":"https://example.com/star.svg?signature=abc"}]}`, "image/svg+xml"},
		{`{"data":[{"url":"https://example.com/star.png"}]}`, "image/png"},
	} {
		t.Run(tc.mime, func(t *testing.T) {
			result, err := aigcdriver.NewDriver().ParseSubmit(context.Background(), aigc.GenerationExecutionResponse{
				StatusCode: http.StatusOK, Metadata: map[string]any{"provider": "openai-compat"}, Body: []byte(tc.body),
			})
			if err != nil || result.Status != aigc.StatusSucceeded || len(result.Artifacts) != 1 {
				t.Fatalf("ParseSubmit = %+v, %v", result, err)
			}
			if result.Artifacts[0].MIMEType != tc.mime {
				t.Errorf("MIMEType = %q, want %q", result.Artifacts[0].MIMEType, tc.mime)
			}
		})
	}
}

func TestImageOperationUsesExplicitEndpoint(t *testing.T) {
	restore := openaicompat.SetReferenceFetcherForTest(func(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
		return []byte("\x89PNG\r\n\x1a\nfake-png"), nil
	})
	defer restore()

	for _, tc := range []struct {
		name, operation, body, path string
		wantErr                     bool
	}{
		{"generation", "generations", `{"prompt":"a star"}`, "/images/generations", false},
		{"edit", "edits", `{"prompt":"edit","images":["https://example.com/a.png"],"output_format":"gif"}`, "/images/edits", false},
		{"generation does not auto-route", "generations", `{"prompt":"edit","images":["https://example.com/a.png"]}`, "", true},
		{"edit needs image", "edits", `{"prompt":"a star"}`, "", true},
		{"edit needs prompt", "edits", `{"images":["https://example.com/a.png"]}`, "", true},
		{"unknown operation", "unknown", `{"prompt":"a star"}`, "", true},
		{"legacy driver caller", "", `{"prompt":"edit","images":["https://example.com/a.png"]}`, "/images/edits", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := aigcdriver.NewDriver().PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{
				Kind: aigc.ContentKindImage, Model: "ze/gpt-image-2.6", Input: []byte(tc.body),
				Metadata: map[string]any{"image_operation": tc.operation},
			}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil {
				if req.URL != tc.path {
					t.Errorf("endpoint = %q, want %q", req.URL, tc.path)
				}
				if tc.path == "/images/edits" {
					if !strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data;") {
						t.Errorf("Content-Type = %q, want multipart/form-data", req.Header.Get("Content-Type"))
					}
				}
				if ojson.GetBytes(req.Body, "image_operation").Exists() {
					t.Error("internal operation leaked into upstream payload")
				}
			}
		})
	}
}

func TestGenericOpenAICompatUsesFixedPaths(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, tc := range []struct {
		kind       aigc.ContentKind
		body, want string
	}{
		{aigc.ContentKindImage, `{"prompt":"test"}`, "/images/generations"},
		{aigc.ContentKindImage, `{"prompt":"test","image":"https://example.com/image.png"}`, "/images/edits"},
		{aigc.ContentKindVideo, `{"prompt":"test","seconds":5}`, "/videos/generations"},
	} {
		req, err := driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: tc.kind, Model: "custom/model", Input: []byte(tc.body)}})
		if err != nil || req.URL != tc.want {
			t.Fatalf("generic path: %+v %v (want %s)", req, err, tc.want)
		}
	}
	req, err := driver.PreparePoll(context.Background(), aigc.GenerationPollInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Provider: "openai-compat", Model: "custom/video-model", ProviderTaskID: "task_test"}})
	if err != nil || req.URL != "/videos/generations/task_test" {
		t.Fatalf("generic poll path: %+v %v", req, err)
	}
}

func TestDriver_URLAndBase64ArtifactHandling(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. Upstream returns direct URL
	urlResp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body: []byte(`{
			"created": 1740000000,
			"data": [
				{"url": "https://images.example.com/generated_cat.png"}
			]
		}`),
	}
	submitRes, errSubmit := driver.ParseSubmit(ctx, urlResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if len(submitRes.Artifacts) != 1 {
		t.Fatalf("Artifacts count = %d, want 1", len(submitRes.Artifacts))
	}
	if submitRes.Artifacts[0].URI != "https://images.example.com/generated_cat.png" {
		t.Fatalf("URI = %s, want https://images.example.com/generated_cat.png", submitRes.Artifacts[0].URI)
	}
	if submitRes.Artifacts[0].StorageProvider != "openai-compat" {
		t.Fatalf("StorageProvider = %s, want openai-compat", submitRes.Artifacts[0].StorageProvider)
	}

	// 2. Upstream returns base64
	b64Resp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body: []byte(`{
			"created": 1740000000,
			"data": [
				{"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}
			]
		}`),
	}
	b64SubmitRes, errB64 := driver.ParseSubmit(ctx, b64Resp)
	if errB64 != nil {
		t.Fatalf("ParseSubmit b64 error: %v", errB64)
	}
	if len(b64SubmitRes.Artifacts) != 1 {
		t.Fatalf("B64 Artifacts count = %d, want 1", len(b64SubmitRes.Artifacts))
	}
	art := b64SubmitRes.Artifacts[0]
	if len(art.URI) > 1024 {
		t.Fatalf("URI length %d exceeds 1024", len(art.URI))
	}
	if !strings.HasPrefix(art.URI, "inline://sha256=") && !strings.HasPrefix(art.URI, "http") {
		t.Fatalf("URI = %s, unexpected format", art.URI)
	}
	if art.SizeBytes <= 0 {
		t.Fatalf("SizeBytes = %d, want > 0", art.SizeBytes)
	}
}

func TestDriver_UsageExtraction(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. Submit response with standard / GPT-Image usage
	submitResp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body: []byte(`{
			"created": 1740000000,
			"data": [{"url": "https://example.com/cat.png"}],
			"usage": {
				"total_tokens": 120,
				"input_tokens": 20,
				"output_tokens": 100,
				"prompt_tokens": 20,
				"completion_tokens": 100
			}
		}`),
	}
	submitRes, errSubmit := driver.ParseSubmit(ctx, submitResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.Metadata == nil || submitRes.Metadata["usage"] == nil {
		t.Fatal("expected usage in submit metadata, got nil")
	}
	usageMap, ok := submitRes.Metadata["usage"].(map[string]any)
	if !ok || usageMap["total_tokens"] != float64(120) {
		t.Fatalf("unexpected usage map in submit: %+v", submitRes.Metadata["usage"])
	}

	// 2. Poll response with usage
	pollResp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
		Metadata:   map[string]any{"provider": "openai-compat"},
		Body: []byte(`{
			"id": "vid_poll_123",
			"status": "completed",
			"data": [{"url": "https://example.com/video.mp4"}],
			"usage": {
				"total_tokens": 500,
				"completion_tokens": 450
			}
		}`),
	}
	pollRes, errPoll := driver.ParsePoll(ctx, pollResp)
	if errPoll != nil {
		t.Fatalf("ParsePoll error: %v", errPoll)
	}
	if pollRes.Metadata == nil || pollRes.Metadata["usage"] == nil {
		t.Fatal("expected usage in poll metadata, got nil")
	}
	pollUsageMap, ok := pollRes.Metadata["usage"].(map[string]any)
	if !ok || pollUsageMap["total_tokens"] != float64(500) {
		t.Fatalf("unexpected usage map in poll: %+v", pollRes.Metadata["usage"])
	}
}

package test

import (
	"context"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestDriver_CancelFlows(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. Qwen Image cancel
	qwenCancel, errQwen := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindImage,
			Model:          "qwen/qwen-image-plus",
			Provider:       "qwen-image",
			ProviderTaskID: "task_123",
		},
	})
	if errQwen != nil || !strings.Contains(qwenCancel.URL, "/tasks/task_123/cancel") {
		t.Fatalf("qwen cancel error: %v, req = %+v", errQwen, qwenCancel)
	}

	// 2. Qwen Wan Video cancel
	qwenWanCancel, errWan := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindVideo,
			Model:          "qwen/wan3.0-video",
			Provider:       "qwen-wan",
			ProviderTaskID: "task_wan_456",
		},
	})
	if errWan != nil || !strings.Contains(qwenWanCancel.URL, "/tasks/task_wan_456/cancel") {
		t.Fatalf("qwen-wan cancel error: %v, req = %+v", errWan, qwenWanCancel)
	}

	// 3. Volcengine Seedance cancel
	volcCancel, errVolc := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindVideo,
			Model:          "volcengine/doubao-seedance-2-5",
			Provider:       "volcengine-seedance",
			ProviderTaskID: "task_456",
		},
	})
	if errVolc != nil || !strings.Contains(volcCancel.URL, "/tasks/task_456/cancel") {
		t.Fatalf("volc cancel error: %v, req = %+v", errVolc, volcCancel)
	}

	// 4. OpenAI Compat cancel
	openAICancel, errOpenAI := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindVideo,
			Model:          "openai/sora-1.0",
			Provider:       "openai-compat",
			ProviderTaskID: "vid_789",
		},
	})
	if errOpenAI != nil || !strings.Contains(openAICancel.URL, "/videos/generations/vid_789/cancel") {
		t.Fatalf("openai cancel error: %v, req = %+v", errOpenAI, openAICancel)
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

func TestDriver_ValidateOpenAIInput_StrictRejection(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. Video Request with non-standard top-level fields: MUST return error
	dirtyVideoInput := []byte(`{
		"prompt": "flying eagle over mountains",
		"seconds": 5,
		"size": "1280x720",
		"model": "volcengine/doubao-seedance-2-5",
		"input_reference": ["https://example.com/eagle.png"],
		"generate_audio": true,
		"camera_fixed": false,
		"custom_junk": 123
	}`)

	videoSubmitInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-5",
			Provider: "volcengine-seedance",
			Input:    dirtyVideoInput,
		},
	}

	_, errVideo := driver.PrepareSubmit(ctx, videoSubmitInput)
	if errVideo == nil {
		t.Fatalf("expected error for non-standard fields in video, got nil")
	}
	if !strings.Contains(errVideo.Error(), "unrecognized parameter") || !strings.Contains(errVideo.Error(), "generate_audio") {
		t.Fatalf("unexpected error message: %v", errVideo)
	}

	// 2. Image Request with non-standard fields: MUST return error
	dirtyImageInput := []byte(`{
		"prompt": "a cute rabbit in cyberpunk city",
		"size": "1024x1024",
		"n": 1,
		"model": "qwen/qwen-image-plus",
		"prompt_extend": true,
		"dirty_field": "remove_me"
	}`)

	imageSubmitInput := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "qwen/qwen-image-plus",
			Provider: "qwen-image",
			Input:    dirtyImageInput,
		},
	}

	_, errImg := driver.PrepareSubmit(ctx, imageSubmitInput)
	if errImg == nil {
		t.Fatalf("expected error for non-standard fields in image, got nil")
	}
	if !strings.Contains(errImg.Error(), "unrecognized parameter") || !strings.Contains(errImg.Error(), "prompt_extend") {
		t.Fatalf("unexpected error message: %v", errImg)
	}

	// 3. Valid standard video request with extra_body: MUST SUCCEED
	cleanVideoInput := []byte(`{
		"prompt": "flying eagle over mountains",
		"seconds": 5,
		"size": "1280x720",
		"model": "volcengine/doubao-seedance-2-5",
		"input_reference": ["https://example.com/eagle.png"],
		"extra_body": {
			"generate_audio": true,
			"camera_fixed": false
		}
	}`)
	cleanVideoSubmit := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-5",
			Provider: "volcengine-seedance",
			Input:    cleanVideoInput,
		},
	}
	reqClean, errClean := driver.PrepareSubmit(ctx, cleanVideoSubmit)
	if errClean != nil {
		t.Fatalf("unexpected error for valid standard request: %v", errClean)
	}
	if ojson.GetBytes(reqClean.Body, "duration").Int() != 5 {
		t.Errorf("duration = %d, want 5", ojson.GetBytes(reqClean.Body, "duration").Int())
	}
	if !ojson.GetBytes(reqClean.Body, "generate_audio").Bool() {
		t.Errorf("generate_audio = false, want true")
	}
}

package openaicompat_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
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

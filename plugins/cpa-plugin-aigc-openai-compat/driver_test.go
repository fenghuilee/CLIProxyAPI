package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

func TestDriver_Supports(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	tests := []struct {
		kind         aigc.ContentKind
		model        string
		wantSupport  bool
		wantProvider string
	}{
		// 1. Qwen Image
		{aigc.ContentKindImage, "qwen/qwen-image-3.0-pro", true, "qwen-image"},
		{aigc.ContentKindImage, "alibaba/qwen-image-max", true, "qwen-image"},
		{aigc.ContentKindImage, "qwen-image-plus", true, "qwen-image"},
		{aigc.ContentKindImage, "wanx2.1-t2i-turbo", true, "qwen-image"},
		{aigc.ContentKindVideo, "qwen/qwen-image-3.0-pro", false, ""},

		// 2. Qwen Wan Video
		{aigc.ContentKindVideo, "wan3.0-video", true, "qwen-wan"},
		{aigc.ContentKindVideo, "wan3.0-video-prime", true, "qwen-wan"},
		{aigc.ContentKindVideo, "qwen/wan3.0-video", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba/wanx2.1-t2v-turbo", true, "qwen-wan"},
		{aigc.ContentKindVideo, "wan2.1-i2v-plus", true, "qwen-wan"},
		{aigc.ContentKindImage, "wan3.0-video", false, ""},

		// 3. Volcengine Seedance Video
		{aigc.ContentKindVideo, "volcengine/doubao-seedance-2-5", true, "volcengine-seedance"},
		{aigc.ContentKindVideo, "doubao-seedance-2-0-mini", true, "volcengine-seedance"},
		{aigc.ContentKindImage, "volcengine/doubao-seedance-2-5", false, ""},

		// 4. Volcengine Seedream Image
		{aigc.ContentKindImage, "volcengine/doubao-seedream-5-0-pro", true, "volcengine-seedream"},
		{aigc.ContentKindImage, "volcengine/doubao-seedream-4-5", true, "volcengine-seedream"},
		{aigc.ContentKindImage, "doubao-seedream-5-0", true, "volcengine-seedream"},
		{aigc.ContentKindVideo, "volcengine/doubao-seedream-5-0-pro", false, ""},

		// 5. OpenAI Compat (Fallback / Explicit)
		{aigc.ContentKindImage, "openai/dall-e-3", true, "openai-compat"},
		{aigc.ContentKindImage, "dall-e-2", true, "openai-compat"},
		{aigc.ContentKindImage, "zeroapi/gpt-image-2", true, "openai-compat"},
		{aigc.ContentKindImage, "siliconflow/flux-1-dev", true, "openai-compat"},
		{aigc.ContentKindVideo, "openai/sora-1.0", true, "openai-compat"},
		{aigc.ContentKindVideo, "xai/grok-video-1.0", true, "openai-compat"},
		{aigc.ContentKindVideo, "kling-v1.5", true, "openai-compat"},

		// 6. Unsupported kinds
		{aigc.ContentKindAudio, "openai/tts-1", false, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind)+"_"+tt.model, func(t *testing.T) {
			res, err := driver.Supports(ctx, aigc.GenerationSupportRequest{
				Kind:  tt.kind,
				Model: tt.model,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Supported != tt.wantSupport {
				t.Fatalf("Supported = %v, want %v", res.Supported, tt.wantSupport)
			}
			if tt.wantSupport && res.Provider != tt.wantProvider {
				t.Fatalf("Provider = %q, want %q", res.Provider, tt.wantProvider)
			}
		})
	}
}

func TestDriver_QwenImageFlow(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. PrepareSubmit
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "qwen/qwen-image-plus",
			Provider: "qwen-image",
			Input:    []byte(`{"prompt":"a cute cat on Mars","size":"1024x1024","n":1,"extra_body":{"prompt_extend":true}}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if !strings.Contains(req.URL, "dashscope.aliyuncs.com") {
		t.Fatalf("URL = %q, want DashScope URL", req.URL)
	}
	if gjson.GetBytes(req.Body, "model").String() != "qwen-image-plus" {
		t.Fatalf("model = %q, want qwen-image-plus", gjson.GetBytes(req.Body, "model").String())
	}
	if gjson.GetBytes(req.Body, "parameters.size").String() != "1024*1024" {
		t.Fatalf("size = %q, want 1024*1024", gjson.GetBytes(req.Body, "parameters.size").String())
	}

	// 2. ParseSubmit (Sync result)
	syncResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"output": {
				"results": [
					{"url": "https://oss.dashscope.com/cat1.png"}
				]
			}
		}`),
	}
	submitRes, errSubmit := driver.ParseSubmit(ctx, syncResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", submitRes.Status)
	}
	if len(submitRes.Artifacts) != 1 || submitRes.Artifacts[0].URI != "https://oss.dashscope.com/cat1.png" {
		t.Fatalf("Artifacts = %+v, want cat1.png", submitRes.Artifacts)
	}

	// 3. ParseSubmit (Async task)
	asyncResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"output": {
				"task_id": "task_qwen_999",
				"task_status": "PENDING"
			}
		}`),
	}
	asyncSubmitRes, errAsync := driver.ParseSubmit(ctx, asyncResp)
	if errAsync != nil {
		t.Fatalf("ParseSubmit async error: %v", errAsync)
	}
	if asyncSubmitRes.Status != aigc.StatusRunning {
		t.Fatalf("Status = %v, want Running", asyncSubmitRes.Status)
	}
	if asyncSubmitRes.ProviderTaskID != "task_qwen_999" {
		t.Fatalf("ProviderTaskID = %q, want task_qwen_999", asyncSubmitRes.ProviderTaskID)
	}

	// 4. Poll
	pollReq, errPoll := driver.PreparePoll(ctx, aigc.GenerationPollInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindImage,
			Model:          "qwen-image-plus",
			Provider:       "qwen-image",
			ProviderTaskID: "task_qwen_999",
		},
	})
	if errPoll != nil {
		t.Fatalf("PreparePoll error: %v", errPoll)
	}
	if !strings.Contains(pollReq.URL, "/tasks/task_qwen_999") {
		t.Fatalf("poll URL = %q, want task url", pollReq.URL)
	}

	pollResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"output": {
				"task_id": "task_qwen_999",
				"task_status": "SUCCEEDED",
				"results": [{"url": "https://oss.dashscope.com/cat_final.png"}]
			}
		}`),
	}
	pollRes, errParsePoll := driver.ParsePoll(ctx, pollResp)
	if errParsePoll != nil {
		t.Fatalf("ParsePoll error: %v", errParsePoll)
	}
	if pollRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", pollRes.Status)
	}
	if len(pollRes.Artifacts) != 1 || pollRes.Artifacts[0].URI != "https://oss.dashscope.com/cat_final.png" {
		t.Fatalf("Artifacts = %+v", pollRes.Artifacts)
	}
}

func TestDriver_QwenWanFlow_TextToVideo(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. PrepareSubmit (Text-to-Video)
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "qwen/wan3.0-video",
			Provider: "qwen-wan",
			Input:    []byte(`{"prompt":"a cute cat running on the roof at night","size":"1280x720","seconds":5,"extra_body":{"prompt_extend":true,"audio":true,"seed":12345}}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if !strings.Contains(req.URL, "dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis") {
		t.Fatalf("URL = %q, want DashScope video synthesis URL", req.URL)
	}
	if req.Header.Get("X-DashScope-Async") != "enable" {
		t.Fatalf("X-DashScope-Async header = %q, want enable", req.Header.Get("X-DashScope-Async"))
	}
	if gjson.GetBytes(req.Body, "model").String() != "wan3.0-video" {
		t.Fatalf("model = %q, want wan3.0-video", gjson.GetBytes(req.Body, "model").String())
	}
	if gjson.GetBytes(req.Body, "input.prompt").String() != "a cute cat running on the roof at night" {
		t.Fatalf("prompt = %q, want expected prompt", gjson.GetBytes(req.Body, "input.prompt").String())
	}
	if gjson.GetBytes(req.Body, "parameters.resolution").String() != "720P" {
		t.Fatalf("resolution = %q, want 720P", gjson.GetBytes(req.Body, "parameters.resolution").String())
	}
	if gjson.GetBytes(req.Body, "parameters.ratio").String() != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", gjson.GetBytes(req.Body, "parameters.ratio").String())
	}
	if gjson.GetBytes(req.Body, "parameters.duration").Int() != 5 {
		t.Fatalf("duration = %d, want 5", gjson.GetBytes(req.Body, "parameters.duration").Int())
	}
	if !gjson.GetBytes(req.Body, "parameters.audio").Bool() {
		t.Fatalf("audio = false, want true")
	}
	if !gjson.GetBytes(req.Body, "parameters.prompt_extend").Bool() {
		t.Fatalf("prompt_extend = false, want true")
	}
	if gjson.GetBytes(req.Body, "parameters.seed").Int() != 12345 {
		t.Fatalf("seed = %d, want 12345", gjson.GetBytes(req.Body, "parameters.seed").Int())
	}

	// 2. ParseSubmit (Async response)
	asyncResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"request_id": "req-wan-123456",
			"output": {
				"task_id": "task_wan_789",
				"task_status": "PENDING"
			}
		}`),
	}
	submitRes, errSubmit := driver.ParseSubmit(ctx, asyncResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.Status != aigc.StatusRunning {
		t.Fatalf("Status = %v, want Running", submitRes.Status)
	}
	if submitRes.ProviderTaskID != "task_wan_789" {
		t.Fatalf("ProviderTaskID = %q, want task_wan_789", submitRes.ProviderTaskID)
	}

	// 3. PreparePoll
	pollReq, errPoll := driver.PreparePoll(ctx, aigc.GenerationPollInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindVideo,
			Model:          "wan3.0-video",
			Provider:       "qwen-wan",
			ProviderTaskID: "task_wan_789",
		},
	})
	if errPoll != nil {
		t.Fatalf("PreparePoll error: %v", errPoll)
	}
	if !strings.Contains(pollReq.URL, "/tasks/task_wan_789") {
		t.Fatalf("poll URL = %q, want /tasks/task_wan_789", pollReq.URL)
	}

	// 4. ParsePoll (SUCCEEDED with Usage)
	pollResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "qwen-wan", "task_id": "task_wan_789"},
		Body: []byte(`{
			"request_id": "req-wan-123456",
			"output": {
				"task_id": "task_wan_789",
				"task_status": "SUCCEEDED",
				"video_url": "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/wan/output.mp4"
			},
			"usage": {
				"video_count": 1,
				"duration": 5,
				"fps": 30,
				"SR": 720,
				"ratio": "16:9"
			}
		}`),
	}
	pollRes, errParsePoll := driver.ParsePoll(ctx, pollResp)
	if errParsePoll != nil {
		t.Fatalf("ParsePoll error: %v", errParsePoll)
	}
	if pollRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", pollRes.Status)
	}
	if len(pollRes.Artifacts) != 1 {
		t.Fatalf("Artifacts count = %d, want 1", len(pollRes.Artifacts))
	}
	if pollRes.Artifacts[0].URI != "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/wan/output.mp4" {
		t.Fatalf("URI = %q, want output.mp4 URL", pollRes.Artifacts[0].URI)
	}
	if pollRes.Artifacts[0].StorageProvider != "dashscope" {
		t.Fatalf("StorageProvider = %q, want dashscope", pollRes.Artifacts[0].StorageProvider)
	}
	if pollRes.Artifacts[0].MIMEType != "video/mp4" {
		t.Fatalf("MIMEType = %q, want video/mp4", pollRes.Artifacts[0].MIMEType)
	}
}

func TestDriver_QwenWanFlow_FirstFrameAndLastFrame(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// First & last frame generation
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "wan3.0-video",
			Provider: "qwen-wan",
			Input: []byte(`{
				"prompt": "A young girl starts smiling and transitions to laughing",
				"extra_body": {
					"first_frame": "https://example.com/first.png",
					"last_frame": "https://example.com/last.png"
				}
			}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	mediaItems := gjson.GetBytes(req.Body, "input.media").Array()
	if len(mediaItems) != 2 {
		t.Fatalf("media count = %d, want 2", len(mediaItems))
	}
	if mediaItems[0].Get("type").String() != "first_frame" || mediaItems[0].Get("url").String() != "https://example.com/first.png" {
		t.Fatalf("media[0] = %+v, want first_frame", mediaItems[0])
	}
	if mediaItems[1].Get("type").String() != "last_frame" || mediaItems[1].Get("url").String() != "https://example.com/last.png" {
		t.Fatalf("media[1] = %+v, want last_frame", mediaItems[1])
	}
}

func TestDriver_QwenWanFlow_MultiReferenceAndFiles(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// Multi-reference mode (Image, Video, Audio, File, Link)
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "alibaba/wan3.0-video-prime",
			Provider: "qwen-wan",
			Input: []byte(`{
				"prompt": "Video1 with Image1 playing acoustic guitar according to Document1",
				"input_reference": [
					"https://example.com/girl.jpg",
					"https://example.com/role.mp4",
					"https://example.com/audio.wav",
					"https://example.com/glass.pptx"
				]
			}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if gjson.GetBytes(req.Body, "model").String() != "wan3.0-video-prime" {
		t.Fatalf("model = %q, want wan3.0-video-prime", gjson.GetBytes(req.Body, "model").String())
	}

	mediaItems := gjson.GetBytes(req.Body, "input.media").Array()
	if len(mediaItems) != 4 {
		t.Fatalf("media count = %d, want 4", len(mediaItems))
	}
	if mediaItems[0].Get("type").String() != "reference_image" {
		t.Fatalf("media[0] type = %q, want reference_image", mediaItems[0].Get("type").String())
	}
	if mediaItems[1].Get("type").String() != "reference_video" {
		t.Fatalf("media[1] type = %q, want reference_video", mediaItems[1].Get("type").String())
	}
	if mediaItems[2].Get("type").String() != "reference_audio" {
		t.Fatalf("media[2] type = %q, want reference_audio", mediaItems[2].Get("type").String())
	}
	if mediaItems[3].Get("type").String() != "file" {
		t.Fatalf("media[3] type = %q, want file", mediaItems[3].Get("type").String())
	}
}

func TestDriver_QwenWanFlow_MutualExclusion(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// Mutual exclusion violation: passing first_frame and reference_image together in media
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "wan3.0-video",
			Provider: "qwen-wan",
			Input: []byte(`{
				"prompt": "invalid mixed request",
				"extra_body": {
					"media": [
						{"type": "first_frame", "url": "https://example.com/first.png"},
						{"type": "reference_video", "url": "https://example.com/ref.mp4"}
					]
				}
			}`),
		},
	}

	_, err := driver.PrepareSubmit(ctx, input)
	if err == nil {
		t.Fatalf("expected error for mutually exclusive media types, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestDriver_QwenWanFlow_PollStatuses(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. RUNNING
	runningResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "qwen-wan"},
		Body: []byte(`{
			"output": {
				"task_id": "task_wan_1",
				"task_status": "RUNNING"
			}
		}`),
	}
	runRes, errRun := driver.ParsePoll(ctx, runningResp)
	if errRun != nil || runRes.Status != aigc.StatusRunning || runRes.Progress != 50 {
		t.Fatalf("RUNNING parse failed: res=%+v, err=%v", runRes, errRun)
	}

	// 2. FAILED
	failedResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "qwen-wan"},
		Body: []byte(`{
			"output": {
				"task_id": "task_wan_2",
				"task_status": "FAILED",
				"code": "InvalidParameter",
				"message": "The two modes are mutually exclusive."
			}
		}`),
	}
	failRes, errFail := driver.ParsePoll(ctx, failedResp)
	if errFail != nil || failRes.Status != aigc.StatusFailed {
		t.Fatalf("FAILED parse failed: res=%+v, err=%v", failRes, errFail)
	}

	// 3. CANCELED
	canceledResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "qwen-wan"},
		Body: []byte(`{
			"output": {
				"task_id": "task_wan_3",
				"task_status": "CANCELED"
			}
		}`),
	}
	cancelRes, errCancel := driver.ParsePoll(ctx, canceledResp)
	if errCancel != nil || cancelRes.Status != aigc.StatusCanceled {
		t.Fatalf("CANCELED parse failed: res=%+v, err=%v", cancelRes, errCancel)
	}
}

func TestDriver_VolcengineSeedanceFlow(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. PrepareSubmit
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-5",
			Provider: "volcengine-seedance",
			Input:    []byte(`{"prompt":"a cinematic drone flying over mountains","seconds":5,"size":"1920x1080"}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if gjson.GetBytes(req.Body, "model").String() != "doubao-seedance-2-5" {
		t.Fatalf("model = %q, want doubao-seedance-2-5", gjson.GetBytes(req.Body, "model").String())
	}
	if gjson.GetBytes(req.Body, "ratio").String() != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", gjson.GetBytes(req.Body, "ratio").String())
	}
	if gjson.GetBytes(req.Body, "resolution").String() != "1080p" {
		t.Fatalf("resolution = %q, want 1080p", gjson.GetBytes(req.Body, "resolution").String())
	}

	// 2. ParseSubmit (Task ID returned)
	submitResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"id":"task_seedance_123","status":"running"}`),
	}
	submitRes, errSubmit := driver.ParseSubmit(ctx, submitResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.ProviderTaskID != "task_seedance_123" {
		t.Fatalf("ProviderTaskID = %q, want task_seedance_123", submitRes.ProviderTaskID)
	}

	// 3. Poll Complete
	pollResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"id": "task_seedance_123",
			"status": "succeeded",
			"content": {
				"video_url": "https://tos.volces.com/drone_1080p.mp4",
				"last_frame_url": "https://tos.volces.com/last_frame.png"
			}
		}`),
	}
	pollRes, errPoll := driver.ParsePoll(ctx, pollResp)
	if errPoll != nil {
		t.Fatalf("ParsePoll error: %v", errPoll)
	}
	if pollRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", pollRes.Status)
	}
	if len(pollRes.Artifacts) != 2 {
		t.Fatalf("Artifacts count = %d, want 2", len(pollRes.Artifacts))
	}
	if pollRes.Artifacts[0].ArtifactType != "output_video" || pollRes.Artifacts[0].URI != "https://tos.volces.com/drone_1080p.mp4" {
		t.Fatalf("Artifact[0] = %+v", pollRes.Artifacts[0])
	}
}

func TestDriver_VolcengineSeedanceFlow_StandardOpenAIAndMultiReference(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. OpenAI format with multi-image input_reference and extra_body
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-0-mini",
			Provider: "volcengine-seedance",
			Input: []byte(`{
				"model": "volcengine/doubao-seedance-2-0-mini",
				"prompt": "【参考图】@图片1 和 @图片2 对话",
				"seconds": "15",
				"size": "1280x720",
				"input_reference": [
					"asset://asset-1",
					"asset://asset-2",
					"asset://asset-3"
				],
				"extra_body": {
					"generate_audio": true,
					"return_last_frame": true,
					"tools": [{"type": "web_search"}]
				}
			}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	if gjson.GetBytes(req.Body, "model").String() != "doubao-seedance-2-0-mini" {
		t.Fatalf("model = %q, want doubao-seedance-2-0-mini", gjson.GetBytes(req.Body, "model").String())
	}
	if gjson.GetBytes(req.Body, "duration").Int() != 15 {
		t.Fatalf("duration = %d, want 15", gjson.GetBytes(req.Body, "duration").Int())
	}
	if gjson.GetBytes(req.Body, "ratio").String() != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", gjson.GetBytes(req.Body, "ratio").String())
	}
	if gjson.GetBytes(req.Body, "resolution").String() != "720p" {
		t.Fatalf("resolution = %q, want 720p", gjson.GetBytes(req.Body, "resolution").String())
	}
	if !gjson.GetBytes(req.Body, "generate_audio").Bool() {
		t.Fatalf("generate_audio = false, want true")
	}
	if !gjson.GetBytes(req.Body, "return_last_frame").Bool() {
		t.Fatalf("return_last_frame = false, want true")
	}

	contentItems := gjson.GetBytes(req.Body, "content").Array()
	if len(contentItems) != 4 {
		t.Fatalf("content items count = %d, want 4", len(contentItems))
	}
	if contentItems[0].Get("type").String() != "text" || contentItems[0].Get("text").String() != "【参考图】@图片1 和 @图片2 对话" {
		t.Fatalf("content[0] text = %+v", contentItems[0])
	}
	// Verify that ALL reference images have role == "reference_image" and NONE has "last_frame"
	for i := 1; i <= 3; i++ {
		item := contentItems[i]
		if item.Get("type").String() != "image_url" {
			t.Fatalf("content[%d] type = %q, want image_url", i, item.Get("type").String())
		}
		if item.Get("role").String() != "reference_image" {
			t.Fatalf("content[%d] role = %q, want reference_image (MUST NOT be last_frame)", i, item.Get("role").String())
		}
	}
}

func TestDriver_VolcengineSeedanceFlow_MultimodalReference(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-5",
			Provider: "volcengine-seedance",
			Input: []byte(`{
				"prompt": "Multi-modal video with image, video and audio reference",
				"seconds": 10,
				"input_reference": [
					"https://example.com/character.png",
					"https://example.com/motion.mp4",
					"https://example.com/voice.mp3"
				]
			}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}

	contentItems := gjson.GetBytes(req.Body, "content").Array()
	if len(contentItems) != 4 {
		t.Fatalf("content items count = %d, want 4", len(contentItems))
	}

	// 1. Image
	if contentItems[1].Get("type").String() != "image_url" || contentItems[1].Get("role").String() != "reference_image" {
		t.Fatalf("content[1] = %+v, want image_url with reference_image", contentItems[1])
	}
	// 2. Video
	if contentItems[2].Get("type").String() != "video_url" || contentItems[2].Get("role").String() != "reference_video" {
		t.Fatalf("content[2] = %+v, want video_url with reference_video", contentItems[2])
	}
	// 3. Audio
	if contentItems[3].Get("type").String() != "audio_url" || contentItems[3].Get("role").String() != "reference_audio" {
		t.Fatalf("content[3] = %+v, want audio_url with reference_audio", contentItems[3])
	}
}

func TestDriver_VolcengineSeedanceFlow_StandardOpenAISizeAndSecondsMapping(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	tests := []struct {
		name           string
		inputJSON      string
		wantDuration   int64
		wantRatio      string
		wantResolution string
	}{
		{
			name:           "1080p landscape 16:9",
			inputJSON:      `{"prompt":"landscape 1080p","seconds":8,"size":"1920x1080"}`,
			wantDuration:   8,
			wantRatio:      "16:9",
			wantResolution: "1080p",
		},
		{
			name:           "720p portrait 9:16",
			inputJSON:      `{"prompt":"portrait 720p","seconds":6,"size":"720x1280"}`,
			wantDuration:   6,
			wantRatio:      "9:16",
			wantResolution: "720p",
		},
		{
			name:           "480p landscape 16:9 854x480",
			inputJSON:      `{"prompt":"landscape 480p","seconds":9,"size":"854x480"}`,
			wantDuration:   9,
			wantRatio:      "16:9",
			wantResolution: "480p",
		},
		{
			name:           "480p portrait 9:16 480x854",
			inputJSON:      `{"prompt":"portrait 480p","seconds":5,"size":"480x854"}`,
			wantDuration:   5,
			wantRatio:      "9:16",
			wantResolution: "480p",
		},
		{
			name:           "480p 4:3 640x480",
			inputJSON:      `{"prompt":"4:3 480p","seconds":5,"size":"640x480"}`,
			wantDuration:   5,
			wantRatio:      "4:3",
			wantResolution: "480p",
		},
		{
			name:           "4k landscape 16:9 3840x2160",
			inputJSON:      `{"prompt":"4k landscape","seconds":5,"size":"3840x2160"}`,
			wantDuration:   5,
			wantRatio:      "16:9",
			wantResolution: "4k",
		},
		{
			name:           "square 1:1 with extra_body ratio override",
			inputJSON:      `{"prompt":"square","seconds":4,"size":"1024x1024","extra_body":{"ratio":"adaptive"}}`,
			wantDuration:   4,
			wantRatio:      "adaptive",
			wantResolution: "720p",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := aigc.GenerationSubmitInput{
				Generation: aigc.ContentGeneration{
					Kind:     aigc.ContentKindVideo,
					Model:    "volcengine/doubao-seedance-2-5",
					Provider: "volcengine-seedance",
					Input:    []byte(tc.inputJSON),
				},
			}
			req, err := driver.PrepareSubmit(ctx, input)
			if err != nil {
				t.Fatalf("PrepareSubmit error: %v", err)
			}
			if gjson.GetBytes(req.Body, "duration").Int() != tc.wantDuration {
				t.Errorf("duration = %d, want %d", gjson.GetBytes(req.Body, "duration").Int(), tc.wantDuration)
			}
			if gjson.GetBytes(req.Body, "ratio").String() != tc.wantRatio {
				t.Errorf("ratio = %q, want %q", gjson.GetBytes(req.Body, "ratio").String(), tc.wantRatio)
			}
			if gjson.GetBytes(req.Body, "resolution").String() != tc.wantResolution {
				t.Errorf("resolution = %q, want %q", gjson.GetBytes(req.Body, "resolution").String(), tc.wantResolution)
			}
		})
	}
}

func TestDriver_VolcengineSeedanceFlow_WithCanonicalMetadata(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-2-0-mini",
			Provider: "volcengine-seedance",
			Input:    []byte(`{"prompt":"landscape video","size":"854x480","seconds":9}`),
			Metadata: map[string]any{
				"canonical": map[string]any{
					"resolution": "480p",
					"ratio":      "16:9",
					"duration":   9,
					"pixels":     409920,
					"width":      854,
					"height":     480,
				},
			},
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}
	if gjson.GetBytes(req.Body, "resolution").String() != "480p" {
		t.Errorf("resolution = %q, want 480p", gjson.GetBytes(req.Body, "resolution").String())
	}
	if gjson.GetBytes(req.Body, "ratio").String() != "16:9" {
		t.Errorf("ratio = %q, want 16:9", gjson.GetBytes(req.Body, "ratio").String())
	}
	if gjson.GetBytes(req.Body, "duration").Int() != 9 {
		t.Errorf("duration = %d, want 9", gjson.GetBytes(req.Body, "duration").Int())
	}
}

func TestDriver_VolcengineSeedreamFlow_WithLayerDecomposition(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. PrepareSubmit
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "volcengine/doubao-seedream-5-0-pro",
			Provider: "volcengine-seedream",
			Input:    []byte(`{"prompt":"a fantasy castle","size":"2048x2048","extra_body":{"layer_decomposition":true}}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}
	if gjson.GetBytes(req.Body, "model").String() != "doubao-seedream-5-0-pro" {
		t.Fatalf("model = %q, want doubao-seedream-5-0-pro", gjson.GetBytes(req.Body, "model").String())
	}
	if gjson.GetBytes(req.Body, "size").String() != "2048x2048" {
		t.Fatalf("size = %q, want 2048x2048", gjson.GetBytes(req.Body, "size").String())
	}
	if !gjson.GetBytes(req.Body, "layer_decomposition").Bool() {
		t.Fatalf("layer_decomposition = false, want true")
	}

	// 2. ParseSubmit with Layer Decomposition data
	submitResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"data": [
				{"url": "https://tos.volces.com/full.png"},
				{"url": "https://tos.volces.com/layer_bg.png", "layer_id": 1, "layer_name": "background", "layer_type": "background"},
				{"url": "https://tos.volces.com/layer_castle.png", "layer_id": 2, "layer_name": "castle", "layer_type": "foreground"}
			]
		}`),
	}

	submitRes, errSubmit := driver.ParseSubmit(ctx, submitResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", submitRes.Status)
	}
	if len(submitRes.Artifacts) != 3 {
		t.Fatalf("Artifacts count = %d, want 3", len(submitRes.Artifacts))
	}
	if submitRes.Artifacts[1].ArtifactType != "layer" || submitRes.Artifacts[1].Metadata["layer_name"] != "background" {
		t.Fatalf("Layer 1 metadata = %+v", submitRes.Artifacts[1].Metadata)
	}
}

func TestDriver_OpenAICompatFlow(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

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
	if gjson.GetBytes(img2imgReq.Body, "images.0.image_url").String() != "https://example.com/portrait.png" {
		t.Fatalf("images.0.image_url = %q, want https://example.com/portrait.png", gjson.GetBytes(img2imgReq.Body, "images.0.image_url").String())
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
	if gjson.GetBytes(inpaintReq.Body, "images.0.image_url").String() != "https://example.com/portrait.png" {
		t.Fatalf("inpaint images.0.image_url = %q, want https://example.com/portrait.png", gjson.GetBytes(inpaintReq.Body, "images.0.image_url").String())
	}
	if gjson.GetBytes(inpaintReq.Body, "mask.image_url").String() != "https://example.com/mask.png" {
		t.Fatalf("mask.image_url = %q, want https://example.com/mask.png", gjson.GetBytes(inpaintReq.Body, "mask.image_url").String())
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

func TestDriver_CancelFlows(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. Qwen Image cancel
	qwenCancel, errQwen := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{
		Generation: aigc.ContentGeneration{
			Kind:           aigc.ContentKindImage,
			Model:          "qwen-image-plus",
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
			Model:          "wan3.0-video",
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

func TestDriver_CustomConfigReconfigure(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// Disable Qwen and test supports
	cfg := DefaultConfig()
	f := false
	cfg.QwenImage.Enabled = &f
	driver.SetConfig(cfg)

	res, err := driver.Supports(ctx, aigc.GenerationSupportRequest{
		Kind:  aigc.ContentKindImage,
		Model: "qwen/qwen-image-3.0-pro",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Since Qwen is disabled, it falls back to openai-compat
	if res.Provider != "openai-compat" {
		t.Fatalf("Provider = %q, want openai-compat", res.Provider)
	}
}

func TestDriver_URLAndBase64ArtifactHandling(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	// 1. Upstream returns direct URL: should passthrough URL directly without TOS
	urlResp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
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

	// 2. Upstream returns base64 (without TOS configured): should safely create inline artifact with sha256
	b64Resp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
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

func TestDriver_SeedancePollFailure(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

	failedPollResp := aigc.GenerationExecutionResponse{
		StatusCode: 200,
		Metadata:   map[string]any{"provider": "volcengine-seedance", "task_id": "cgt-20260825102931-gd2rv"},
		Body: []byte(`{
			"id": "cgt-20260825102931-gd2rv",
			"model": "doubao-seedance-2-0-mini-260615",
			"status": "failed",
			"error": {
				"code": "OutputVideoSensitiveContentDetected.PolicyViolation",
				"message": "The request failed because the output video may be related to copyright restrictions."
			}
		}`),
	}

	pollRes, err := driver.ParsePoll(ctx, failedPollResp)
	if err != nil {
		t.Fatalf("ParsePoll failed error: %v", err)
	}
	if pollRes.Status != aigc.StatusFailed {
		t.Fatalf("Status = %v, want %v", pollRes.Status, aigc.StatusFailed)
	}
	if pollRes.Stage != aigc.StageFailed {
		t.Fatalf("Stage = %v, want %v", pollRes.Stage, aigc.StageFailed)
	}
	if pollRes.ErrorCode != aigc.ErrCodeCopyrightViolation {
		t.Fatalf("ErrorCode = %q, want %q", pollRes.ErrorCode, aigc.ErrCodeCopyrightViolation)
	}
	if pollRes.ErrorMessage == "" {
		t.Fatalf("ErrorMessage is empty")
	}
}

func TestDriver_ValidateOpenAIInput_StrictRejection(t *testing.T) {
	ctx := context.Background()
	driver := NewDriver()

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
	if gjson.GetBytes(reqClean.Body, "duration").Int() != 5 {
		t.Errorf("duration = %d, want 5", gjson.GetBytes(reqClean.Body, "duration").Int())
	}
	if !gjson.GetBytes(reqClean.Body, "generate_audio").Bool() {
		t.Errorf("generate_audio = false, want true")
	}
}

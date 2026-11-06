package qwen_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/qwen"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestDriver_QwenImageFlow(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

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
	if ojson.GetBytes(req.Body, "model").String() != "qwen-image-plus" {
		t.Fatalf("model = %q, want qwen-image-plus", ojson.GetBytes(req.Body, "model").String())
	}
	if ojson.GetBytes(req.Body, "parameters.size").String() != "1024*1024" {
		t.Fatalf("size = %q, want 1024*1024", ojson.GetBytes(req.Body, "parameters.size").String())
	}

	// 2. ParseSubmit (Sync result)
	syncResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "qwen-image"},
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
		Metadata:   map[string]any{"provider": "qwen-image"},
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
			Model:          "qwen/qwen-image-plus",
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
		Metadata:   map[string]any{"provider": "qwen-image"},
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
	driver := aigcdriver.NewDriver()

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
	if ojson.GetBytes(req.Body, "model").String() != "wan3.0-video" {
		t.Fatalf("model = %q, want wan3.0-video", ojson.GetBytes(req.Body, "model").String())
	}
	if ojson.GetBytes(req.Body, "input.prompt").String() != "a cute cat running on the roof at night" {
		t.Fatalf("prompt = %q, want expected prompt", ojson.GetBytes(req.Body, "input.prompt").String())
	}
	if ojson.GetBytes(req.Body, "parameters.resolution").String() != "720P" {
		t.Fatalf("resolution = %q, want 720P", ojson.GetBytes(req.Body, "parameters.resolution").String())
	}
	if ojson.GetBytes(req.Body, "parameters.ratio").String() != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", ojson.GetBytes(req.Body, "parameters.ratio").String())
	}
	if ojson.GetBytes(req.Body, "parameters.duration").Int() != 5 {
		t.Fatalf("duration = %d, want 5", ojson.GetBytes(req.Body, "parameters.duration").Int())
	}
	if !ojson.GetBytes(req.Body, "parameters.audio").Bool() {
		t.Fatalf("audio = false, want true")
	}
	if !ojson.GetBytes(req.Body, "parameters.prompt_extend").Bool() {
		t.Fatalf("prompt_extend = false, want true")
	}
	if ojson.GetBytes(req.Body, "parameters.seed").Int() != 12345 {
		t.Fatalf("seed = %d, want 12345", ojson.GetBytes(req.Body, "parameters.seed").Int())
	}

	// 2. ParseSubmit (Async response)
	asyncResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "qwen-wan"},
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
			Model:          "qwen/wan3.0-video",
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
	driver := aigcdriver.NewDriver()

	// First & last frame generation
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "qwen/wan3.0-video",
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

	mediaItems := ojson.GetBytes(req.Body, "input.media").Array()
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
	driver := aigcdriver.NewDriver()

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

	if ojson.GetBytes(req.Body, "model").String() != "wan3.0-video-prime" {
		t.Fatalf("model = %q, want wan3.0-video-prime", ojson.GetBytes(req.Body, "model").String())
	}

	mediaItems := ojson.GetBytes(req.Body, "input.media").Array()
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
	driver := aigcdriver.NewDriver()

	// Mutual exclusion violation: passing first_frame and reference_image together in media
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindVideo,
			Model:    "qwen/wan3.0-video",
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
	driver := aigcdriver.NewDriver()

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

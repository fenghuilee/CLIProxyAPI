package main

import (
	"context"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/bananarouter"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestCDPModelRoutingAndUpstreamNames(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, prefix := range []string{"cdp", "CDP", "chinadatapay"} {
		for _, tc := range []struct {
			model, provider, path, input string
		}{
			{"doubao-seedance-2.0-aicc", "chinadatapay-seedance", "/api/v3/contents/generations/tasks", `{"prompt":"cat","seconds":5,"extra_body":{"omni_reference_task_type":"reference"}}`},
			{"doubao-seedance-2-5", "chinadatapay-seedance", "/api/v3/contents/generations/tasks", `{"prompt":"cat","seconds":5}`},
			{"wan3.0-video", "chinadatapay-wan", "/api/v1/services/aigc/video-generation/video-synthesis", `{"prompt":"cat","seconds":5}`},
			{"wan3.0-video-prime", "chinadatapay-wan", "/api/v1/services/aigc/video-generation/video-synthesis", `{"prompt":"cat","seconds":5}`},
			{"MiniMax-H3", "chinadatapay-minimax-h3", "/v2/video_generation", `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"768P"}}`},
		} {
			t.Run(prefix+"/"+tc.model, func(t *testing.T) {
				model := prefix + "/" + tc.model
				support, err := driver.Supports(context.Background(), aigc.GenerationSupportRequest{Kind: aigc.ContentKindVideo, Model: model})
				if err != nil || support.Provider != tc.provider {
					t.Fatalf("routing: %+v %v (want %s)", support, err, tc.provider)
				}
				req, err := driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: model, Input: []byte(tc.input)}})
				if err != nil {
					t.Fatal(err)
				}
				if req.URL != "https://tp-api.chinadatapay.com:8000"+tc.path || req.Model != model || ojson.GetBytes(req.Body, "model").String() != tc.model {
					t.Fatalf("upstream model/endpoint changed: %+v, %s", req, req.Body)
				}
				if tc.model == "doubao-seedance-2.0-aicc" && ojson.GetBytes(req.Body, "omni_reference_task_type").String() != "reference" {
					t.Fatal("task type was lost")
				}
			})
		}
	}
}

func TestCDPArtifactsAndLockedPolling(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, tc := range []struct{ model, provider, path, response string }{
		{"doubao-seedance-2.0-aicc", "chinadatapay-seedance", "/api/v3/contents/generations/tasks/task_cdp", `{"status":"succeeded","content":{"video_url":"https://example.com/video.mp4","last_frame_url":"https://example.com/frame.png"}}`},
		{"wan3.0-video", "chinadatapay-wan", "/api/v1/tasks/task_cdp", `{"output":{"task_id":"task_cdp","task_status":"SUCCEEDED","video_url":"https://example.com/video.mp4"}}`},
		{"MiniMax-H3", "chinadatapay-minimax-h3", "/v2/query/video_generation/task_cdp", `{"task":{"id":"task_cdp","status":"succeeded","content":{"url":"https://example.com/video.mp4"}}}`},
	} {
		for _, prefix := range []string{"cdp", "chinadatapay"} {
			t.Run(prefix+"/"+tc.model, func(t *testing.T) {
				gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: prefix + "/" + tc.model, ProviderTaskID: "task_cdp", Metadata: map[string]any{"aigc_adapter": tc.provider}}
				req, err := driver.PreparePoll(context.Background(), aigc.GenerationPollInput{Generation: gen})
				if err != nil || !strings.HasSuffix(req.URL, tc.path) {
					t.Fatalf("locked task could not be queried: %+v %v", req, err)
				}
				result, err := driver.ParsePoll(context.Background(), aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(tc.response), Metadata: req.Metadata})
				if err != nil || result.Status != aigc.StatusSucceeded || len(result.Artifacts) == 0 {
					t.Fatalf("poll: %+v %v", result, err)
				}
				for _, artifact := range result.Artifacts {
					if artifact.StorageProvider != "cdp" {
						t.Fatalf("public artifact exposes legacy provider: %+v", artifact)
					}
				}
			})
		}
	}
}

func TestBuiltInCDPModelsAreNotWidened(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, model := range []string{"cdp/doubao-seedance-2-5", "chinadatapay/unknown-model", "other/doubao-seedance-2.0-aicc"} {
		support, err := driver.Supports(context.Background(), aigc.GenerationSupportRequest{Kind: aigc.ContentKindVideo, Model: model})
		if err != nil {
			t.Fatal(err)
		}
		if model == "cdp/doubao-seedance-2-5" && support.Provider != "chinadatapay-seedance" {
			t.Fatalf("built-in route not matched: %+v", support)
		}
		if model != "cdp/doubao-seedance-2-5" && support.Provider == "chinadatapay-seedance" {
			t.Fatalf("unexpected relay route for %s", model)
		}
	}
}

func TestDriver_Supports(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	tests := []struct {
		kind         aigc.ContentKind
		model        string
		wantSupport  bool
		wantProvider string
	}{
		// 1. Qwen Image
		{aigc.ContentKindImage, "qwen/qwen-image-3.0-pro", true, "qwen-image"},
		{aigc.ContentKindImage, "alibaba/qwen-image-max", true, "qwen-image"},
		{aigc.ContentKindImage, "qwen/qwen-image-plus", true, "qwen-image"},
		{aigc.ContentKindImage, "qwen/wanx2.1-t2i-turbo", true, "qwen-image"},

		// 2. Qwen Wan Video
		{aigc.ContentKindVideo, "qwen/wan3.0-video", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba/wan3.0-video-prime", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba-cn/wan2.1-i2v-plus", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba/wanx2.1-t2v-turbo", true, "qwen-wan"},

		// 3. Volcengine Seedance Video
		{aigc.ContentKindVideo, "volcengine/doubao-seedance-2-5", true, "volcengine-seedance"},
		{aigc.ContentKindVideo, "volcengine/doubao-seedance-2-0-mini", true, "volcengine-seedance"},

		// 4. Volcengine Seedream Image
		{aigc.ContentKindImage, "volcengine/doubao-seedream-5-0-pro", true, "volcengine-seedream"},
		{aigc.ContentKindImage, "volcengine/doubao-seedream-4-5", true, "volcengine-seedream"},
		{aigc.ContentKindImage, "volcengine/doubao-seedream-5-0", true, "volcengine-seedream"},

		// 5. Bare models explicit fallback to openai-compat
		{aigc.ContentKindImage, "qwen-image-plus", true, "openai-compat"},
		{aigc.ContentKindImage, "wanx2.1-t2i-turbo", true, "openai-compat"},
		{aigc.ContentKindVideo, "wan3.0-video", true, "openai-compat"},
		{aigc.ContentKindVideo, "wan3.0-video-prime", true, "openai-compat"},
		{aigc.ContentKindVideo, "wan2.1-i2v-plus", true, "openai-compat"},
		{aigc.ContentKindVideo, "doubao-seedance-2-0-mini", true, "openai-compat"},
		{aigc.ContentKindImage, "doubao-seedream-5-0", true, "openai-compat"},

		// ZeroAPI SD2.5 video variants use the same dedicated adapter.
		{aigc.ContentKindVideo, "ze/sd2.5", true, "zeroapi-video-v2"},
		{aigc.ContentKindVideo, "ze/sd2.5-antiao", true, "zeroapi-video-v2"},
		{aigc.ContentKindVideo, "ze/SD2.5-Flash-720p", true, "zeroapi-video-v2"},
		{aigc.ContentKindVideo, "zeroapi/sd2.5-antiao", true, "openai-compat"},

		// ZeroAPI GPT Image uses the dedicated JSON-only image adapter.
		{aigc.ContentKindImage, "ze/gpt-image-2.6-sunburst", true, "zeroapi-image"},
		{aigc.ContentKindImage, "ze/gpt-image-2.6", true, "zeroapi-image"},
		{aigc.ContentKindImage, "zeroapi/gpt-image-2", true, "zeroapi-image"},

		// 6. OpenAI Compat (Fallback / Explicit)
		{aigc.ContentKindImage, "openai/dall-e-3", true, "openai-compat"},
		{aigc.ContentKindImage, "dall-e-2", true, "openai-compat"},
		{aigc.ContentKindVideo, "sd2.5-antiao", true, "openai-compat"},
		{aigc.ContentKindImage, "siliconflow/flux-1-dev", true, "openai-compat"},
		{aigc.ContentKindVideo, "openai/sora-1.0", true, "openai-compat"},
		{aigc.ContentKindVideo, "xai/grok-video-1.0", true, "openai-compat"},
		{aigc.ContentKindVideo, "kling-v1.5", true, "openai-compat"},

		// 7. Fallback does not infer the content kind from the model name.
		{aigc.ContentKindVideo, "qwen/qwen-image-3.0-pro", true, "openai-compat"},
		{aigc.ContentKindImage, "qwen/wan3.0-video", true, "openai-compat"},
		{aigc.ContentKindImage, "volcengine/doubao-seedance-2-5", true, "openai-compat"},
		{aigc.ContentKindVideo, "volcengine/doubao-seedream-5-0-pro", true, "openai-compat"},

		// 8. Unsupported kinds and empty model names.
		{aigc.ContentKindAudio, "openai/tts-1", false, ""},
		{aigc.ContentKindImage, " ", false, ""},
		{aigc.ContentKindVideo, "", false, ""},
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

func TestFixedVendorEndpoints(t *testing.T) {
	driver := aigcdriver.NewDriver()
	relayTasksURL := chinadatapay.ChinaDataPaySeedanceTasksURL
	for _, tc := range []struct {
		name, model, submit, poll string
		kind                      aigc.ContentKind
	}{
		{"qwen-image", "qwen/qwen-image-plus", "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", "https://dashscope.aliyuncs.com/api/v1/tasks/task_test", aigc.ContentKindImage},
		{"qwen-wan", "qwen/wan3.0-video", "https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis", "https://dashscope.aliyuncs.com/api/v1/tasks/task_test", aigc.ContentKindVideo},
		{"volcengine-seedance", "volcengine/doubao-seedance-2-5", "/contents/generations/tasks", "/contents/generations/tasks/task_test", aigc.ContentKindVideo},
		{"volcengine-seedream", "volcengine/doubao-seedream-5-0-pro", "/images/generations", "/contents/generations/tasks/task_test", aigc.ContentKindImage},
		{"chinadatapay-seedance", "cdp/doubao-seedance-2-5", relayTasksURL, relayTasksURL + "/task_test", aigc.ContentKindVideo},
		{"chinadatapay-minimax-h3", "cdp/MiniMax-H3", "https://tp-api.chinadatapay.com:8000/v2/video_generation", "https://tp-api.chinadatapay.com:8000/v2/query/video_generation/task_test", aigc.ContentKindVideo},
		{"chinadatapay-wan", "cdp/wan3.0-video", "https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis", "https://tp-api.chinadatapay.com:8000/api/v1/tasks/task_test", aigc.ContentKindVideo},
		{"lk888-minimax-h3", "lk/minimax-h3", "/media/generate", "/media/status?task_id=task_test", aigc.ContentKindVideo},
		{"zeroapi-video-v2", "ze/sd2.5-antiao", "/videos", "/videos/task_test", aigc.ContentKindVideo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"prompt":"test"}`)
			if tc.kind == aigc.ContentKindVideo {
				body = []byte(`{"prompt":"test","seconds":6,"size":"1280x720"}`)
			}
			gen := aigc.ContentGeneration{Kind: tc.kind, Model: tc.model, Input: body}
			req, err := driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: gen})
			if err != nil || req.URL != tc.submit {
				t.Fatalf("submit: %+v %v", req, err)
			}
			provider := req.Metadata["provider"].(string)
			gen.ProviderTaskID = "task_test"
			gen.Metadata = map[string]any{"aigc_adapter": provider}
			req, err = driver.PreparePoll(context.Background(), aigc.GenerationPollInput{Generation: gen})
			if err != nil || req.URL != tc.poll {
				t.Fatalf("poll: %+v %v", req, err)
			}
		})
	}
}

func TestQwenRequestFlagsAreCallerControlled(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, tc := range []struct {
		name, model string
		kind        aigc.ContentKind
		body        string
		paths       []string
	}{
		{"image omitted", "qwen/qwen-image-plus", aigc.ContentKindImage, `{"prompt":"test"}`, []string{"parameters.prompt_extend", "parameters.enable_thinking"}},
		{"image caller flags", "qwen/qwen-image-plus", aigc.ContentKindImage, `{"prompt":"test","extra_body":{"prompt_extend":false,"enable_thinking":true}}`, nil},
		{"wan omitted", "qwen/wan3.0-video", aigc.ContentKindVideo, `{"prompt":"test"}`, []string{"parameters.prompt_extend"}},
		{"wan caller flag", "qwen/wan3.0-video", aigc.ContentKindVideo, `{"prompt":"test","extra_body":{"prompt_extend":false}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: tc.kind, Model: tc.model, Input: []byte(tc.body)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.paths) > 0 {
				for _, path := range tc.paths {
					if ojson.GetBytes(req.Body, path).Exists() {
						t.Errorf("%s should be omitted when caller leaves it unspecified", path)
					}
				}
				return
			}
			for path, want := range map[string]bool{"parameters.prompt_extend": false, "parameters.enable_thinking": true} {
				if tc.kind == aigc.ContentKindVideo && path == "parameters.enable_thinking" {
					continue
				}
				node := ojson.GetBytes(req.Body, path)
				if !node.Exists() || node.Bool() != want {
					t.Errorf("%s = %v, want %v", path, node.Raw, want)
				}
			}
		})
	}
}

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

func TestBananaRouterIntegration(t *testing.T) {
	driverInstance := aigcdriver.NewDriver()
	ctx := context.Background()

	models := []string{
		"bter3/gpt-image-2.6-sunburst",
		"bter/gpt-image-2.6",
		"bananarouter/gpt-image-2.6",
	}

	for _, m := range models {
		support, err := driverInstance.Supports(ctx, aigc.GenerationSupportRequest{
			Kind:  aigc.ContentKindImage,
			Model: m,
		})
		if err != nil || support.Provider != "bananarouter" {
			t.Fatalf("model %s routing = %v (err=%v), want bananarouter", m, support.Provider, err)
		}
	}
}

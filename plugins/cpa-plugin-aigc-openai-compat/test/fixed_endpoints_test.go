package test

import (
	"context"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

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

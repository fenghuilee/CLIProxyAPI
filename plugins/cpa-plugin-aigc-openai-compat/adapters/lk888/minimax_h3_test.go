package lk888_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/lk888"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

const lk888H3Provider = "lk888-minimax-h3"

func lk888Submit(driver *aigcdriver.Driver, body string) (aigc.GenerationExecutionRequest, error) {
	return driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:  aigc.ContentKindVideo,
			Model: "lk/minimax-h3",
			Input: []byte(body),
		},
	})
}

func TestLK888MiniMaxH3RoutingAndSubmit(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()

	// 1. Check support for canonical lk/ prefix (case-insensitive)
	for _, m := range []string{"lk/minimax-h3", "lk/MiniMax-H3", "LK/MiniMax-H3"} {
		res, err := driver.Supports(ctx, aigc.GenerationSupportRequest{Kind: aigc.ContentKindVideo, Model: m})
		if err != nil || !res.Supported || res.Provider != lk888H3Provider {
			t.Fatalf("Supports(%q) = %+v, %v; want supported by %s", m, res, err, lk888H3Provider)
		}
	}

	// Non-video should not be routed to lk888-minimax-h3
	res, err := driver.Supports(ctx, aigc.GenerationSupportRequest{Kind: aigc.ContentKindImage, Model: "lk/minimax-h3"})
	if err != nil || res.Provider == lk888H3Provider {
		t.Fatalf("image request should not route to %s, got %+v, %v", lk888H3Provider, res, err)
	}

	// 2. Submit standard text-to-video request
	req, err := lk888Submit(driver, `{"prompt":"A peaceful lake with birds","seconds":5,"size":"1280x720","extra_body":{"notify_url":"https://example.com/webhook"}}`)
	if err != nil {
		t.Fatalf("lk888Submit error: %v", err)
	}

	if req.Method != http.MethodPost || req.URL != "/media/generate" {
		t.Fatalf("wrong request endpoint: %s %s", req.Method, req.URL)
	}
	if req.Metadata["provider"] != lk888H3Provider || req.Metadata["poll_backoff"] != true {
		t.Fatalf("wrong metadata: %+v", req.Metadata)
	}

	bodyStr := string(req.Body)
	if ojson.Get(bodyStr, "model").String() != "minimax-h3" {
		t.Fatalf("wrong model: %s", ojson.Get(bodyStr, "model").String())
	}
	if ojson.Get(bodyStr, "prompt").String() != "A peaceful lake with birds" {
		t.Fatalf("wrong prompt: %s", ojson.Get(bodyStr, "prompt").String())
	}
	if ojson.Get(bodyStr, "params.duration").String() != "5" {
		t.Fatalf("wrong duration: %s", ojson.Get(bodyStr, "params.duration").String())
	}
	if ojson.Get(bodyStr, "params.resolution").String() != "768P" {
		t.Fatalf("wrong resolution: %s", ojson.Get(bodyStr, "params.resolution").String())
	}
	if ojson.Get(bodyStr, "params.aspect_ratio").String() != "16:9" {
		t.Fatalf("wrong aspect_ratio: %s", ojson.Get(bodyStr, "params.aspect_ratio").String())
	}
	if ojson.Get(bodyStr, "notify_url").Exists() {
		t.Fatalf("user-provided notify_url must be ignored, got: %s", ojson.Get(bodyStr, "notify_url").String())
	}
}

func TestLK888MiniMaxH3Dimensions(t *testing.T) {
	driver := aigcdriver.NewDriver()

	tests := []struct {
		name        string
		body        string
		resolution  string
		aspectRatio string
		errContains string
	}{
		{"HD 768P landscape", `{"size":"1280x720"}`, "768P", "16:9", ""},
		{"768P portrait", `{"size":"768x1366"}`, "768P", "9:16", ""},
		{"768P square", `{"size":"1024x1024"}`, "768P", "1:1", ""},
		{"FHD 1080P", `{"size":"1920x1080"}`, "1080P", "16:9", ""},
		{"FHD 1080P portrait", `{"size":"1080x1920"}`, "1080P", "9:16", ""},
		{"2K landscape", `{"size":"2560x1440"}`, "2K", "16:9", ""},
		{"2K square", `{"size":"2048x2048"}`, "2K", "1:1", ""},
		{"2K ultrawide", `{"size":"3360x1440"}`, "2K", "21:9", ""},
		{"4K UHD", `{"size":"3840x2160"}`, "4K", "16:9", ""},
		{"explicit resolution 4K", `{"size":"1280x720","extra_body":{"resolution":"4K"}}`, "4K", "16:9", ""},
		{"explicit aspect_ratio adaptive", `{"size":"1280x720","extra_body":{"aspect_ratio":"adaptive"}}`, "768P", "adaptive", ""},
		{"size exceeds 4K", `{"size":"5000x5000"}`, "", "", "exceeds supported 4K pixel tier"},
		{"invalid resolution", `{"size":"1280x720","extra_body":{"resolution":"8K"}}`, "", "", "resolution must be"},
		{"invalid aspect_ratio", `{"size":"1280x720","extra_body":{"aspect_ratio":"5:3"}}`, "", "", "invalid aspect_ratio"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var bodyMap map[string]any
			if err := json.Unmarshal([]byte(tc.body), &bodyMap); err != nil {
				t.Fatal(err)
			}
			bodyMap["prompt"] = "test prompt"
			bodyMap["seconds"] = 6
			raw, _ := json.Marshal(bodyMap)

			req, err := lk888Submit(driver, string(raw))
			if tc.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			bodyStr := string(req.Body)
			if tc.resolution != "" && ojson.Get(bodyStr, "params.resolution").String() != tc.resolution {
				t.Fatalf("expected resolution %q, got %q", tc.resolution, ojson.Get(bodyStr, "params.resolution").String())
			}
			if tc.aspectRatio != "" && ojson.Get(bodyStr, "params.aspect_ratio").String() != tc.aspectRatio {
				t.Fatalf("expected aspect_ratio %q, got %q", tc.aspectRatio, ojson.Get(bodyStr, "params.aspect_ratio").String())
			}
		})
	}
}

func TestLK888MiniMaxH3MediaModes(t *testing.T) {
	driver := aigcdriver.NewDriver()

	// 1. First/last frame mode via extra_body
	req, err := lk888Submit(driver, `{
		"prompt": "Morphing landscape",
		"seconds": 5,
		"size": "1280x720",
		"extra_body": {
			"first_frame": "https://example.com/first.jpg",
			"last_frame": "https://example.com/last.jpg"
		}
	}`)
	if err != nil {
		t.Fatalf("first/last frame submit failed: %v", err)
	}
	bodyStr := string(req.Body)
	if ojson.Get(bodyStr, "params.mode").String() != "shouweizhen" {
		t.Fatalf("expected mode shouweizhen, got %s", ojson.Get(bodyStr, "params.mode").String())
	}
	if ojson.Get(bodyStr, "params.images.0").String() != "https://example.com/first.jpg" || ojson.Get(bodyStr, "params.images.1").String() != "https://example.com/last.jpg" {
		t.Fatalf("wrong images: %s", ojson.Get(bodyStr, "params.images").Raw)
	}

	// 2. last_frame without first_frame should fail
	_, err = lk888Submit(driver, `{
		"prompt": "Morphing landscape",
		"seconds": 5,
		"size": "1280x720",
		"extra_body": {
			"last_frame": "https://example.com/last.jpg"
		}
	}`)
	if err == nil || !strings.Contains(err.Error(), "requires first_frame") {
		t.Fatalf("expected requires first_frame error, got %v", err)
	}

	// 3. Reference generation mode via input_reference
	req, err = lk888Submit(driver, `{
		"prompt": "Style reference animation",
		"seconds": 6,
		"size": "1920x1080",
		"input_reference": [
			{"type": "image_url", "role": "reference_image", "image_url": {"url": "https://example.com/ref1.jpg"}},
			{"type": "image_url", "role": "reference_image", "image_url": {"url": "https://example.com/ref2.jpg"}},
			{"type": "video_url", "role": "reference_video", "video_url": {"url": "https://example.com/ref.mp4"}},
			{"type": "audio_url", "role": "reference_audio", "audio_url": {"url": "https://example.com/ref.mp3"}}
		]
	}`)
	if err != nil {
		t.Fatalf("reference media submit failed: %v", err)
	}
	bodyStr = string(req.Body)
	if ojson.Get(bodyStr, "params.mode").String() != "cankaosheng" {
		t.Fatalf("expected mode cankaosheng, got %s", ojson.Get(bodyStr, "params.mode").String())
	}
	if len(ojson.Get(bodyStr, "params.image_url").Array()) != 2 {
		t.Fatalf("expected 2 image_url items, got %d", len(ojson.Get(bodyStr, "params.image_url").Array()))
	}
	if len(ojson.Get(bodyStr, "params.video_url").Array()) != 1 {
		t.Fatalf("expected 1 video_url item, got %d", len(ojson.Get(bodyStr, "params.video_url").Array()))
	}
	if len(ojson.Get(bodyStr, "params.audio_url").Array()) != 1 {
		t.Fatalf("expected 1 audio_url item, got %d", len(ojson.Get(bodyStr, "params.audio_url").Array()))
	}

	// 4. Mixing frames and reference media should fail
	_, err = lk888Submit(driver, `{
		"prompt": "Mixed conflict",
		"seconds": 5,
		"size": "1280x720",
		"extra_body": {
			"first_frame": "https://example.com/first.jpg"
		},
		"input_reference": [
			{"type": "video_url", "role": "reference_video", "video_url": {"url": "https://example.com/ref.mp4"}}
		]
	}`)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestLK888MiniMaxH3Lifecycle(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()

	// 1. ParseSubmit success with integer task_id
	submitResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"code":200,"data":{"task_id":987654},"msg":"任务创建成功"}`),
		Metadata:   map[string]any{"provider": lk888H3Provider},
	}
	submitRes, err := driver.ParseSubmit(ctx, submitResp)
	if err != nil {
		t.Fatalf("ParseSubmit error: %v", err)
	}
	if submitRes.ProviderTaskID != "987654" || submitRes.Status != aigc.StatusRunning {
		t.Fatalf("wrong submit result: %+v", submitRes)
	}

	// 2. ParseSubmit success with string task_id
	submitResp2 := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"code":200,"data":{"task_id":"str_task_123"},"msg":"ok"}`),
		Metadata:   map[string]any{"provider": lk888H3Provider},
	}
	submitRes2, err := driver.ParseSubmit(ctx, submitResp2)
	if err != nil || submitRes2.ProviderTaskID != "str_task_123" {
		t.Fatalf("ParseSubmit string ID error: %v, %+v", err, submitRes2)
	}

	// 3. PreparePoll
	pollReq, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{
		Generation: aigc.ContentGeneration{
			ID:             "gen-1",
			Kind:           aigc.ContentKindVideo,
			Model:          "lk888/minimax-h3",
			ProviderTaskID: "987654",
			Provider:       lk888H3Provider,
		},
	})
	if err != nil {
		t.Fatalf("PreparePoll error: %v", err)
	}
	if pollReq.Method != http.MethodGet || pollReq.URL != "/media/status?task_id=987654" {
		t.Fatalf("wrong poll URL: %s", pollReq.URL)
	}

	// 4. ParsePoll pending
	pollRespPending := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"task_id":987654,"state":"pending","status":"排队中","is_final":false,"progress":"0%"}`),
		Metadata:   map[string]any{"provider": lk888H3Provider, "task_id": "987654"},
	}
	pollResPending, err := driver.ParsePoll(ctx, pollRespPending)
	if err != nil || pollResPending.Status != aigc.StatusRunning || pollResPending.Progress != 10 {
		t.Fatalf("poll pending error: %+v, %v", pollResPending, err)
	}

	// 5. ParsePoll running
	pollRespRunning := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"task_id":987654,"state":"running","status":"处理中","is_final":false,"progress":"65%"}`),
		Metadata:   map[string]any{"provider": lk888H3Provider, "task_id": "987654"},
	}
	pollResRunning, err := driver.ParsePoll(ctx, pollRespRunning)
	if err != nil || pollResRunning.Status != aigc.StatusRunning || pollResRunning.Progress != 65 {
		t.Fatalf("poll running error: %+v, %v", pollResRunning, err)
	}

	// 6. ParsePoll success
	pollRespSuccess := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"task_id":987654,"state":"success","status":"已完成","is_final":true,"progress":"100%","result_url":"https://cdn.example.com/video.mp4","result_type":"video","cost":0.25}`),
		Metadata:   map[string]any{"provider": lk888H3Provider, "task_id": "987654"},
	}
	pollResSuccess, err := driver.ParsePoll(ctx, pollRespSuccess)
	if err != nil || pollResSuccess.Status != aigc.StatusSucceeded {
		t.Fatalf("poll success error: %+v, %v", pollResSuccess, err)
	}
	if len(pollResSuccess.Artifacts) != 1 || pollResSuccess.Artifacts[0].URI != "https://cdn.example.com/video.mp4" || pollResSuccess.Artifacts[0].StorageProvider != "lk" {
		t.Fatalf("wrong artifact: %+v", pollResSuccess.Artifacts)
	}

	// 7. ParsePoll failed
	pollRespFailed := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"task_id":987654,"state":"failed","status":"失败","is_final":true,"error":"敏感内容拦截"}`),
		Metadata:   map[string]any{"provider": lk888H3Provider, "task_id": "987654"},
	}
	pollResFailed, err := driver.ParsePoll(ctx, pollRespFailed)
	if err != nil || pollResFailed.Status != aigc.StatusFailed || pollResFailed.ErrorMessage != "敏感内容拦截" {
		t.Fatalf("poll failed error: %+v, %v", pollResFailed, err)
	}

	// 8. PrepareCancel should return unsupported error
	_, err = driver.PrepareCancel(ctx, aigc.GenerationCancelInput{
		Generation: aigc.ContentGeneration{
			ID:             "gen-1",
			Kind:           aigc.ContentKindVideo,
			Model:          "lk888/minimax-h3",
			ProviderTaskID: "987654",
			Provider:       lk888H3Provider,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected cancellation not supported, got %v", err)
	}
}

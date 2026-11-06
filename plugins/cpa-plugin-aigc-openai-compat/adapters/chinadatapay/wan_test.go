package chinadatapay_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/qwen"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func wanGatewaySubmit(driver *aigcdriver.Driver, body string) (aigc.GenerationExecutionRequest, error) {
	return driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "chinadatapay/wan3.0-video", Input: []byte(body)}})
}

func TestWanGatewayDefaultsNativePrecedenceAndExtensions(t *testing.T) {
	driver := aigcdriver.NewDriver()
	req, err := wanGatewaySubmit(driver, `{"prompt":"cat"}`)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL != "https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis" || req.Metadata["provider"] != "chinadatapay-wan" || req.Metadata["single_attempt"] != true || req.Metadata["poll_interval_seconds"] != 15 || req.Header.Get("Authorization") != "" || req.Header.Get("X-DashScope-Async") != "" {
		t.Fatalf("wrong channel: %+v", req)
	}
	if ojson.GetBytes(req.Body, "model").String() != "wan3.0-video" || ojson.GetBytes(req.Body, "parameters.resolution").String() != "1080P" || ojson.GetBytes(req.Body, "parameters.duration").Int() != 5 {
		t.Fatalf("defaults: %s", req.Body)
	}
	for _, field := range []string{"audio", "prompt_extend", "watermark", "ratio", "seed"} {
		if ojson.GetBytes(req.Body, "parameters."+field).Exists() {
			t.Fatalf("invented optional %s", field)
		}
	}
	req, err = wanGatewaySubmit(driver, `{"prompt":"ignored","seconds":9,"extra_body":{"input":{"prompt":"native","custom_input":1,"media":[{"type":"link","url":"https://example.com/link","custom_media":true}]},"parameters":{"resolution":"480P","duration":-1,"audio":false,"prompt_extend":false,"watermark":false,"seed":0,"custom_parameter":2},"duration":7,"custom_top":{"keep":true}}}`)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"input.prompt": "native", "parameters.resolution": "480P"} {
		if ojson.GetBytes(req.Body, path).String() != want {
			t.Fatalf("precedence lost: %s", req.Body)
		}
	}
	if ojson.GetBytes(req.Body, "parameters.duration").Int() != -1 || !ojson.GetBytes(req.Body, "parameters.seed").Exists() || ojson.GetBytes(req.Body, "parameters.seed").Int() != 0 || !ojson.GetBytes(req.Body, "custom_top.keep").Bool() || !ojson.GetBytes(req.Body, "input.media.0.custom_media").Bool() {
		t.Fatalf("extensions/zero lost: %s", req.Body)
	}
	for _, field := range []string{"audio", "prompt_extend", "watermark"} {
		node := ojson.GetBytes(req.Body, "parameters."+field)
		if !node.Exists() || node.Bool() {
			t.Fatalf("false lost: %s", req.Body)
		}
	}
}

func TestWanGatewayValidation(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, body := range []string{
		`{}`, `{"prompt":"cat","seconds":1}`, `{"prompt":"cat","seconds":31}`, `{"prompt":"cat","seconds":2.5}`, `{"prompt":"cat","seconds":"5"}`,
		`{"extra_body":{"input":[]}}`, `{"extra_body":{"input":{"media":"bad"}}}`, `{"prompt":"cat","extra_body":{"parameters":[]}}`,
		`{"prompt":"cat","extra_body":{"audio":"false"}}`, `{"prompt":"cat","extra_body":{"seed":-1}}`, `{"prompt":"cat","extra_body":{"resolution":null}}`,
		`{"prompt":"cat","input_reference":[{"type":"video_url"}]}`, `{"prompt":"cat","input_reference":[{"type":"unknown","url":"https://example.com/a"}]}`,
		`{"prompt":"cat","extra_body":{"media":[{"type":"unknown","url":"https://example.com/a"}]}}`,
		`{"prompt":"cat","extra_body":{"media":[{"type":"first_frame","url":"https://example.com/first"},{"type":"reference_image","url":"https://example.com/ref"}]}}`,
		`{"prompt":"cat","extra_body":{"media":[{"type":"first_frame","url":"https://example.com/first"},{"type":"first_frame","url":"https://example.com/second"}]}}`,
		`{"prompt":"cat","extra_body":{"media":[{"type":"reference_video","url":"asset://wrong"}]}}`,
	} {
		if _, err := wanGatewaySubmit(driver, body); err == nil {
			t.Fatalf("invalid body accepted: %s", body)
		}
	}
	for _, kind := range []string{"reference_image", "reference_video", "reference_audio"} {
		media := []map[string]string{}
		for i := 0; i < 11; i++ {
			media = append(media, map[string]string{"type": kind, "url": "https://example.com/ref"})
		}
		body, _ := json.Marshal(map[string]any{"extra_body": map[string]any{"input": map[string]any{"media": media}}})
		if _, err := wanGatewaySubmit(driver, string(body)); err == nil {
			t.Fatal("media limit not enforced")
		}
	}
	for _, seconds := range []int{2, 30, -1} {
		body, _ := json.Marshal(map[string]any{"prompt": "cat", "seconds": seconds})
		if _, err := wanGatewaySubmit(driver, string(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWanGatewayTaskLifecycle(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	meta := map[string]any{"provider": "chinadatapay-wan", "task_id": "task_gateway"}
	submit, err := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(`{"output":{"task_id":"task_gateway","task_status":"PENDING"}}`), Metadata: meta})
	if err != nil || submit.ProviderTaskID != "task_gateway" || submit.Metadata["provider"] != "chinadatapay-wan" {
		t.Fatalf("submit: %+v %v", submit, err)
	}
	gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "chinadatapay/wan3.0-video", ProviderTaskID: submit.ProviderTaskID, Metadata: map[string]any{"aigc_adapter": "chinadatapay-wan"}}
	req, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
	if err != nil || req.URL != "https://tp-api.chinadatapay.com:8000/api/v1/tasks/task_gateway" {
		t.Fatalf("poll: %+v %v", req, err)
	}
	for _, tc := range []struct {
		state string
		want  aigc.GenerationStatus
	}{{"PENDING", aigc.StatusRunning}, {"RUNNING", aigc.StatusRunning}, {"SUCCEEDED", aigc.StatusSucceeded}, {"FAILED", aigc.StatusFailed}, {"CANCELED", aigc.StatusCanceled}} {
		body := []byte(`{"output":{"task_id":"task_gateway","task_status":"` + tc.state + `","video_url":"https://example.com/out.mp4","code":"risk","message":"blocked"},"usage":{"duration":5.5,"output_video_duration":5.75,"input_video_duration":0.25}}`)
		res, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: body, Metadata: meta})
		if err != nil || res.Status != tc.want || res.Metadata["wan"] == nil {
			t.Fatalf("%s: %+v %v", tc.state, res, err)
		}
		if tc.state == "SUCCEEDED" && (len(res.Artifacts) != 1 || res.Artifacts[0].Metadata["duration"] != 5.75 || len(res.Artifacts[0].Metadata["usage"].(map[string]any)) != 3) {
			t.Fatalf("usage lost/truncated: %+v", res.Artifacts)
		}
		if tc.state == "FAILED" && (res.ErrorCode != "risk" || res.ErrorMessage != "blocked") {
			t.Fatalf("error lost: %+v", res)
		}
	}
	for _, body := range []string{`{"output":{"task_id":"task_gateway","task_status":"UNKNOWN"}}`, `{"output":{"task_id":"task_other","task_status":"RUNNING"}}`, `{"output":{"task_id":"task_gateway","task_status":"SUCCEEDED"}}`, `bad json`} {
		res, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(body), Metadata: meta})
		if err == nil || res.Status == aigc.StatusFailed {
			t.Fatalf("transient/invalid query became terminal: %+v %v", res, err)
		}
	}
	if _, err := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{Generation: gen}); err == nil {
		t.Fatal("unsupported cancel sent upstream")
	}
	gen.Model = "unrelated-model"
	if _, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen}); err != nil {
		t.Fatal("locked adapter lost task after model changed")
	}
	gen.Metadata = nil
	gen.Provider = "qwen-wan"
	gen.Model = "qwen/wan3.0-video"
	req, err = driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
	if err != nil || req.URL != "https://dashscope.aliyuncs.com/api/v1/tasks/task_gateway" {
		t.Fatal("direct route changed")
	}
}

func TestWanGatewayCanonicalDefaultsAndFixedOrigin(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "chinadatapay/wan3.0-video-prime", Input: []byte(`{"prompt":"cat"}`), Metadata: map[string]any{"canonical": map[string]any{"resolution": "720p", "duration": 9, "ratio": "16:9"}}}
	req, err := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{Generation: gen})
	if err != nil || ojson.GetBytes(req.Body, "model").String() != "wan3.0-video-prime" || ojson.GetBytes(req.Body, "parameters.resolution").String() != "1080P" || ojson.GetBytes(req.Body, "parameters.duration").Int() != 5 {
		t.Fatalf("canonical defaults leaked: %s %v", req.Body, err)
	}
	if req.URL != "https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis" {
		t.Fatalf("origin: %+v %v", req, err)
	}
}

func TestWanGatewaySignedMediaAndMixedFrameSources(t *testing.T) {
	driver := aigcdriver.NewDriver()
	req, err := wanGatewaySubmit(driver, `{"input_reference":"https://example.com/video.mp4?signature=test"}`)
	if err != nil || ojson.GetBytes(req.Body, "input.media.0.type").String() != "reference_video" {
		t.Fatalf("signed video misclassified: %s %v", req.Body, err)
	}
	if _, err := wanGatewaySubmit(driver, `{"input_reference":"https://example.com/ref.png","extra_body":{"first_frame":"https://example.com/first.png"}}`); err == nil {
		t.Fatal("mixed frame/reference sources silently dropped")
	}
}

func TestWanGatewayQueryFailuresAndUsageFallback(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	meta := map[string]any{"provider": "chinadatapay-wan", "task_id": "task_gateway"}
	for _, status := range []int{429, 500, 502, 503} {
		res, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: status, Body: []byte(`{"code":"transient"}`), Metadata: meta})
		if err == nil || res.Status == aigc.StatusFailed {
			t.Fatalf("HTTP %d became terminal", status)
		}
	}
	for _, status := range []int{400, 401, 404} {
		res, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: status, Body: []byte(`{"code":"InvalidParameter","message":"not found"}`), Metadata: meta})
		if err != nil || res.Status != aigc.StatusFailed {
			t.Fatalf("HTTP %d: %+v %v", status, res, err)
		}
	}
	res404, err404 := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 404, Body: []byte("not JSON"), Metadata: meta})
	if err404 != nil || res404.Status != aigc.StatusFailed {
		t.Fatal("non-JSON 404 retried")
	}
	raw := []byte(`{"output":{"task_id":"task_gateway","task_status":"SUCCEEDED","video_url":"https://example.com/video.mp4"},"usage":{"duration":5.25}}`)
	res, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: raw, Metadata: meta})
	if err != nil || res.Artifacts[0].Metadata["duration"] != 5.25 {
		t.Fatalf("duration fallback: %+v %v", res, err)
	}
	submit, err := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: raw, Metadata: meta})
	if err != nil || submit.Status != aigc.StatusSucceeded || submit.Artifacts[0].Metadata["duration"] != 5.25 {
		t.Fatalf("immediate completion: %+v %v", submit, err)
	}
	if _, err := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(`{"output":{"task_id":"real-upstream-id"}}`), Metadata: meta}); err == nil {
		t.Fatal("upstream task ID accepted")
	}
}

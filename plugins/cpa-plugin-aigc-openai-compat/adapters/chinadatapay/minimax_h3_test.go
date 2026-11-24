package chinadatapay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

const h3Provider = "chinadatapay-minimax-h3"

func h3Submit(driver *aigcdriver.Driver, body string) (aigc.GenerationExecutionRequest, error) {
	return driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "chinadatapay/MiniMax-H3", Input: []byte(body)}})
}

func TestMiniMaxH3GenerationAndRouting(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	support, err := driver.Supports(ctx, aigc.GenerationSupportRequest{Kind: aigc.ContentKindVideo, Model: "chinadatapay/MiniMax-H3"})
	if err != nil || support.Provider != h3Provider {
		t.Fatalf("routing: %+v %v", support, err)
	}
	req, err := h3Submit(driver, `{"prompt":"cat","seconds":6,"input_reference":[{"type":"video_url","video_url":{"url":"mm_file://video"},"role":"reference_video"},{"type":"audio_url","audio_url":{"url":"data:audio/wav;base64,AAAA"}}],"extra_body":{"resolution":"768P","aigc_watermark":false,"callback_url":"https://example.com/callback"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost || req.URL != "https://tp-api.chinadatapay.com:8000/v2/video_generation" || req.Metadata["provider"] != h3Provider || req.Metadata["cancel_requires_confirmation"] != true {
		t.Fatalf("request: %+v", req)
	}
	if ojson.GetBytes(req.Body, "model").String() != "MiniMax-H3" || ojson.GetBytes(req.Body, "resolution").String() != "768P" || ojson.GetBytes(req.Body, "duration").Int() != 6 {
		t.Fatalf("payload: %s", req.Body)
	}
	if !ojson.GetBytes(req.Body, "aigc_watermark").Exists() || ojson.GetBytes(req.Body, "aigc_watermark").Bool() || ojson.GetBytes(req.Body, "ratio").Exists() || req.Header.Get("Authorization") != "" {
		t.Fatal("optional fields or auth changed")
	}
	if ojson.GetBytes(req.Body, "content.1.video_url.url").String() != "mm_file://video" || ojson.GetBytes(req.Body, "content.2.role").String() != "reference_audio" {
		t.Fatal("media identity lost")
	}
	result, err := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(`{"task_id":"task_gateway"}`), Metadata: req.Metadata})
	if err != nil || result.ProviderTaskID != "task_gateway" || result.Status != aigc.StatusRunning {
		t.Fatalf("submit: %+v %v", result, err)
	}
	req, err = h3Submit(driver, `{"prompt":"cat","extra_body":{"duration":4,"resolution":"2K"}}`)
	if err != nil || ojson.GetBytes(req.Body, "aigc_watermark").Exists() {
		t.Fatalf("omitted watermark: %s %v", req.Body, err)
	}
}

func TestMiniMaxH3SizeTranslation(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, tc := range []struct {
		name, body, resolution, ratio, errorText string
	}{
		{"HD landscape", `{"size":"1280x720"}`, "768P", "16:9", ""},
		{"768 portrait", `{"size":"768x1366"}`, "768P", "9:16", ""},
		{"HD square", `{"size":"1024x1024"}`, "768P", "1:1", ""},
		{"HD ceiling", `{"size":"1400x1000"}`, "768P", "4:3", ""},
		{"above HD ceiling", `{"size":"1401x1000"}`, "2K", "4:3", ""},
		{"full HD", `{"size":"1920x1080"}`, "2K", "16:9", ""},
		{"2K landscape", `{"size":"2560x1440"}`, "2K", "16:9", ""},
		{"2K portrait", `{"size":"1440x2560"}`, "2K", "9:16", ""},
		{"2K square", `{"size":"2048x2048"}`, "2K", "1:1", ""},
		{"2K ultrawide", `{"size":"3360x1440"}`, "2K", "21:9", ""},
		{"QHD ceiling", `{"size":"2750x2000"}`, "2K", "4:3", ""},
		{"above QHD ceiling", `{"size":"2751x2000"}`, "", "", "exceeds"},
		{"star separator", `{"size":"1024*768"}`, "768P", "4:3", ""},
		{"uppercase separator", `{"size":"768X1024"}`, "768P", "3:4", ""},
		{"extra size", `{"extra_body":{"size":"2560x1440"}}`, "2K", "16:9", ""},
		{"top size priority", `{"size":"1280x720","extra_body":{"size":"2560x1440"}}`, "768P", "16:9", ""},
		{"native resolution priority", `{"size":"1280x720","extra_body":{"resolution":"2K"}}`, "2K", "16:9", ""},
		{"native ratio priority", `{"size":"1280x720","extra_body":{"ratio":"adaptive"}}`, "768P", "adaptive", ""},
		{"native invalid resolution", `{"size":"1280x720","extra_body":{"resolution":"720p"}}`, "", "", "resolution"},
		{"native invalid ratio", `{"size":"1280x720","extra_body":{"ratio":"2:1"}}`, "", "", "ratio"},
		{"invalid size", `{"size":"720p"}`, "", "", "size"},
		{"numeric size", `{"size":720}`, "", "", "size"},
		{"zero size", `{"size":"0x720"}`, "", "", "size"},
		{"negative size", `{"size":"-1280x720"}`, "", "", "size"},
		{"overflow size", `{"size":"9223372036854775807x9223372036854775807"}`, "", "", "exceeds"},
		{"4K unsupported", `{"size":"3840x2160"}`, "", "", "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
				t.Fatal(err)
			}
			body["prompt"], body["seconds"] = "cat", 6
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req, err := h3Submit(driver, string(raw))
			if tc.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorText) {
					t.Fatalf("expected %q error, got %v", tc.errorText, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ojson.GetBytes(req.Body, "resolution").String() != tc.resolution || ojson.GetBytes(req.Body, "ratio").String() != tc.ratio {
				t.Fatalf("wrong dimensions: %s", req.Body)
			}
			if ojson.GetBytes(req.Body, "size").Exists() || ojson.GetBytes(req.Body, "duration").Int() != 6 {
				t.Fatalf("wrong native payload: %s", req.Body)
			}
		})
	}
}

func TestMiniMaxH3RegenerationSize(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, size := range []string{"2560x1440", "1440x2560", "1280x720"} {
		req, err := h3Submit(driver, `{"size":"`+size+`","extra_body":{"task_type":"regeneration","source_task_id":"task_original"}}`)
		if size == "1280x720" {
			if err == nil || !strings.Contains(err.Error(), "regeneration requires 2K") {
				t.Fatalf("expected 2K validation: %v", err)
			}
			continue
		}
		if err != nil || req.URL != "https://tp-api.chinadatapay.com:8000/v2/video_regeneration" || ojson.GetBytes(req.Body, "resolution").String() != "2K" || ojson.GetBytes(req.Body, "duration").Exists() {
			t.Fatalf("size regeneration: %s %v", req.Body, err)
		}
	}
}

func TestMiniMaxH3Regeneration(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, body := range []string{
		`{"extra_body":{"task_type":"regeneration","resolution":"2K","source_task_id":"task_original"}}`,
		`{"extra_body":{"task_type":"regeneration","resolution":"2K","content":[{"type":"text","text":"improve"},{"type":"video_url","role":"base_video","video_url":{"url":"https://example.com/base.mp4"}}]}}`,
	} {
		req, err := h3Submit(driver, body)
		if err != nil || req.URL != "https://tp-api.chinadatapay.com:8000/v2/video_regeneration" || ojson.GetBytes(req.Body, "duration").Exists() {
			t.Fatalf("regeneration: %s %v", req.Body, err)
		}
	}
}

func TestMiniMaxH3Validation(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, tc := range []struct{ name, body string }{
		{"missing duration", `{"prompt":"cat","extra_body":{"resolution":"768P"}}`},
		{"fraction duration", `{"prompt":"cat","seconds":6.5,"extra_body":{"resolution":"768P"}}`},
		{"string duration", `{"prompt":"cat","seconds":"6","extra_body":{"resolution":"768P"}}`},
		{"small duration", `{"prompt":"cat","seconds":3,"extra_body":{"resolution":"768P"}}`},
		{"large duration", `{"prompt":"cat","seconds":16,"extra_body":{"resolution":"768P"}}`},
		{"missing resolution", `{"prompt":"cat","seconds":6}`},
		{"seedance resolution", `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"720p"}}`},
		{"resolution case", `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"768p"}}`},
		{"empty text", `{"prompt":"   ","seconds":6,"extra_body":{"resolution":"768P"}}`},
		{"boolean type", `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"768P","aigc_watermark":"false"}}`},
		{"null boolean", `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"768P","aigc_watermark":null}}`},
		{"invalid ratio", `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"768P","ratio":"2:1"}}`},
		{"mutual exclusion", `{"prompt":"cat","seconds":6,"input_reference":[{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.png"}},{"type":"video_url","video_url":{"url":"https://example.com/ref.mp4"}}],"extra_body":{"resolution":"768P"}}`},
		{"Ark asset", `{"prompt":"cat","seconds":6,"input_reference":"asset://wrong","extra_body":{"resolution":"768P"}}`},
		{"unknown native type", `{"seconds":6,"extra_body":{"resolution":"768P","content":[{"type":"text","text":"cat"},{"type":"draft_task","draft_task":{"id":"task_x"}}]}}`},
		{"base_video generation", `{"prompt":"cat","seconds":6,"input_reference":{"type":"video_url","role":"base_video","video_url":{"url":"https://example.com/base.mp4"}},"extra_body":{"resolution":"768P"}}`},
		{"both regeneration modes", `{"extra_body":{"task_type":"regeneration","resolution":"2K","source_task_id":"task_x","content":[]}}`},
		{"neither regeneration mode", `{"extra_body":{"task_type":"regeneration","resolution":"2K"}}`},
		{"regeneration resolution", `{"extra_body":{"task_type":"regeneration","resolution":"768P","source_task_id":"task_x"}}`},
		{"regeneration duration", `{"seconds":6,"extra_body":{"task_type":"regeneration","resolution":"2K","source_task_id":"task_x"}}`},
		{"text task", `{"prompt":"cat","seconds":6,"extra_body":{"task_type":"h3_context_ir","resolution":"768P"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h3Submit(driver, tc.body); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMiniMaxH3QueryAndCancel(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "chinadatapay/MiniMax-H3", ProviderTaskID: "task_gateway", Metadata: map[string]any{"aigc_adapter": h3Provider}}
	poll, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
	if err != nil || poll.Method != http.MethodGet || poll.URL != "https://tp-api.chinadatapay.com:8000/v2/query/video_generation/task_gateway" {
		t.Fatalf("poll: %+v %v", poll, err)
	}
	for _, tc := range []struct {
		state string
		want  aigc.GenerationStatus
	}{
		{"queued", aigc.StatusRunning}, {"running", aigc.StatusRunning}, {"succeeded", aigc.StatusSucceeded}, {"failed", aigc.StatusFailed}, {"cancelled", aigc.StatusCanceled},
	} {
		raw := []byte(`{"task":{"id":"task_gateway","status":"` + tc.state + `","modality":"video","content":{"url":"https://example.com/result.mp4"},"usage":{"output_seconds":6},"error":{"code":"1026","message":"content risk detected"}}}`)
		result, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: raw, Metadata: poll.Metadata})
		if err != nil || result.Status != tc.want || string(result.ProviderResponse) != string(raw) {
			t.Fatalf("%s: %+v %v", tc.state, result, err)
		}
		if tc.state == "failed" && (result.ErrorCode != "1026" || result.ErrorMessage != "content risk detected") {
			t.Fatalf("failed detail: %+v", result)
		}
		if tc.state == "succeeded" {
			if len(result.Artifacts) != 1 || result.Artifacts[0].URI != "https://example.com/result.mp4" {
				t.Fatalf("artifact: %+v", result.Artifacts)
			}
			usage := result.Artifacts[0].Metadata["usage"].(map[string]any)
			if len(usage) != 1 || usage["output_seconds"] != float64(6) {
				t.Fatal("usage was invented or lost")
			}
		}
	}
	cancel, err := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{Generation: gen})
	if err != nil || cancel.Method != http.MethodDelete || cancel.URL != "https://tp-api.chinadatapay.com:8000/v2/video_generation/task_gateway" {
		t.Fatalf("cancel: %+v %v", cancel, err)
	}
	for _, tc := range []struct {
		status int
		body   string
		valid  bool
	}{
		{200, `{"task_id":"task_gateway","action":"cancelled","status":"cancelled"}`, true},
		{200, `{"task_id":"task_other","action":"cancelled","status":"cancelled"}`, false},
		{200, `{"task_id":"task_gateway","action":"deleted","status":"deleted"}`, false},
		{200, `{}`, false},
		{400, `{"error":{"message":"cannot cancel running task"}}`, false},
	} {
		err := driver.ParseCancel(ctx, aigc.GenerationExecutionResponse{StatusCode: tc.status, Body: []byte(tc.body), Metadata: cancel.Metadata})
		if (err == nil) != tc.valid {
			t.Fatalf("cancel response %s: %v", tc.body, err)
		}
	}
}

func TestMiniMaxH3QueryErrorsAndLockedRouting(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	meta := map[string]any{"provider": h3Provider, "task_id": "task_gateway"}
	for _, status := range []int{400, 401, 404} {
		result, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: status, Body: []byte(`{"error_code":"invalid_request","message":"unavailable"}`), Metadata: meta})
		if err != nil || result.Status != aigc.StatusFailed {
			t.Fatalf("HTTP %d: %+v %v", status, result, err)
		}
	}
	for _, status := range []int{429, 500, 502, 503} {
		if _, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: status, Metadata: meta}); err == nil {
			t.Fatal("transient error became terminal")
		}
	}
	for _, body := range []string{`{"task":{"id":"task_other","status":"running"}}`, `{"task":{"id":"task_gateway","status":"succeeded"}}`, `{"task":{"id":"task_gateway","status":"running","modality":"text"}}`, `{"task":{"id":"task_gateway","status":"unknown"}}`} {
		if _, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(body), Metadata: meta}); err == nil {
			t.Fatalf("invalid task accepted: %s", body)
		}
	}
	req, err := h3Submit(driver, `{"prompt":"cat","seconds":6,"extra_body":{"resolution":"768P"}}`)
	if err != nil || req.URL != "https://tp-api.chinadatapay.com:8000/v2/video_generation" {
		t.Fatalf("fixed endpoint: %+v %v", req, err)
	}
	locked := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "unrelated-model", ProviderTaskID: "task_gateway", Metadata: map[string]any{"aigc_adapter": h3Provider}}
	if req, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: locked}); err != nil || req.URL != "https://tp-api.chinadatapay.com:8000/v2/query/video_generation/task_gateway" {
		t.Fatalf("locked adapter poll: %+v %v", req, err)
	}
	if _, err := json.Marshal(req); err != nil {
		t.Fatal(err)
	}
}

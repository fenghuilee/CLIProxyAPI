package chinadatapay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/volcengine"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

const relayTasksURL = chinadatapay.ChinaDataPaySeedanceTasksURL

func TestChinaDataPaySeedanceRoutingAndParameters(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()
	for _, tc := range []struct{ model, provider string }{
		{"chinadatapay/doubao-seedance-2-5", "chinadatapay-seedance"},
		{"CHINADATAPAY/doubao-seedance-2-5", "chinadatapay-seedance"},
		{"volcengine/doubao-seedance-2-5", "volcengine-seedance"},
		{"doubao-seedance-2-5", "openai-compat"},
		{"chinadatapay/other-video", "openai-compat"},
	} {
		res, err := driver.Supports(ctx, aigc.GenerationSupportRequest{Kind: aigc.ContentKindVideo, Model: tc.model})
		if err != nil || !res.Supported || res.Provider != tc.provider {
			t.Fatalf("%s: %+v, %v", tc.model, res, err)
		}
	}
	req, err := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{
		Kind:  aigc.ContentKindVideo,
		Model: "chinadatapay/doubao-seedance-2-5",
		Input: json.RawMessage(`{"prompt":"new video","input_reference":[
   {"type":"image_url","image_url":{"url":"asset://asset-image"},"role":"reference_image"},
   {"type":"video_url","video_url":{"url":"asset://asset-video"}},
   {"type":"audio_url","audio_url":{"url":"asset://asset-audio"}}
  ],"extra_body":{"duration":-1,"ratio":"adaptive","resolution":"720p","generate_audio":false,"camera_fixed":false,"watermark":false,"return_last_frame":false,"draft":false,"seed":0,"priority":0,"omni_reference_task_type":"edit","output_format":"mov","callback_url":"https://example.com/callback","execution_expires_after":3600,"safety_identifier":"hashed-user","tools":[{"type":"web_search"}]}}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL != relayTasksURL || req.Method != http.MethodPost || req.Metadata["provider"] != "chinadatapay-seedance" || req.Model != "chinadatapay/doubao-seedance-2-5" {
		t.Fatalf("wrong channel request: %+v", req)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("adapter must not own credentials")
	}
	if ojson.GetBytes(req.Body, "model").String() != "doubao-seedance-2-5" || ojson.GetBytes(req.Body, "duration").Int() != -1 {
		t.Fatalf("wrong model/duration: %s", req.Body)
	}
	for _, key := range []string{"generate_audio", "camera_fixed", "watermark", "return_last_frame", "draft"} {
		node := ojson.GetBytes(req.Body, key)
		if !node.Exists() || node.Bool() {
			t.Errorf("lost explicit false for %s", key)
		}
	}
	for _, key := range []string{"seed", "priority"} {
		node := ojson.GetBytes(req.Body, key)
		if !node.Exists() || node.Int() != 0 {
			t.Errorf("lost zero for %s", key)
		}
	}
	for key, want := range map[string]string{"ratio": "adaptive", "omni_reference_task_type": "edit", "output_format": "mov", "safety_identifier": "hashed-user"} {
		if got := ojson.GetBytes(req.Body, key).String(); got != want {
			t.Errorf("%s=%s, want %s", key, got, want)
		}
	}
	if ojson.GetBytes(req.Body, "callback_url").Exists() {
		t.Errorf("user-provided callback_url must be ignored, got: %s", ojson.GetBytes(req.Body, "callback_url").String())
	}
	for i, kind := range []string{"image", "video", "audio"} {
		item := ojson.GetBytes(req.Body, "content").Array()[i+1]
		if item.Get("type").String() != kind+"_url" || item.Get(kind+"_url.url").String() != "asset://asset-"+kind || item.Get("role").String() != "reference_"+kind {
			t.Errorf("lost reference: %s", item.Raw)
		}
	}
	submit, err := driver.ParseSubmit(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(`{"id":"task-relay"}`), Metadata: req.Metadata})
	if err != nil || submit.ProviderTaskID != "task-relay" || submit.Metadata["provider"] != "chinadatapay-seedance" {
		t.Fatalf("submit identity: %+v, %v", submit, err)
	}
}

func TestChinaDataPaySeedancePollAndProviderLock(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()
	gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "chinadatapay/doubao-seedance-2-5", Provider: "chinadatapay-seedance", ProviderTaskID: "task-relay"}
	if req, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen}); err != nil || req.URL != relayTasksURL+"/task-relay" {
		t.Fatalf("default query endpoint unavailable: %+v %v", req, err)
	}
	req, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
	if err != nil || req.Method != http.MethodGet || req.URL != relayTasksURL+"/task-relay" || req.Metadata["provider"] != "chinadatapay-seedance" {
		t.Fatalf("poll request: %+v, %v", req, err)
	}
	for _, tc := range []struct {
		state string
		want  aigc.GenerationStatus
	}{
		{"queued", aigc.StatusRunning}, {"running", aigc.StatusRunning}, {"succeeded", aigc.StatusSucceeded}, {"failed", aigc.StatusFailed}, {"expired", aigc.StatusFailed},
	} {
		raw := []byte(`{"status":"` + tc.state + `","content":{"video_url":"https://example.com/out.mp4","last_frame_url":"https://example.com/last.png"}}`)
		res, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: raw, Metadata: req.Metadata})
		if err != nil || res.Status != tc.want {
			t.Fatalf("%s: %+v, %v", tc.state, res, err)
		}
		if tc.state == "succeeded" && (len(res.Artifacts) != 2 || res.Artifacts[0].URI != "https://example.com/out.mp4") {
			t.Fatalf("artifacts: %+v", res.Artifacts)
		}
	}
	gen.Provider = "aigc-openai-compat"
	gen.Metadata = map[string]any{"aigc_adapter": "chinadatapay-seedance"}
	if _, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen}); err != nil {
		t.Fatalf("locked adapter lost existing task: %v", err)
	}
	if _, err := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{Generation: gen}); err == nil {
		t.Fatal("unconfirmed cancel API accepted")
	}
	gen.Provider = "volcengine-seedance"
	gen.Metadata = nil
	gen.Model = "volcengine/doubao-seedance-2-5"
	req, err = driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
	if err != nil || req.URL != "/contents/generations/tasks/task-relay" {
		t.Fatalf("legacy poll changed: %+v %v", req, err)
	}
}

func TestChinaDataPaySeedanceNativeContent(t *testing.T) {
	driver := aigcdriver.NewDriver()
	req, err := driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "cdp/doubao-seedance-2-5", Input: []byte(`{"extra_body":{"content":[{"type":"image_url","image_url":{"url":"asset://first"},"role":"first_frame"},{"type":"image_url","image_url":{"url":"asset://last"},"role":"last_frame"}],"ratio":"adaptive"}}`)}})
	if err != nil || req.URL != relayTasksURL || ojson.GetBytes(req.Body, "content.1.role").String() != "last_frame" {
		t.Fatalf("native content: %+v %v", req, err)
	}
	_, err = driver.PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "cdp/doubao-seedance-2-5", Input: []byte(`{"content":[]}`)}})
	if err == nil || !strings.Contains(err.Error(), "unrecognized parameter") {
		t.Fatal("OpenAI validation changed")
	}
}

func TestChinaDataPaySeedanceErrors(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	for _, tc := range []struct {
		status int
		body   string
	}{
		{401, `{"error":{"code":"Unauthorized","message":"invalid token"}}`},
		{429, `{"code":1,"message":"rate limited"}`},
		{200, `{"code":1,"message":"business failure","id":"not-success"}`},
		{200, `{"error":{"code":"InvalidParameter"},"id":"not-success"}`},
	} {
		resp := aigc.GenerationExecutionResponse{StatusCode: tc.status, Body: []byte(tc.body), Metadata: map[string]any{"provider": "chinadatapay-seedance"}}
		submit, err := driver.ParseSubmit(ctx, resp)
		if err != nil || submit.Status != aigc.StatusFailed {
			t.Fatalf("submit error: %+v %v", submit, err)
		}
		poll, err := driver.ParsePoll(ctx, resp)
		if err != nil || poll.Status != aigc.StatusFailed {
			t.Fatalf("poll error: %+v %v", poll, err)
		}
	}
}

func TestChinaDataPaySeedanceConcurrentRequests(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				support, err := driver.Supports(ctx, aigc.GenerationSupportRequest{Kind: aigc.ContentKindVideo, Model: "cdp/doubao-seedance-2-5"})
				if err != nil || support.Provider != "chinadatapay-seedance" {
					t.Errorf("support: %+v %v", support, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestChinaDataPaySeedanceFixedPollForExistingTask(t *testing.T) {
	for _, model := range []string{"cdp/doubao-seedance-2-0-mini", "chinadatapay/doubao-seedance-2-0-mini"} {
		t.Run(model, func(t *testing.T) {
			driver := aigcdriver.NewDriver()
			ctx := context.Background()
			submit, err := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: model, Input: []byte(`{"prompt":"test"}`)}})
			if err != nil || submit.Method != http.MethodPost || submit.URL != relayTasksURL {
				t.Fatalf("create request: %+v %v", submit, err)
			}
			gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: "unrelated-model", Provider: "aigc-openai-compat", ProviderTaskID: "task_existing", Metadata: map[string]any{"aigc_adapter": "chinadatapay-seedance"}}
			req, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
			if err != nil || req.Method != http.MethodGet || req.URL != relayTasksURL+"/task_existing" || req.Metadata["provider"] != "chinadatapay-seedance" || req.Metadata["task_id"] != "task_existing" || req.Model != gen.Model {
				t.Fatalf("poll request: %+v %v", req, err)
			}
			result, err := driver.ParsePoll(ctx, aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(`{"id":"task_existing","status":"succeeded","content":{"video_url":"https://example.com/result.mp4"},"usage":{"completion_tokens":42}}`), Metadata: req.Metadata})
			if err != nil || result.Status != aigc.StatusSucceeded || len(result.Artifacts) != 1 {
				t.Fatalf("completion parse: %+v %v", result, err)
			}
		})
	}
}

func TestSeedanceChannelsShareProtocolWithSeparateAddresses(t *testing.T) {
	driver := aigcdriver.NewDriver()
	ctx := context.Background()
	for _, tc := range []struct{ channel, url string }{{"volcengine", "/contents/generations/tasks"}, {"cdp", relayTasksURL}, {"chinadatapay", relayTasksURL}} {
		gen := aigc.ContentGeneration{Kind: aigc.ContentKindVideo, Model: tc.channel + "/doubao-seedance-2-0-mini", Input: []byte(`{"prompt":"test"}`)}
		submit, err := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{Generation: gen})
		if err != nil || submit.URL != tc.url {
			t.Fatalf("%s submit: %+v %v", tc.channel, submit, err)
		}
		gen.ProviderTaskID = "task_existing"
		poll, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: gen})
		if err != nil || poll.URL != tc.url+"/task_existing" {
			t.Fatalf("%s poll: %+v %v", tc.channel, poll, err)
		}
	}
}

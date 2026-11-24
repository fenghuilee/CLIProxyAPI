package zeroapi

import (
	"net/http"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestPrepareSubmit(t *testing.T) {
	gen := aigc.ContentGeneration{Model: "ze/sd2.5-antiao", Input: []byte(`{
		"prompt":"edit the product", "seconds":6,
		"input_reference":[
			{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/last.png"}},
			{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.png"}},
			{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/clip.mp4?token=test"}},
			{"type":"audio_url","role":"reference_audio","audio_url":{"url":"https://example.com/audio.mp3"}}
		],
		"extra_body":{"ratio":"9:16","images":["https://example.com/product.png"],"stream":true}
	}`)}
	req, err := (videoAdapter{}).PrepareSubmit(t.Context(), aigc.GenerationSubmitInput{Generation: gen})
	if err != nil || req.Method != http.MethodPost || req.URL != "/videos" || req.Model != gen.Model {
		t.Fatalf("submit = %+v, %v", req, err)
	}
	body := ojson.ParseBytes(req.Body)
	for path, want := range map[string]string{
		"model": "sd2.5-antiao", "prompt": "edit the product", "size": "720x1280",
		"images.0": "https://example.com/first.png", "images.1": "https://example.com/last.png",
		"images.2": "https://example.com/product.png", "reference_videos.0": "https://example.com/clip.mp4?token=test",
		"audio_urls.0": "https://example.com/audio.mp3",
	} {
		if got := body.Get(path).String(); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if body.Get("duration").Raw != `"30"` || body.Get("stream").Raw != "false" || body.Get("extra_body").Exists() {
		t.Fatalf("unexpected payload: %s", req.Body)
	}
	gen.Input = []byte(`{"prompt":"edit","input_reference":[{"type":"video_url","video_url":{"url":"asset://clip"}}]}`)
	if _, err := (videoAdapter{}).PrepareSubmit(t.Context(), aigc.GenerationSubmitInput{Generation: gen}); err == nil {
		t.Fatal("expected invalid reference error")
	}
}

func TestPrepareSubmitModelVariants(t *testing.T) {
	for _, tc := range []struct{ model, duration string }{
		{"sd2.5", `"12"`},
		{"sd2.5-antiao", `"30"`},
		{"SD2.5-Flash-720p", `"12"`},
	} {
		t.Run(tc.model, func(t *testing.T) {
			gen := aigc.ContentGeneration{Model: "ze/" + tc.model, Input: []byte(`{"prompt":"test","seconds":12}`)}
			req, err := (videoAdapter{}).PrepareSubmit(t.Context(), aigc.GenerationSubmitInput{Generation: gen})
			if err != nil {
				t.Fatal(err)
			}
			body := ojson.ParseBytes(req.Body)
			if req.Model != gen.Model || body.Get("model").String() != tc.model || body.Get("duration").Raw != tc.duration {
				t.Fatalf("unexpected request model %q or body %s", req.Model, req.Body)
			}
		})
	}
}

func TestDocumentedSD25Input(t *testing.T) {
	for _, tc := range []struct {
		seconds string
		valid   bool
	}{
		{`4`, true}, {`"30"`, true}, {`3`, false}, {`31`, false}, {`4.5`, false},
	} {
		t.Run(tc.seconds, func(t *testing.T) {
			gen := aigc.ContentGeneration{Model: "ze/sd2.5", Input: []byte(`{"prompt":"test","seconds":` + tc.seconds + `,"extra_body":{"reference_videos":"https://example.com/video.mp4","audio_urls":"https://example.com/audio.mp3"}}`)}
			req, err := (videoAdapter{}).PrepareSubmit(t.Context(), aigc.GenerationSubmitInput{Generation: gen})
			if !tc.valid {
				if err == nil {
					t.Fatal("expected duration validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body := ojson.ParseBytes(req.Body)
			if body.Get("reference_videos.0").String() != "https://example.com/video.mp4" || body.Get("audio_urls.0").String() != "https://example.com/audio.mp3" || body.Get("asset_id").Exists() {
				t.Fatalf("unexpected reference payload: %s", req.Body)
			}
		})
	}
}

func TestTaskLifecycle(t *testing.T) {
	a := videoAdapter{}
	resp := aigc.GenerationExecutionResponse{StatusCode: 200, Body: []byte(`{"id":"job/1","status":"queued"}`)}
	submitted, err := a.ParseSubmit(t.Context(), resp)
	if err != nil || submitted.Status != aigc.StatusRunning || submitted.ProviderTaskID != "job/1" {
		t.Fatalf("parse submit = %+v, %v", submitted, err)
	}
	req, err := a.PreparePoll(t.Context(), aigc.GenerationPollInput{Generation: aigc.ContentGeneration{ProviderTaskID: submitted.ProviderTaskID}})
	if err != nil || req.Method != http.MethodGet || req.URL != "/videos/job%2F1" {
		t.Fatalf("prepare poll = %+v, %v", req, err)
	}
	for _, tc := range []struct {
		body string
		want aigc.GenerationStatus
	}{
		{`{"status":"in_progress","progress":30}`, aigc.StatusRunning},
		{`{"status":"completed","result":{"data":[{"url":"https://example.com/out.mp4"}]}}`, aigc.StatusSucceeded},
		{`{"status":"completed","data":[{"url":"https://example.com/out.mp4"}]}`, aigc.StatusSucceeded},
		{`{"status":"success","metadata":{"url":"https://example.com/out.mp4"}}`, aigc.StatusSucceeded},
		{`{"status":"failed","error":{"message":"generation rejected"}}`, aigc.StatusFailed},
		{`{"status":"failure"}`, aigc.StatusFailed},
		{`{"status":"cancelled"}`, aigc.StatusFailed},
		{`{"status":"completed"}`, ""},
	} {
		resp.Body = []byte(tc.body)
		result, err := a.ParsePoll(t.Context(), resp)
		if tc.want == "" {
			if err == nil {
				t.Errorf("expected missing video error for %s", tc.body)
			}
			continue
		}
		if err != nil || result.Status != tc.want {
			t.Errorf("poll %s = %+v, %v; want %s", tc.body, result, err, tc.want)
		}
		if tc.want == aigc.StatusSucceeded && (len(result.Artifacts) != 1 || result.Artifacts[0].URI != "https://example.com/out.mp4" || result.Artifacts[0].StorageProvider != "zeroapi") {
			t.Errorf("unexpected output artifact: %+v", result.Artifacts)
		}
	}
	if _, err := a.PrepareCancel(t.Context(), aigc.GenerationCancelInput{}); err == nil {
		t.Fatal("ZeroAPI cancellation should be unsupported")
	}
}

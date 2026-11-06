package chinadatapay_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
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

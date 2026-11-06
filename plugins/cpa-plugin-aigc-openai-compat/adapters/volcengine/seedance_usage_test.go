package volcengine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/volcengine"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestSeedanceRecordsCompleteUsageForBothChannels(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, provider := range []string{"volcengine-seedance", "chinadatapay-seedance"} {
		for _, state := range []string{"succeeded", "running", "failed", "cancelled"} {
			t.Run(provider+"/"+state, func(t *testing.T) {
				body := []byte(`{"id":"task-test","status":"` + state + `","content":{"video_url":"https://example.com/video.mp4"},"usage":{"completion_tokens":123456,"total_tokens":123456,"tool_usage":{"web_search":0},"future_field":{"tokens":12,"enabled":false},"fractional_metric":1.25}}`)
				result, err := driver.ParsePoll(context.Background(), aigc.GenerationExecutionResponse{StatusCode: 200, Body: body, Metadata: map[string]any{"provider": provider}})
				if err != nil {
					t.Fatal(err)
				}
				metadata, err := json.Marshal(result.Metadata)
				if err != nil {
					t.Fatal(err)
				}
				recorded := ojson.GetBytes(metadata, "seedance.usage")
				original := ojson.GetBytes(body, "usage")
				actualJSON, _ := json.Marshal(recorded.Value())
				expectedJSON, _ := json.Marshal(original.Value())
				if string(actualJSON) != string(expectedJSON) {
					t.Fatalf("usage changed: %s, want %s", actualJSON, expectedJSON)
				}
				if state == "succeeded" && (result.Status != aigc.StatusSucceeded || len(result.Artifacts) != 1) {
					t.Fatalf("result behavior changed: %+v", result)
				}
			})
		}
	}
}

func TestSeedanceUsageAbsentPartialAndZero(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, provider := range []string{"volcengine-seedance", "chinadatapay-seedance"} {
		for _, tc := range []struct {
			name, usage string
			record      bool
		}{
			{"absent", "", false}, {"null", `,"usage":null`, false}, {"wrong type", `,"usage":"unknown"`, false},
			{"empty object", `,"usage":{}`, true}, {"zero", `,"usage":{"completion_tokens":0}`, true},
			{"partial", `,"usage":{"completion_tokens":42}`, true},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				body := []byte(`{"status":"succeeded","content":{"video_url":"https://example.com/video.mp4"}` + tc.usage + `}`)
				result, err := driver.ParsePoll(context.Background(), aigc.GenerationExecutionResponse{StatusCode: 200, Body: body, Metadata: map[string]any{"provider": provider}})
				if err != nil || result.Status != aigc.StatusSucceeded {
					t.Fatalf("legacy result rejected: %+v %v", result, err)
				}
				metadata, _ := json.Marshal(result.Metadata)
				usage := ojson.GetBytes(metadata, "seedance.usage")
				if usage.Exists() != tc.record {
					t.Fatalf("usage presence changed: %s", metadata)
				}
				if usage.Get("total_tokens").Exists() {
					t.Fatalf("missing total_tokens fabricated: %s", metadata)
				}
				if tc.name == "zero" && (!usage.Get("completion_tokens").Exists() || usage.Get("completion_tokens").Int() != 0) {
					t.Fatal("explicit zero lost")
				}
			})
		}
	}
}

func TestSeedanceFailureCanRetainReturnedUsage(t *testing.T) {
	driver := aigcdriver.NewDriver()
	for _, provider := range []string{"volcengine-seedance", "chinadatapay-seedance"} {
		result, err := driver.ParsePoll(context.Background(), aigc.GenerationExecutionResponse{StatusCode: 200, Metadata: map[string]any{"provider": provider}, Body: []byte(`{"status":"failed","error":{"code":"InvalidParameter","message":"task failed"},"usage":{"completion_tokens":0}}`)})
		if err != nil || result.Status != aigc.StatusFailed {
			t.Fatalf("failure handling changed: %+v %v", result, err)
		}
		raw, _ := json.Marshal(result.Metadata)
		if !ojson.GetBytes(raw, "seedance.usage.completion_tokens").Exists() {
			t.Fatalf("returned usage discarded: %s", raw)
		}
	}
}

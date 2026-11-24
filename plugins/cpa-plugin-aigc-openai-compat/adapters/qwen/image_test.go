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

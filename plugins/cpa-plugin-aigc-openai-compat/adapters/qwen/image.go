// Package qwen provides image and video generation adapters for Alibaba DashScope / Qwen.
package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// QwenImageAdapter adapts DashScope Qwen/Wanx multimodal image generation.
type QwenImageAdapter struct{}

// NewQwenImageAdapter creates a DashScope image adapter.
func NewQwenImageAdapter() *QwenImageAdapter {
	return &QwenImageAdapter{}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "qwen-image",
		New:  func() driver.Adapter { return NewQwenImageAdapter() },
	})
}

// Name returns the adapter identifier.
func (a *QwenImageAdapter) Name() string {
	return "qwen-image"
}

// Kind declares image content kind.
func (a *QwenImageAdapter) Kind() aigc.ContentKind {
	return aigc.ContentKindImage
}

// Prefixes returns the vendor/model prefixes for model routing.
func (a *QwenImageAdapter) Prefixes() []string {
	return []string{
		"alibaba-cn/qwen-image",
		"alibaba/qwen-image",
		"qwen/qwen-image",
		"alibaba-cn/wanx",
		"alibaba/wanx",
		"qwen/wanx",
	}
}

// PrepareSubmit transforms input into DashScope multimodal generation request.
func (a *QwenImageAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := driver.UpstreamModel(gen.Model)

	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required")
	}

	var contentNodes []map[string]any
	for _, img := range images {
		contentNodes = append(contentNodes, map[string]any{
			"image": img,
		})
	}
	if prompt != "" {
		contentNodes = append(contentNodes, map[string]any{
			"text": prompt,
		})
	}

	parameters := make(map[string]any)

	if size := driver.NormalizeSizeToStar(inputBytes); size != "" {
		parameters["size"] = size
	}

	if n := ojson.GetBytes(inputBytes, "n"); n.Exists() && n.Int() > 0 {
		parameters["n"] = n.Int()
	}

	if seed := ojson.GetBytes(inputBytes, "extra_body.seed"); seed.Exists() {
		parameters["seed"] = seed.Int()
	} else if seed := ojson.GetBytes(inputBytes, "seed"); seed.Exists() {
		parameters["seed"] = seed.Int()
	}

	if negPrompt := driver.ExtractNegativePrompt(inputBytes); negPrompt != "" {
		parameters["negative_prompt"] = negPrompt
	}

	if wm := ojson.GetBytes(inputBytes, "extra_body.watermark"); wm.Exists() {
		parameters["watermark"] = wm.Bool()
	} else if wm := ojson.GetBytes(inputBytes, "watermark"); wm.Exists() {
		parameters["watermark"] = wm.Bool()
	}

	if mask := ojson.GetBytes(inputBytes, "mask"); mask.Exists() {
		parameters["mask"] = mask.Value()
	}

	if pe := ojson.GetBytes(inputBytes, "extra_body.prompt_extend"); pe.Exists() {
		parameters["prompt_extend"] = pe.Bool()
	} else if pe := ojson.GetBytes(inputBytes, "prompt_extend"); pe.Exists() {
		parameters["prompt_extend"] = pe.Bool()
	}

	if pem := strings.TrimSpace(ojson.GetBytes(inputBytes, "extra_body.prompt_extend_mode").String()); pem != "" {
		parameters["prompt_extend_mode"] = pem
	} else if pem := strings.TrimSpace(ojson.GetBytes(inputBytes, "prompt_extend_mode").String()); pem != "" {
		parameters["prompt_extend_mode"] = pem
	}

	if et := ojson.GetBytes(inputBytes, "extra_body.enable_thinking"); et.Exists() {
		parameters["enable_thinking"] = et.Bool()
	} else if et := ojson.GetBytes(inputBytes, "enable_thinking"); et.Exists() {
		parameters["enable_thinking"] = et.Bool()
	}

	payload := map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": contentNodes,
				},
			},
		},
		"parameters": parameters,
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal qwen image payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation",
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "qwen-image"},
	}, nil
}

// ParseSubmit parses DashScope image submit response.
func (a *QwenImageAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedSubmitResultFromRaw("qwen-image", resp.StatusCode, resp.Body, nil), nil
	}

	if taskID := ojson.GetBytes(resp.Body, "output.task_id").String(); taskID != "" {
		taskStatus := ojson.GetBytes(resp.Body, "output.task_status").String()
		switch strings.ToUpper(taskStatus) {
		case "SUCCEEDED":
			artifacts := extractQwenArtifacts(resp.Body)
			return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "qwen-image", "task_id": taskID}), nil
		case "FAILED":
			return driver.BuildFailedSubmitResultFromRaw("qwen-image", 200, resp.Body, nil), nil
		default:
			return driver.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "qwen-image"}), nil
		}
	}

	artifacts := extractQwenArtifacts(resp.Body)
	if len(artifacts) > 0 {
		return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "qwen-image"}), nil
	}

	return aigc.GenerationSubmitResult{}, fmt.Errorf("not a recognized qwen-image response")
}

// PreparePoll prepares a task polling GET request.
func (a *QwenImageAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := strings.TrimSpace(input.Generation.ProviderTaskID)
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("task_id is required for polling")
	}

	taskURL := fmt.Sprintf("https://dashscope.aliyuncs.com/api/v1/tasks/%s", taskID)
	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      taskURL,
		Header:   header,
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": "qwen-image", "task_id": taskID},
	}, nil
}

// ParsePoll maps DashScope async status to unified result.
func (a *QwenImageAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedPollResultFromRaw("qwen-image", resp.StatusCode, resp.Body, nil), nil
	}

	if !ojson.GetBytes(resp.Body, "output").Exists() && !ojson.GetBytes(resp.Body, "request_id").Exists() {
		return aigc.GenerationPollResult{}, fmt.Errorf("not a dashscope poll response")
	}

	taskStatus := strings.ToUpper(ojson.GetBytes(resp.Body, "output.task_status").String())
	switch taskStatus {
	case "SUCCEEDED":
		artifacts := extractQwenArtifacts(resp.Body)
		if len(artifacts) == 0 {
			return aigc.GenerationPollResult{}, fmt.Errorf("not a qwen-image poll response")
		}
		return driver.BuildCompletedPollResult(artifacts), nil
	case "FAILED":
		return driver.BuildFailedPollResultFromRaw("qwen-image", 200, resp.Body, nil), nil
	case "CANCELED":
		return driver.BuildCanceledPollResult(), nil
	case "RUNNING":
		return driver.BuildRunningPollResult(50, aigc.StageProviderRunning), nil
	case "PENDING":
		return driver.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		return aigc.GenerationPollResult{}, fmt.Errorf("unrecognized dashscope task status: %s", taskStatus)
	}
}

// PrepareCancel builds cancellation POST request.
func (a *QwenImageAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := strings.TrimSpace(input.Generation.ProviderTaskID)
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("task_id is required for cancel")
	}

	cancelURL := fmt.Sprintf("https://dashscope.aliyuncs.com/api/v1/tasks/%s/cancel", taskID)
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    cancelURL,
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel acknowledges cancellation.
func (a *QwenImageAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func extractQwenArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	choices := ojson.GetBytes(body, "output.choices").Array()
	for _, choice := range choices {
		contentList := choice.Get("message.content").Array()
		for _, item := range contentList {
			if imgURL := item.Get("image").String(); imgURL != "" {
				artifacts = append(artifacts, driver.CreateArtifact(
					"image", "dashscope", imgURL, "image/png", 0, nil,
				))
			}
		}
	}

	results := ojson.GetBytes(body, "output.results").Array()
	for _, res := range results {
		if u := res.Get("url").String(); u != "" {
			artifacts = append(artifacts, driver.CreateArtifact(
				"image", "dashscope", u, "image/png", 0, nil,
			))
		}
	}

	return artifacts
}

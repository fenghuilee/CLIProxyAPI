package image

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-aigc-openai-compat/utils"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

// QwenImageAdapter handles Alibaba DashScope multimodal image generation.
type QwenImageAdapter struct {
	cfgFunc func() (models []string, endpoint string, promptExtend, enableThinking bool)
}

// NewQwenImageAdapter creates a new instance of QwenImageAdapter.
func NewQwenImageAdapter(cfgFunc func() (models []string, endpoint string, promptExtend, enableThinking bool)) *QwenImageAdapter {
	return &QwenImageAdapter{cfgFunc: cfgFunc}
}

// Name returns the provider name.
func (a *QwenImageAdapter) Name() string {
	return "qwen-image"
}

// Supports checks if this adapter supports the model and kind.
func (a *QwenImageAdapter) Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool {
	if req.Kind != aigc.ContentKindImage {
		return false
	}
	models, _, _, _ := a.cfgFunc()
	modelLower := strings.ToLower(strings.TrimSpace(req.Model))
	if modelLower == "" {
		return false
	}
	for _, pattern := range models {
		patternLower := strings.ToLower(strings.TrimSpace(pattern))
		if patternLower == "" {
			continue
		}
		if patternLower == "*" || patternLower == modelLower {
			return true
		}
		if matched, _ := strings.HasPrefix(patternLower, modelLower), false; matched {
			return true
		}
		// Glob pattern match
		if utils.MatchModel(patternLower, modelLower) {
			return true
		}
	}
	return false
}

// PrepareSubmit builds the DashScope multimodal generation request.
func (a *QwenImageAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input
	if len(gen.PreparedInput) > 0 {
		inputBytes = gen.PreparedInput
	}

	model := strings.TrimSpace(gen.Model)
	if strings.HasPrefix(model, "qwen/") {
		model = strings.TrimPrefix(model, "qwen/")
	}

	prompt := utils.ExtractPrompt(inputBytes)
	images := utils.ExtractImages(inputBytes)

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

	// Size normalization
	if size := utils.NormalizeSizeToStar(inputBytes); size != "" {
		parameters["size"] = size
	}

	// Output count n
	if n := gjson.GetBytes(inputBytes, "n"); n.Exists() && n.Int() > 0 {
		parameters["n"] = n.Int()
	}

	// Seed
	if seed := gjson.GetBytes(inputBytes, "extra_body.seed"); seed.Exists() {
		parameters["seed"] = seed.Int()
	} else if seed := gjson.GetBytes(inputBytes, "seed"); seed.Exists() {
		parameters["seed"] = seed.Int()
	}

	// Negative prompt
	if negPrompt := utils.ExtractNegativePrompt(inputBytes); negPrompt != "" {
		parameters["negative_prompt"] = negPrompt
	}

	// Watermark
	if wm := gjson.GetBytes(inputBytes, "extra_body.watermark"); wm.Exists() {
		parameters["watermark"] = wm.Bool()
	} else if wm := gjson.GetBytes(inputBytes, "watermark"); wm.Exists() {
		parameters["watermark"] = wm.Bool()
	}

	_, endpoint, defaultPE, defaultET := a.cfgFunc()

	// Prompt extend
	if pe := gjson.GetBytes(inputBytes, "extra_body.prompt_extend"); pe.Exists() {
		parameters["prompt_extend"] = pe.Bool()
	} else if pe := gjson.GetBytes(inputBytes, "prompt_extend"); pe.Exists() {
		parameters["prompt_extend"] = pe.Bool()
	} else {
		parameters["prompt_extend"] = defaultPE
	}

	// Prompt extend mode
	if pem := strings.TrimSpace(gjson.GetBytes(inputBytes, "extra_body.prompt_extend_mode").String()); pem != "" {
		parameters["prompt_extend_mode"] = pem
	} else if pem := strings.TrimSpace(gjson.GetBytes(inputBytes, "prompt_extend_mode").String()); pem != "" {
		parameters["prompt_extend_mode"] = pem
	}

	// Enable thinking
	if et := gjson.GetBytes(inputBytes, "extra_body.enable_thinking"); et.Exists() {
		parameters["enable_thinking"] = et.Bool()
	} else if et := gjson.GetBytes(inputBytes, "enable_thinking"); et.Exists() {
		parameters["enable_thinking"] = et.Bool()
	} else {
		parameters["enable_thinking"] = defaultET
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

	if endpoint == "" {
		endpoint = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      endpoint,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		AuthID:   gen.AuthID,
		Metadata: map[string]any{"provider": "qwen-image"},
	}, nil
}

// ParseSubmit processes the response from DashScope multimodal generation.
func (a *QwenImageAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedSubmitResultFromRaw("qwen-image", resp.StatusCode, resp.Body, nil), nil
	}

	// 1. Check if asynchronous response (task_id returned)
	if taskID := gjson.GetBytes(resp.Body, "output.task_id").String(); taskID != "" {
		taskStatus := gjson.GetBytes(resp.Body, "output.task_status").String()
		switch strings.ToUpper(taskStatus) {
		case "SUCCEEDED":
			artifacts := extractQwenArtifacts(resp.Body)
			return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "qwen-image", "task_id": taskID}), nil
		case "FAILED":
			return utils.BuildFailedSubmitResultFromRaw("qwen-image", 200, resp.Body, nil), nil
		default:
			return utils.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "qwen-image"}), nil
		}
	}

	// 2. Check synchronous response
	artifacts := extractQwenArtifacts(resp.Body)
	if len(artifacts) > 0 {
		return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "qwen-image"}), nil
	}

	return aigc.GenerationSubmitResult{}, fmt.Errorf("not a recognized qwen-image response")
}

// PreparePoll creates the HTTP polling request for DashScope task status.
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
		AuthID:   input.Generation.AuthID,
		Metadata: map[string]any{"provider": "qwen-image", "task_id": taskID},
	}, nil
}

// ParsePoll processes the status polling response from DashScope.
func (a *QwenImageAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedPollResultFromRaw("qwen-image", resp.StatusCode, resp.Body, nil), nil
	}

	if !gjson.GetBytes(resp.Body, "output").Exists() && !gjson.GetBytes(resp.Body, "request_id").Exists() {
		return aigc.GenerationPollResult{}, fmt.Errorf("not a dashscope poll response")
	}

	taskStatus := strings.ToUpper(gjson.GetBytes(resp.Body, "output.task_status").String())
	switch taskStatus {
	case "SUCCEEDED":
		artifacts := extractQwenArtifacts(resp.Body)
		return utils.BuildCompletedPollResult(artifacts), nil
	case "FAILED":
		return utils.BuildFailedPollResultFromRaw("qwen-image", 200, resp.Body, nil), nil
	case "CANCELED":
		return utils.BuildCanceledPollResult(), nil
	case "RUNNING":
		return utils.BuildRunningPollResult(50, aigc.StageProviderRunning), nil
	case "PENDING":
		return utils.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		return aigc.GenerationPollResult{}, fmt.Errorf("unrecognized dashscope task status: %s", taskStatus)
	}
}

// PrepareCancel creates the cancel request for DashScope.
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
		AuthID: input.Generation.AuthID,
	}, nil
}

// ParseCancel processes the cancel response.
func (a *QwenImageAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func extractQwenArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	// Check output.choices[].message.content[].image
	choices := gjson.GetBytes(body, "output.choices").Array()
	for _, choice := range choices {
		contentList := choice.Get("message.content").Array()
		for _, item := range contentList {
			if imgURL := item.Get("image").String(); imgURL != "" {
				artifacts = append(artifacts, utils.CreateArtifact(
					"image", "dashscope", imgURL, "image/png", 0, nil,
				))
			}
		}
	}

	// Check output.results[].url
	results := gjson.GetBytes(body, "output.results").Array()
	for _, res := range results {
		if u := res.Get("url").String(); u != "" {
			artifacts = append(artifacts, utils.CreateArtifact(
				"image", "dashscope", u, "image/png", 0, nil,
			))
		}
	}

	return artifacts
}

package common

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

// OpenAICompatAdapter handles standard OpenAI-compatible image and video generation APIs.
type OpenAICompatAdapter struct {
	cfgFunc func() (imageModels, videoModels []string, fallback bool, imgSubmitPath, imgEditPath, vidSubmitPath, vidPollTpl string)
}

// NewOpenAICompatAdapter creates a new OpenAICompatAdapter.
func NewOpenAICompatAdapter(cfgFunc func() (imageModels, videoModels []string, fallback bool, imgSubmitPath, imgEditPath, vidSubmitPath, vidPollTpl string)) *OpenAICompatAdapter {
	return &OpenAICompatAdapter{cfgFunc: cfgFunc}
}

// Name returns the provider name.
func (a *OpenAICompatAdapter) Name() string {
	return "openai-compat"
}

// Supports checks if this adapter supports the requested kind and model.
func (a *OpenAICompatAdapter) Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool {
	imgModels, vidModels, fallback, _, _, _, _ := a.cfgFunc()
	modelLower := strings.ToLower(strings.TrimSpace(req.Model))
	if modelLower == "" {
		return false
	}

	if req.Kind == aigc.ContentKindImage {
		if isExplicitVideoModel(modelLower) {
			return false
		}
		for _, pattern := range imgModels {
			if pattern == "*" || utils.MatchModel(pattern, modelLower) {
				return true
			}
		}
		return fallback
	}

	if req.Kind == aigc.ContentKindVideo {
		if isExplicitImageModel(modelLower) {
			return false
		}
		for _, pattern := range vidModels {
			if pattern == "*" || utils.MatchModel(pattern, modelLower) {
				return true
			}
		}
		return fallback
	}

	return false
}

func isExplicitVideoModel(model string) bool {
	return strings.Contains(model, "video") ||
		strings.Contains(model, "seedance") ||
		strings.Contains(model, "sora") ||
		strings.Contains(model, "kling") ||
		strings.Contains(model, "cogvideo") ||
		strings.Contains(model, "luma") ||
		strings.Contains(model, "runway")
}

func isExplicitImageModel(model string) bool {
	return strings.Contains(model, "image") ||
		strings.Contains(model, "seedream") ||
		strings.Contains(model, "dall-e") ||
		strings.Contains(model, "flux") ||
		strings.Contains(model, "recraft") ||
		strings.Contains(model, "midjourney") ||
		strings.Contains(model, "wanx")
}

// PrepareSubmit builds an OpenAI standard request for images or videos.
func (a *OpenAICompatAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input
	if len(gen.PreparedInput) > 0 {
		inputBytes = gen.PreparedInput
	}

	_, _, _, imgSubmitPath, imgEditPath, vidSubmitPath, _ := a.cfgFunc()
	if imgSubmitPath == "" {
		imgSubmitPath = "/images/generations"
	}
	if imgEditPath == "" {
		imgEditPath = "/images/edits"
	}
	if vidSubmitPath == "" {
		vidSubmitPath = "/videos/generations"
	}

	if gen.Kind == aigc.ContentKindVideo {
		return a.prepareVideoSubmit(gen, inputBytes, vidSubmitPath)
	}

	return a.prepareImageSubmit(gen, inputBytes, imgSubmitPath, imgEditPath)
}

func (a *OpenAICompatAdapter) prepareImageSubmit(gen aigc.ContentGeneration, inputBytes []byte, submitPath, editPath string) (aigc.GenerationExecutionRequest, error) {
	prompt := utils.ExtractPrompt(inputBytes)
	images := utils.ExtractImages(inputBytes)
	hasMask := gjson.GetBytes(inputBytes, "mask").Exists()

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required for image generation")
	}

	payload := make(map[string]any)
	payload["model"] = gen.Model
	if prompt != "" {
		payload["prompt"] = prompt
	}

	if len(images) > 0 {
		if len(images) == 1 {
			payload["image"] = images[0]
		} else {
			payload["images"] = images
		}
	}
	if hasMask {
		payload["mask"] = gjson.GetBytes(inputBytes, "mask").Value()
	}

	if size := utils.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}
	if n := gjson.GetBytes(inputBytes, "n").Int(); n > 0 {
		payload["n"] = n
	}
	if quality := strings.TrimSpace(gjson.GetBytes(inputBytes, "quality").String()); quality != "" {
		payload["quality"] = quality
	}
	if rf := strings.TrimSpace(gjson.GetBytes(inputBytes, "response_format").String()); rf != "" {
		payload["response_format"] = rf
	}
	if style := strings.TrimSpace(gjson.GetBytes(inputBytes, "style").String()); style != "" {
		payload["style"] = style
	}
	if seed := gjson.GetBytes(inputBytes, "seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	}

	// Copy other custom fields
	if neg := utils.ExtractNegativePrompt(inputBytes); neg != "" {
		payload["negative_prompt"] = neg
	}

	targetPath := submitPath

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal openai-compat image payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      targetPath,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		AuthID:   gen.AuthID,
		Metadata: map[string]any{"provider": "openai-compat", "kind": "image"},
	}, nil
}

func (a *OpenAICompatAdapter) prepareVideoSubmit(gen aigc.ContentGeneration, inputBytes []byte, submitPath string) (aigc.GenerationExecutionRequest, error) {
	prompt := utils.ExtractPrompt(inputBytes)
	images := utils.ExtractImages(inputBytes)

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required for video generation")
	}

	payload := make(map[string]any)
	payload["model"] = gen.Model
	if prompt != "" {
		payload["prompt"] = prompt
	}
	if len(images) > 0 {
		payload["image_url"] = images[0]
		if len(images) > 1 {
			payload["last_frame_image_url"] = images[1]
		}
	}

	if duration := gjson.GetBytes(inputBytes, "duration").Int(); duration > 0 {
		payload["duration"] = duration
	} else if seconds := gjson.GetBytes(inputBytes, "seconds").Int(); seconds > 0 {
		payload["duration"] = seconds
	}

	if fps := gjson.GetBytes(inputBytes, "fps").Int(); fps > 0 {
		payload["fps"] = fps
	}
	if size := utils.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}
	if ratio := utils.RatioFromSize(gjson.GetBytes(inputBytes, "size").String()); ratio != "" {
		payload["ratio"] = ratio
	}
	if quality := strings.TrimSpace(gjson.GetBytes(inputBytes, "quality").String()); quality != "" {
		payload["quality"] = quality
	}
	if seed := gjson.GetBytes(inputBytes, "seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal openai-compat video payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      submitPath,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		AuthID:   gen.AuthID,
		Metadata: map[string]any{"provider": "openai-compat", "kind": "video"},
	}, nil
}

// ParseSubmit processes the response from OpenAI-compatible submit.
func (a *OpenAICompatAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedSubmitResultFromRaw("openai-compat", resp.StatusCode, resp.Body, nil), nil
	}

	// 1. Check synchronous image/video response with data array
	artifacts := extractOpenAICompatArtifacts(resp.Body)
	if len(artifacts) > 0 {
		return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "openai-compat"}), nil
	}

	// 2. Check asynchronous task response
	taskID := firstString(resp.Body, "id", "task_id", "video_id", "task.id")
	if taskID != "" {
		status := strings.ToLower(firstString(resp.Body, "status", "state"))
		if status == "completed" || status == "succeeded" || status == "success" {
			artifacts = extractOpenAICompatArtifacts(resp.Body)
			return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "openai-compat", "task_id": taskID}), nil
		}
		return utils.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "openai-compat"}), nil
	}

	return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "openai-compat"}), nil
}

// PreparePoll creates the query request for async task polling.
func (a *OpenAICompatAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("provider task_id is required for poll")
	}

	_, _, _, _, _, _, vidPollTpl := a.cfgFunc()
	if vidPollTpl == "" {
		vidPollTpl = "/videos/generations/%s"
	}

	pollURL := fmt.Sprintf(vidPollTpl, taskID)
	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      pollURL,
		Model:    input.Generation.Model,
		AuthID:   input.Generation.AuthID,
		Metadata: map[string]any{"provider": "openai-compat", "task_id": taskID},
	}, nil
}

// ParsePoll processes the query response from async polling.
func (a *OpenAICompatAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedPollResultFromRaw("openai-compat", resp.StatusCode, resp.Body, nil), nil
	}

	state := strings.ToLower(firstString(resp.Body, "status", "state"))
	switch state {
	case "completed", "succeeded", "success", "done":
		artifacts := extractOpenAICompatArtifacts(resp.Body)
		return utils.BuildCompletedPollResult(artifacts), nil
	case "failed", "error":
		return utils.BuildFailedPollResultFromRaw("openai-compat", 200, resp.Body, nil), nil
	case "canceled", "cancelled":
		return utils.BuildCanceledPollResult(), nil
	case "queued", "pending":
		return utils.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		progress := int(gjson.GetBytes(resp.Body, "progress").Int())
		if progress <= 0 {
			progress = 50
		}
		return utils.BuildRunningPollResult(progress, aigc.StageProviderRunning), nil
	}
}

// PrepareCancel creates cancel request.
func (a *OpenAICompatAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/videos/generations/%s/cancel", taskID),
		Model:  input.Generation.Model,
		AuthID: input.Generation.AuthID,
	}, nil
}

// ParseCancel processes cancel response.
func (a *OpenAICompatAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func extractOpenAICompatArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	// 1. Check data[] array (Standard OpenAI image or video item)
	dataItems := gjson.GetBytes(body, "data").Array()
	for _, item := range dataItems {
		u := item.Get("url").String()
		b64 := item.Get("b64_json").String()

		if strings.Contains(u, ".mp4") || strings.Contains(u, "video") {
			artifacts = append(artifacts, utils.CreateArtifact("output_video", "openai-compat", u, "video/mp4", 0, nil))
		} else if u != "" || b64 != "" {
			artifacts = append(artifacts, utils.ProcessImageArtifact("output_image", "openai-compat", u, b64, nil))
		}
	}

	// 2. Check direct video_url / url
	if len(artifacts) == 0 {
		if videoURL := firstString(body, "video_url", "output.video_url", "result.video_url"); videoURL != "" {
			artifacts = append(artifacts, utils.CreateArtifact("output_video", "openai-compat", videoURL, "video/mp4", 0, nil))
		} else if u := firstString(body, "url"); u != "" {
			artifacts = append(artifacts, utils.ProcessImageArtifact("output_image", "openai-compat", u, "", nil))
		} else if b64 := firstString(body, "b64_json"); b64 != "" {
			artifacts = append(artifacts, utils.ProcessImageArtifact("output_image", "openai-compat", "", b64, nil))
		}
	}

	return artifacts
}

func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if val := strings.TrimSpace(gjson.GetBytes(body, path).String()); val != "" {
			return val
		}
	}
	return ""
}

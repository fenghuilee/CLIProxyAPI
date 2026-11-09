// Package openaicompat provides a generic OpenAI-compatible image and video generation adapter.
package openaicompat

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

// OpenAICompatAdapter converts generic image/video requests to the OpenAI-compatible protocol.
type OpenAICompatAdapter struct{}

// NewOpenAICompatAdapter creates a generic adapter using standard OpenAI-compatible endpoints.
func NewOpenAICompatAdapter() *OpenAICompatAdapter {
	return &OpenAICompatAdapter{}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name:     "openai-compat",
		New:      func() driver.Adapter { return NewOpenAICompatAdapter() },
		Fallback: true,
	})
}

// Name returns the adapter's unique provider identifier used for routing and execution metadata.
func (a *OpenAICompatAdapter) Name() string {
	return "openai-compat"
}

// Supports excludes empty models, unsupported kinds, and cross-kind model names.
func (a *OpenAICompatAdapter) Supports(_ context.Context, req aigc.GenerationSupportRequest) bool {
	model := strings.ToLower(strings.TrimSpace(req.Model))
	if model == "" {
		return false
	}
	switch req.Kind {
	case aigc.ContentKindImage:
		return !isExplicitVideoModel(model)
	case aigc.ContentKindVideo:
		return !isExplicitImageModel(model)
	default:
		return false
	}
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

// PrepareSubmit builds the HTTP request for the upstream model.
func (a *OpenAICompatAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	if gen.Kind == aigc.ContentKindVideo {
		return a.prepareVideoSubmit(gen, inputBytes, "/videos/generations")
	}

	return a.prepareImageSubmit(gen, inputBytes, "/images/generations", "/images/edits")
}

func (a *OpenAICompatAdapter) prepareImageSubmit(gen aigc.ContentGeneration, inputBytes []byte, submitPath, editPath string) (aigc.GenerationExecutionRequest, error) {
	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)
	hasMask := ojson.GetBytes(inputBytes, "mask").Exists()

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required for image generation")
	}

	payload := make(map[string]any)
	payload["model"] = gen.Model
	if prompt != "" {
		payload["prompt"] = prompt
	}

	hasPrompt := prompt != ""
	hasImages := len(images) > 0

	targetPath := submitPath
	operation, _ := gen.Metadata["image_operation"].(string)
	switch operation {
	case "generations":
		if hasImages || hasMask {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("reference images require the images/edits endpoint")
		}
	case "edits":
		if !hasImages || !hasPrompt {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("image edits require prompt and reference images")
		}
		targetPath = editPath
	case "":
		// Preserve existing asynchronous/direct driver callers without an explicit operation.
		if hasImages && hasPrompt && editPath != "" {
			targetPath = editPath
		} else if hasImages && !hasPrompt {
			targetPath = "/images/variations"
		}
	default:
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("unsupported image operation")
	}

	if targetPath == editPath {
		var imageObjects []map[string]any
		for _, img := range images {
			imageObjects = append(imageObjects, map[string]any{
				"image_url": img,
			})
		}
		if len(imageObjects) > 0 {
			payload["images"] = imageObjects
		}
		if hasMask {
			maskVal := ojson.GetBytes(inputBytes, "mask")
			if maskVal.IsObject() && maskVal.Get("image_url").Exists() {
				payload["mask"] = maskVal.Value()
			} else if maskStr := strings.TrimSpace(maskVal.String()); maskStr != "" {
				payload["mask"] = map[string]any{
					"image_url": maskStr,
				}
			}
		}
	} else {
		if len(images) > 0 {
			if len(images) == 1 {
				payload["image"] = images[0]
			} else {
				payload["images"] = images
			}
		}
		if hasMask {
			payload["mask"] = ojson.GetBytes(inputBytes, "mask").Value()
		}
	}

	if size := driver.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}
	if n := ojson.GetBytes(inputBytes, "n").Int(); n > 0 {
		payload["n"] = n
	}
	if quality := strings.TrimSpace(ojson.GetBytes(inputBytes, "quality").String()); quality != "" {
		payload["quality"] = quality
	}
	if rf := strings.TrimSpace(ojson.GetBytes(inputBytes, "response_format").String()); rf != "" {
		payload["response_format"] = rf
	}
	if style := strings.TrimSpace(ojson.GetBytes(inputBytes, "style").String()); style != "" {
		payload["style"] = style
	}
	if seed := ojson.GetBytes(inputBytes, "seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	}

	if neg := driver.ExtractNegativePrompt(inputBytes); neg != "" {
		payload["negative_prompt"] = neg
	}

	doc := ojson.NewDocString(`{}`)
	for key, value := range payload {
		if errSet := doc.Set(key, value); errSet != nil {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("build openai-compat image payload: %w", errSet)
		}
	}
	// Preserve explicit output controls, including integer frame parameters.
	// Do not inject defaults or animation parameters into legacy image requests.
	for _, key := range []string{"output_format", "frame_count", "frame_duration_ms", "complexity", "background"} {
		value := ojson.GetBytes(inputBytes, key)
		if value.Exists() {
			if errSet := doc.SetRaw(key, value.Raw); errSet != nil {
				return aigc.GenerationExecutionRequest{}, fmt.Errorf("set image parameter %s: %w", key, errSet)
			}
		}
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      targetPath,
		Header:   header,
		Body:     doc.Bytes(),
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "openai-compat", "kind": "image"},
	}, nil
}

func (a *OpenAICompatAdapter) prepareVideoSubmit(gen aigc.ContentGeneration, inputBytes []byte, submitPath string) (aigc.GenerationExecutionRequest, error) {
	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)

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

	if duration := ojson.GetBytes(inputBytes, "duration").Int(); duration > 0 {
		payload["duration"] = duration
	} else if seconds := ojson.GetBytes(inputBytes, "seconds").Int(); seconds > 0 {
		payload["duration"] = seconds
	}

	if fps := ojson.GetBytes(inputBytes, "fps").Int(); fps > 0 {
		payload["fps"] = fps
	}
	if size := driver.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}
	if ratio := driver.RatioFromSize(ojson.GetBytes(inputBytes, "size").String()); ratio != "" {
		payload["ratio"] = ratio
	}
	if quality := strings.TrimSpace(ojson.GetBytes(inputBytes, "quality").String()); quality != "" {
		payload["quality"] = quality
	}
	if seed := ojson.GetBytes(inputBytes, "seed"); seed.Exists() {
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
		Metadata: map[string]any{"provider": "openai-compat", "kind": "video"},
	}, nil
}

// ParseSubmit extracts artifacts or async task ID from submit response.
func (a *OpenAICompatAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedSubmitResultFromRaw("openai-compat", resp.StatusCode, resp.Body, nil), nil
	}

	artifacts := extractOpenAICompatArtifacts(resp.Body)
	if len(artifacts) > 0 {
		return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "openai-compat"}), nil
	}

	taskID := firstString(resp.Body, "id", "task_id", "video_id", "task.id")
	if taskID != "" {
		status := strings.ToLower(firstString(resp.Body, "status", "state"))
		if status == "completed" || status == "succeeded" || status == "success" {
			artifacts = extractOpenAICompatArtifacts(resp.Body)
			return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "openai-compat", "task_id": taskID}), nil
		}
		return driver.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "openai-compat"}), nil
	}

	return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "openai-compat"}), nil
}

// PreparePoll queries task status using the OpenAI-compatible video tasks path.
func (a *OpenAICompatAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("provider task_id is required for poll")
	}

	pollURL := fmt.Sprintf("/videos/generations/%s", taskID)
	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      pollURL,
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": "openai-compat", "task_id": taskID},
	}, nil
}

// ParsePoll maps the upstream response to unified generation states.
func (a *OpenAICompatAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedPollResultFromRaw("openai-compat", resp.StatusCode, resp.Body, nil), nil
	}

	state := strings.ToLower(firstString(resp.Body, "status", "state"))
	switch state {
	case "completed", "succeeded", "success", "done":
		artifacts := extractOpenAICompatArtifacts(resp.Body)
		return driver.BuildCompletedPollResult(artifacts), nil
	case "failed", "error":
		return driver.BuildFailedPollResultFromRaw("openai-compat", 200, resp.Body, nil), nil
	case "canceled", "cancelled":
		return driver.BuildCanceledPollResult(), nil
	case "queued", "pending":
		return driver.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		progress := int(ojson.GetBytes(resp.Body, "progress").Int())
		if progress <= 0 {
			progress = 50
		}
		return driver.BuildRunningPollResult(progress, aigc.StageProviderRunning), nil
	}
}

// PrepareCancel creates cancellation request for async video generation task.
func (a *OpenAICompatAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/videos/generations/%s/cancel", taskID),
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel acknowledges cancel responses.
func (a *OpenAICompatAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

// ParseCallback parses task webhook notification from provider.
func (a *OpenAICompatAdapter) ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return a.ParsePoll(ctx, resp)
}

func extractOpenAICompatArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	dataItems := ojson.GetBytes(body, "data").Array()
	for _, item := range dataItems {
		u := item.Get("url").String()
		b64 := item.Get("b64_json").String()

		if strings.Contains(u, ".mp4") || strings.Contains(u, "video") {
			artifacts = append(artifacts, driver.CreateArtifact("output_video", "openai-compat", u, "video/mp4", 0, nil))
		} else if u != "" || b64 != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "openai-compat", u, b64, nil))
		}
	}

	if len(artifacts) == 0 {
		if videoURL := firstString(body, "output.0.uri", "output.0.url", "output.video_url", "video_url", "result.video_url"); videoURL != "" && (strings.Contains(videoURL, ".mp4") || strings.Contains(videoURL, "video") || (!strings.HasSuffix(videoURL, ".png") && !strings.HasSuffix(videoURL, ".jpg") && !strings.HasSuffix(videoURL, ".jpeg") && !strings.HasSuffix(videoURL, ".webp"))) {
			artifacts = append(artifacts, driver.CreateArtifact("output_video", "openai-compat", videoURL, "video/mp4", 0, nil))
		} else if videoURL := firstString(body, "video_url", "output.video_url", "result.video_url"); videoURL != "" {
			artifacts = append(artifacts, driver.CreateArtifact("output_video", "openai-compat", videoURL, "video/mp4", 0, nil))
		} else if u := firstString(body, "output.0.uri", "output.0.url", "url"); u != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "openai-compat", u, "", nil))
		} else if b64 := firstString(body, "b64_json"); b64 != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "openai-compat", "", b64, nil))
		}
	}

	return artifacts
}

func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if val := strings.TrimSpace(ojson.GetBytes(body, path).String()); val != "" {
			return val
		}
	}
	return ""
}

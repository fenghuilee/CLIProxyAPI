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

// VolcengineSeedreamAdapter handles Volcengine Doubao Seedream image generation & layer decomposition.
type VolcengineSeedreamAdapter struct {
	cfgFunc func() (models []string, endpoint string)
}

// NewVolcengineSeedreamAdapter creates a new VolcengineSeedreamAdapter.
func NewVolcengineSeedreamAdapter(cfgFunc func() (models []string, endpoint string)) *VolcengineSeedreamAdapter {
	return &VolcengineSeedreamAdapter{cfgFunc: cfgFunc}
}

// Name returns the provider name.
func (a *VolcengineSeedreamAdapter) Name() string {
	return "volcengine-seedream"
}

// Supports checks if this adapter supports the image model.
func (a *VolcengineSeedreamAdapter) Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool {
	if req.Kind != aigc.ContentKindImage {
		return false
	}
	models, _ := a.cfgFunc()
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
		if utils.MatchModel(patternLower, modelLower) {
			return true
		}
	}
	return false
}

// PrepareSubmit builds the Ark Seedream image generation request.
func (a *VolcengineSeedreamAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := gen.Model
	if strings.HasPrefix(model, "volcengine/") {
		model = strings.TrimPrefix(model, "volcengine/")
	}

	prompt := utils.ExtractPrompt(inputBytes)
	images := utils.ExtractImages(inputBytes)

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required")
	}

	payload := map[string]any{
		"model": model,
	}
	if prompt != "" {
		payload["prompt"] = prompt
	}

	// Reference images
	if len(images) == 1 {
		payload["image"] = images[0]
	} else if len(images) > 1 {
		payload["images"] = images
	}

	// Size / Resolution
	if size := utils.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}

	// Output count n
	if n := gjson.GetBytes(inputBytes, "n").Int(); n > 0 {
		payload["n"] = n
	}

	// Seed
	if seed := gjson.GetBytes(inputBytes, "extra_body.seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	} else if seed := gjson.GetBytes(inputBytes, "seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	}

	// Negative prompt
	if neg := utils.ExtractNegativePrompt(inputBytes); neg != "" {
		payload["negative_prompt"] = neg
	}

	// Watermark
	if wm := gjson.GetBytes(inputBytes, "extra_body.watermark"); wm.Exists() {
		payload["watermark"] = wm.Bool()
	} else if wm := gjson.GetBytes(inputBytes, "watermark"); wm.Exists() {
		payload["watermark"] = wm.Bool()
	}

	// Layer decomposition (图层拆分)
	if ld := gjson.GetBytes(inputBytes, "extra_body.layer_decomposition"); ld.Exists() {
		payload["layer_decomposition"] = ld.Value()
	} else if dl := gjson.GetBytes(inputBytes, "extra_body.decompose_layers"); dl.Exists() {
		payload["layer_decomposition"] = dl.Value()
	} else if ld := gjson.GetBytes(inputBytes, "layer_decomposition"); ld.Exists() {
		payload["layer_decomposition"] = ld.Value()
	} else if dl := gjson.GetBytes(inputBytes, "decompose_layers"); dl.Exists() {
		payload["layer_decomposition"] = dl.Value()
	}

	// Response format
	if rf := strings.TrimSpace(gjson.GetBytes(inputBytes, "response_format").String()); rf != "" {
		payload["response_format"] = rf
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal seedream submit payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	_, endpoint := a.cfgFunc()
	if endpoint == "" {
		endpoint = "/images/generations"
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      endpoint,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "volcengine-seedream"},
	}, nil
}

// ParseSubmit processes the response from Seedream.
func (a *VolcengineSeedreamAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedSubmitResultFromRaw("volcengine-seedream", resp.StatusCode, resp.Body, nil), nil
	}

	// 1. Check if asynchronous task ID returned
	if taskID := firstString(resp.Body, "id", "task_id", "task.id"); taskID != "" && !gjson.GetBytes(resp.Body, "data").Exists() {
		status := strings.ToLower(firstString(resp.Body, "status", "state"))
		if status == "succeeded" || status == "completed" || status == "success" {
			artifacts := extractSeedreamArtifacts(resp.Body)
			return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "volcengine-seedream", "task_id": taskID}), nil
		}
		return utils.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "volcengine-seedream"}), nil
	}

	// 2. Synchronous response with layer items or seedream/ark data
	hasLayers := gjson.GetBytes(resp.Body, "layers").Exists()
	hasLayerID := gjson.GetBytes(resp.Body, "data.0.layer_id").Exists()
	isVolcURL := strings.Contains(string(resp.Body), "volces.com") || strings.Contains(string(resp.Body), "volcengine")
	if hasLayers || hasLayerID || isVolcURL {
		artifacts := extractSeedreamArtifacts(resp.Body)
		if len(artifacts) > 0 {
			return utils.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "volcengine-seedream"}), nil
		}
	}

	return aigc.GenerationSubmitResult{}, fmt.Errorf("not a recognized seedream response")
}

// PreparePoll creates the query request for Seedream async tasks.
func (a *VolcengineSeedreamAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("task_id is required for seedream poll")
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      fmt.Sprintf("/contents/generations/tasks/%s", taskID),
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": "volcengine-seedream", "task_id": taskID},
	}, nil
}

// ParsePoll processes the query response from Seedream.
func (a *VolcengineSeedreamAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedPollResultFromRaw("volcengine-seedream", resp.StatusCode, resp.Body, nil), nil
	}

	state := strings.ToLower(firstString(resp.Body, "status", "state", "output.task_status"))
	if state == "" {
		return aigc.GenerationPollResult{}, fmt.Errorf("not a seedream poll response")
	}
	switch state {
	case "succeeded", "success", "completed", "done":
		artifacts := extractSeedreamArtifacts(resp.Body)
		if len(artifacts) == 0 {
			return aigc.GenerationPollResult{}, fmt.Errorf("no seedream image artifacts found")
		}
		return utils.BuildCompletedPollResult(artifacts), nil
	case "failed", "error":
		return utils.BuildFailedPollResultFromRaw("volcengine-seedream", 200, resp.Body, nil), nil
	case "canceled", "cancelled":
		return utils.BuildCanceledPollResult(), nil
	case "queued", "pending":
		return utils.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		return utils.BuildRunningPollResult(50, aigc.StageProviderRunning), nil
	}
}

// PrepareCancel creates cancel request for Seedream task.
func (a *VolcengineSeedreamAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/contents/generations/tasks/%s/cancel", taskID),
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel processes cancel response.
func (a *VolcengineSeedreamAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func extractSeedreamArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	// 1. Check data[] array (OpenAI/Ark image format)
	dataItems := gjson.GetBytes(body, "data").Array()
	for i, item := range dataItems {
		meta := make(map[string]any)
		if layerID := item.Get("layer_id").Int(); layerID > 0 {
			meta["layer_id"] = layerID
		}
		if layerName := item.Get("layer_name").String(); layerName != "" {
			meta["layer_name"] = layerName
		}
		if layerType := item.Get("layer_type").String(); layerType != "" {
			meta["layer_type"] = layerType
		}

		artifactType := "output_image"
		if len(meta) > 0 {
			artifactType = "layer"
		}

		u := item.Get("url").String()
		if u == "" {
			u = item.Get("image_url").String()
		}
		b64 := item.Get("b64_json").String()

		if strings.Contains(u, ".mp4") || strings.Contains(u, "video") {
			continue
		}

		if u != "" || b64 != "" {
			artifacts = append(artifacts, utils.ProcessImageArtifact(artifactType, "volcengine", u, b64, meta))
		} else {
			meta["index"] = i
		}
	}

	// 2. Check layers[] array (Dedicated layer decomposition response)
	layerItems := gjson.GetBytes(body, "layers").Array()
	for _, layer := range layerItems {
		meta := map[string]any{
			"layer_id":   layer.Get("id").Int(),
			"layer_name": layer.Get("name").String(),
			"layer_type": layer.Get("type").String(),
		}
		u := layer.Get("url").String()
		if u == "" {
			u = layer.Get("image_url").String()
		}
		if u != "" {
			artifacts = append(artifacts, utils.CreateArtifact(
				"layer", "volcengine", u, "image/png", 0, meta,
			))
		}
	}

	// 3. Check result.images[] or output.results[]
	resImages := gjson.GetBytes(body, "result.images").Array()
	if len(resImages) == 0 {
		resImages = gjson.GetBytes(body, "output.results").Array()
	}
	for _, img := range resImages {
		if u := img.Get("url").String(); u != "" {
			artifacts = append(artifacts, utils.CreateArtifact(
				"image", "volcengine", u, "image/png", 0, nil,
			))
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

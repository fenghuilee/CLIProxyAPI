package volcengine

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

// VolcengineSeedreamAdapter adapts Volcengine Doubao Seedream image generation and layer decomposition.
type VolcengineSeedreamAdapter struct{}

// NewVolcengineSeedreamAdapter creates a Seedream image adapter.
func NewVolcengineSeedreamAdapter() *VolcengineSeedreamAdapter {
	return &VolcengineSeedreamAdapter{}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "volcengine-seedream",
		New:  func() driver.Adapter { return NewVolcengineSeedreamAdapter() },
	})
}

// Name returns the provider identifier.
func (a *VolcengineSeedreamAdapter) Name() string {
	return "volcengine-seedream"
}

// Kind declares image content kind.
func (a *VolcengineSeedreamAdapter) Kind() aigc.ContentKind {
	return aigc.ContentKindImage
}

// Prefixes returns the vendor/model prefixes.
func (a *VolcengineSeedreamAdapter) Prefixes() []string {
	return []string{
		"volcengine/doubao-seedream",
	}
}

// PrepareSubmit transforms image input into Volcengine /images/generations POST request.
func (a *VolcengineSeedreamAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := driver.UpstreamModel(gen.Model)

	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required")
	}

	payload := map[string]any{
		"model": model,
	}
	if prompt != "" {
		payload["prompt"] = prompt
	}

	if len(images) == 1 {
		payload["image"] = images[0]
	} else if len(images) > 1 {
		payload["images"] = images
	}

	if size := driver.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}

	if n := ojson.GetBytes(inputBytes, "n").Int(); n > 0 {
		payload["n"] = n
	}

	if seed := ojson.GetBytes(inputBytes, "extra_body.seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	} else if seed := ojson.GetBytes(inputBytes, "seed"); seed.Exists() {
		payload["seed"] = seed.Int()
	}

	if neg := driver.ExtractNegativePrompt(inputBytes); neg != "" {
		payload["negative_prompt"] = neg
	}

	if wm := ojson.GetBytes(inputBytes, "extra_body.watermark"); wm.Exists() {
		payload["watermark"] = wm.Bool()
	} else if wm := ojson.GetBytes(inputBytes, "watermark"); wm.Exists() {
		payload["watermark"] = wm.Bool()
	}

	if ld := ojson.GetBytes(inputBytes, "extra_body.layer_decomposition"); ld.Exists() {
		payload["layer_decomposition"] = ld.Value()
	} else if dl := ojson.GetBytes(inputBytes, "extra_body.decompose_layers"); dl.Exists() {
		payload["layer_decomposition"] = dl.Value()
	} else if ld := ojson.GetBytes(inputBytes, "layer_decomposition"); ld.Exists() {
		payload["layer_decomposition"] = ld.Value()
	} else if dl := ojson.GetBytes(inputBytes, "decompose_layers"); dl.Exists() {
		payload["layer_decomposition"] = dl.Value()
	}

	if rf := strings.TrimSpace(ojson.GetBytes(inputBytes, "response_format").String()); rf != "" {
		payload["response_format"] = rf
	}

	if mask := ojson.GetBytes(inputBytes, "mask"); mask.Exists() {
		payload["mask"] = mask.Value()
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal seedream submit payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      "/images/generations",
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "volcengine-seedream"},
	}, nil
}

// ParseSubmit parses Seedream response into submit result.
func (a *VolcengineSeedreamAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedSubmitResultFromRaw("volcengine-seedream", resp.StatusCode, resp.Body, nil), nil
	}

	if taskID := firstString(resp.Body, "id", "task_id", "task.id"); taskID != "" && !ojson.GetBytes(resp.Body, "data").Exists() {
		status := strings.ToLower(firstString(resp.Body, "status", "state"))
		if status == "succeeded" || status == "completed" || status == "success" {
			artifacts := extractSeedreamArtifacts(resp.Body)
			return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "volcengine-seedream", "task_id": taskID}), nil
		}
		return driver.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "volcengine-seedream"}), nil
	}

	hasLayers := ojson.GetBytes(resp.Body, "layers").Exists()
	hasLayerID := ojson.GetBytes(resp.Body, "data.0.layer_id").Exists()
	isVolcURL := strings.Contains(string(resp.Body), "volces.com") || strings.Contains(string(resp.Body), "volcengine")
	if hasLayers || hasLayerID || isVolcURL {
		artifacts := extractSeedreamArtifacts(resp.Body)
		if len(artifacts) > 0 {
			return driver.BuildCompletedSubmitResult(artifacts, map[string]any{"provider": "volcengine-seedream"}), nil
		}
	}

	return aigc.GenerationSubmitResult{}, fmt.Errorf("not a recognized seedream response")
}

// PreparePoll prepares async task polling request.
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

// ParsePoll converts poll response to generation result.
func (a *VolcengineSeedreamAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedPollResultFromRaw("volcengine-seedream", resp.StatusCode, resp.Body, nil), nil
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
		return driver.BuildCompletedPollResult(artifacts), nil
	case "failed", "error":
		return driver.BuildFailedPollResultFromRaw("volcengine-seedream", 200, resp.Body, nil), nil
	case "canceled", "cancelled":
		return driver.BuildCanceledPollResult(), nil
	case "queued", "pending":
		return driver.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		return driver.BuildRunningPollResult(50, aigc.StageProviderRunning), nil
	}
}

// PrepareCancel creates cancel request.
func (a *VolcengineSeedreamAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/contents/generations/tasks/%s/cancel", taskID),
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel acknowledges cancellation.
func (a *VolcengineSeedreamAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func extractSeedreamArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	dataItems := ojson.GetBytes(body, "data").Array()
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
			artifacts = append(artifacts, driver.ProcessImageArtifact(artifactType, "volcengine", u, b64, meta))
		} else {
			meta["index"] = i
		}
	}

	layerItems := ojson.GetBytes(body, "layers").Array()
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
			artifacts = append(artifacts, driver.CreateArtifact(
				"layer", "volcengine", u, "image/png", 0, meta,
			))
		}
	}

	resImages := ojson.GetBytes(body, "result.images").Array()
	if len(resImages) == 0 {
		resImages = ojson.GetBytes(body, "output.results").Array()
	}
	for _, img := range resImages {
		if u := img.Get("url").String(); u != "" {
			artifacts = append(artifacts, driver.CreateArtifact(
				"image", "volcengine", u, "image/png", 0, nil,
			))
		}
	}

	return artifacts
}

// Package bananarouter provides an image adapter for BananaRouter / Bter OpenAI-compatible endpoints.
package bananarouter

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

const (
	adapterName            = "bananarouter"
	defaultGenerationsPath = "/images/generations"
	defaultEditsPath       = "/images/edits"
)

// BananaRouterAdapter adapts BananaRouter/Bter image generation and editing using pure JSON.
type BananaRouterAdapter struct {
	generationsPath string
	editsPath       string
}

// NewBananaRouterAdapter creates a new BananaRouter image adapter.
func NewBananaRouterAdapter() *BananaRouterAdapter {
	return &BananaRouterAdapter{
		generationsPath: defaultGenerationsPath,
		editsPath:       defaultEditsPath,
	}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: adapterName,
		New:  func() driver.Adapter { return NewBananaRouterAdapter() },
	})
}

// Name returns the unique adapter identifier.
func (a *BananaRouterAdapter) Name() string {
	return adapterName
}

// Kind returns the supported content kind.
func (a *BananaRouterAdapter) Kind() aigc.ContentKind {
	return aigc.ContentKindImage
}

// Prefixes defines the model routing prefixes handled by BananaRouter.
func (a *BananaRouterAdapter) Prefixes() []string {
	return []string{
		"bananarouter/gpt-image",
		"bter/gpt-image",
		"bter2/gpt-image",
		"bter3/gpt-image",
	}
}

// PrepareSubmit builds the HTTP request for BananaRouter image generation or editing.
// Unlike standard OpenAI endpoints that require multipart/form-data for edits,
// BananaRouter accepts pure JSON payloads carrying image URLs.
func (a *BananaRouterAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := driver.UpstreamModel(gen.Model)
	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)

	targetPath := a.generationsPath
	operation, _ := gen.Metadata["image_operation"].(string)

	isEdit := operation == "edits" || len(images) > 0
	if isEdit {
		if prompt == "" && len(images) == 0 {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("bananarouter: prompt or reference images required for image edit")
		}
		targetPath = a.editsPath
	} else if prompt == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("bananarouter: prompt is required for image generation")
	}

	payload := make(map[string]any)
	payload["model"] = model
	payload["prompt"] = prompt

	if size := driver.NormalizeSizeToX(inputBytes); size != "" {
		payload["size"] = size
	}
	if q := strings.TrimSpace(ojson.GetBytes(inputBytes, "quality").String()); q != "" {
		payload["quality"] = q
	}
	if rf := strings.TrimSpace(ojson.GetBytes(inputBytes, "response_format").String()); rf != "" {
		payload["response_format"] = rf
	}
	if of := strings.TrimSpace(ojson.GetBytes(inputBytes, "output_format").String()); of != "" {
		payload["output_format"] = of
	}
	if n := ojson.GetBytes(inputBytes, "n").Int(); n > 0 {
		payload["n"] = n
	}

	if isEdit && len(images) > 0 {
		payload["images"] = images
		if len(images) == 1 {
			payload["image"] = images[0]
		}
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal bananarouter image payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      targetPath,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": adapterName, "kind": "image"},
	}, nil
}

// GetUsage extracts token usage from BananaRouter JSON response.
func (a *BananaRouterAdapter) GetUsage(body []byte) *aigc.GenerationUsage {
	return driver.ExtractStandardUsage(body)
}

// ParseSubmit extracts synchronous image output or an asynchronous task ID from the response.
func (a *BananaRouterAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedSubmitResultFromRaw(adapterName, resp.StatusCode, resp.Body, nil), nil
	}

	meta := map[string]any{"provider": adapterName}
	usage := a.GetUsage(resp.Body)
	if usage != nil {
		meta["usage"] = map[string]any{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
			"total_tokens":  usage.TotalTokens,
		}
	}

	artifacts := extractImageArtifacts(resp.Body)
	if len(artifacts) > 0 {
		res := driver.BuildCompletedSubmitResult(artifacts, meta)
		res.Usage = usage
		return res, nil
	}

	taskID := firstString(resp.Body, "id", "task_id", "data.task_id")
	if taskID != "" {
		meta["task_id"] = taskID
		status := strings.ToLower(firstString(resp.Body, "status", "state"))
		if status == "completed" || status == "succeeded" || status == "success" {
			artifacts = extractImageArtifacts(resp.Body)
			res := driver.BuildCompletedSubmitResult(artifacts, meta)
			res.Usage = usage
			return res, nil
		}
		res := driver.BuildAsyncSubmitResult(taskID, meta)
		res.Usage = usage
		return res, nil
	}

	res := driver.BuildCompletedSubmitResult(artifacts, meta)
	res.Usage = usage
	return res, nil
}

// PreparePoll prepares status lookup request for asynchronous tasks.
func (a *BananaRouterAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := strings.TrimSpace(input.Generation.ProviderTaskID)
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("bananarouter: task_id is required for polling")
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      "/images/tasks/" + taskID,
		Header:   header,
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": adapterName, "task_id": taskID},
	}, nil
}

// ParsePoll translates task polling response into a standard GenerationPollResult.
func (a *BananaRouterAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedPollResultFromRaw(adapterName, resp.StatusCode, resp.Body, nil), nil
	}

	statusRaw := strings.ToLower(firstString(resp.Body, "status", "state", "task_status"))
	meta := map[string]any{"provider": adapterName}
	usage := a.GetUsage(resp.Body)
	if usage != nil {
		meta["usage"] = map[string]any{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
			"total_tokens":  usage.TotalTokens,
		}
	}

	switch statusRaw {
	case "succeeded", "success", "completed":
		artifacts := extractImageArtifacts(resp.Body)
		return aigc.GenerationPollResult{
			Status:           aigc.StatusSucceeded,
			Stage:            aigc.StageCompleted,
			Progress:         100,
			Artifacts:        artifacts,
			ProviderResponse: resp.Body,
			Metadata:         meta,
			Usage:            usage,
		}, nil
	case "failed", "error":
		errMsg := firstString(resp.Body, "error.message", "error_message", "message", "error")
		if errMsg == "" {
			errMsg = "bananarouter task failed"
		}
		return aigc.GenerationPollResult{
			Status:           aigc.StatusFailed,
			Stage:            aigc.StageFailed,
			ErrorMessage:     errMsg,
			ProviderResponse: resp.Body,
			Metadata:         meta,
			Usage:            usage,
		}, nil
	default:
		return aigc.GenerationPollResult{
			Status:           aigc.StatusRunning,
			Stage:            aigc.StageProviderRunning,
			Progress:         50,
			ProviderResponse: resp.Body,
			Metadata:         meta,
			Usage:            usage,
		}, nil
	}
}

// PrepareCancel returns an unsupported error for BananaRouter image tasks.
func (a *BananaRouterAdapter) PrepareCancel(context.Context, aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, fmt.Errorf("bananarouter: task cancellation is not supported")
}

// ParseCancel returns an unsupported error for BananaRouter image tasks.
func (a *BananaRouterAdapter) ParseCancel(context.Context, aigc.GenerationExecutionResponse) error {
	return fmt.Errorf("bananarouter: task cancellation is not supported")
}

// ParseCallback delegates callback translation to ParsePoll.
func (a *BananaRouterAdapter) ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return a.ParsePoll(ctx, resp)
}

func extractImageArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact
	dataArray := ojson.GetBytes(body, "data").Array()
	for _, item := range dataArray {
		if u := item.Get("url").String(); u != "" {
			artifacts = append(artifacts, aigc.ContentGenerationArtifact{
				ArtifactType: "output_image",
				URI:          u,
			})
		} else if b64 := item.Get("b64_json").String(); b64 != "" {
			artifacts = append(artifacts, aigc.ContentGenerationArtifact{
				ArtifactType: "output_image",
				URI:          "data:image/png;base64," + b64,
			})
		}
	}
	return artifacts
}

func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if val := ojson.GetBytes(body, path).String(); val != "" {
			return val
		}
	}
	return ""
}

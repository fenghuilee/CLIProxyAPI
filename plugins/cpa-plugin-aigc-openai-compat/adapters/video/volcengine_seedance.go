package video

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-aigc-openai-compat/utils"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

// VolcengineSeedanceAdapter handles Volcengine Doubao Seedance video generation tasks.
type VolcengineSeedanceAdapter struct {
	cfgFunc func() (models []string, endpoint string)
}

// NewVolcengineSeedanceAdapter creates a new VolcengineSeedanceAdapter.
func NewVolcengineSeedanceAdapter(cfgFunc func() (models []string, endpoint string)) *VolcengineSeedanceAdapter {
	return &VolcengineSeedanceAdapter{cfgFunc: cfgFunc}
}

// Name returns the provider name.
func (a *VolcengineSeedanceAdapter) Name() string {
	return "volcengine-seedance"
}

// Supports checks if this adapter supports the video model.
func (a *VolcengineSeedanceAdapter) Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool {
	if req.Kind != aigc.ContentKindVideo {
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

// PrepareSubmit transforms AIGC video generation input into a Volcengine ARK video task.
func (a *VolcengineSeedanceAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := gen.Model
	if strings.HasPrefix(model, "volcengine/") {
		model = strings.TrimPrefix(model, "volcengine/")
	}

	contentItems := buildSeedanceContentItems(inputBytes)
	if len(contentItems) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or content is required")
	}

	canonical := utils.ExtractCanonical(gen)
	ratio := canonical.Ratio
	resolution := canonical.ResolutionTier

	if strings.EqualFold(model, "doubao-seedance-2-0") && strings.EqualFold(resolution, "4k") {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("4k resolution is disabled for doubao-seedance-2-0")
	}

	payload := map[string]any{
		"model":   model,
		"content": contentItems,
	}

	frames := firstInt(inputBytes, "extra_body.frames")
	if frames > 0 {
		payload["frames"] = frames
	} else {
		seconds := canonical.DurationSec
		if seconds > 0 || seconds == -1 {
			payload["duration"] = seconds
		} else if strings.Contains(strings.ToLower(model), "seedance-2-5") {
			payload["duration"] = -1
		} else {
			payload["duration"] = 5
		}
	}

	if ratio != "" {
		payload["ratio"] = ratio
	}
	if resolution != "" {
		payload["resolution"] = resolution
	}

	if generateAudio, ok := firstBool(inputBytes, "generate_audio", "extra_body.generate_audio"); ok {
		payload["generate_audio"] = generateAudio
	} else if canonical.GenerateAudio {
		payload["generate_audio"] = true
	}
	if cameraFixed, ok := firstBool(inputBytes, "camera_fixed", "extra_body.camera_fixed"); ok {
		payload["camera_fixed"] = cameraFixed
	}
	if watermark, ok := firstBool(inputBytes, "watermark", "extra_body.watermark"); ok {
		payload["watermark"] = watermark
	}
	if returnLastFrame, ok := firstBool(inputBytes, "return_last_frame", "extra_body.return_last_frame"); ok {
		payload["return_last_frame"] = returnLastFrame
	}
	if draft, ok := firstBool(inputBytes, "draft", "extra_body.draft"); ok {
		payload["draft"] = draft
	}
	if seed := firstInt(inputBytes, "seed", "extra_body.seed"); seed != 0 {
		payload["seed"] = seed
	}
	if serviceTier := firstString(inputBytes, "service_tier", "extra_body.service_tier"); serviceTier != "" {
		payload["service_tier"] = serviceTier
	}
	if outputFormat := firstString(inputBytes, "output_format", "extra_body.output_format"); outputFormat != "" {
		payload["output_format"] = outputFormat
	}
	if safetyIdentifier := firstString(inputBytes, "safety_identifier", "extra_body.safety_identifier"); safetyIdentifier != "" {
		payload["safety_identifier"] = safetyIdentifier
	}
	if callbackURL := firstString(inputBytes, "callback_url", "extra_body.callback_url"); callbackURL != "" {
		payload["callback_url"] = callbackURL
	}
	if execExpiresAfter := firstInt(inputBytes, "execution_expires_after", "extra_body.execution_expires_after"); execExpiresAfter > 0 {
		payload["execution_expires_after"] = execExpiresAfter
	}
	if tools := firstValue(inputBytes, "tools", "extra_body.tools"); tools != nil {
		payload["tools"] = tools
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal seedance submit payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	_, endpoint := a.cfgFunc()
	if endpoint == "" {
		endpoint = "/contents/generations/tasks"
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      endpoint,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "volcengine-seedance"},
	}, nil
}

// ParseSubmit processes the response from Seedance task creation.
func (a *VolcengineSeedanceAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedSubmitResultFromRaw("volcengine-seedance", resp.StatusCode, resp.Body, nil), nil
	}

	if gjson.GetBytes(resp.Body, "data").Exists() && !gjson.GetBytes(resp.Body, "content.video_url").Exists() {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("not a seedance video response")
	}

	taskID := firstString(resp.Body, "id", "task_id", "task.id")
	if taskID == "" {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("missing task id in seedance submit response")
	}

	return aigc.GenerationSubmitResult{
		ProviderTaskID:   taskID,
		Status:           aigc.StatusRunning,
		Stage:            aigc.StageProviderRunning,
		Progress:         5,
		ProviderResponse: json.RawMessage(resp.Body),
		Metadata:         map[string]any{"provider": "volcengine-seedance", "task_id": taskID},
	}, nil
}

// PreparePoll creates the request to query Seedance task status.
func (a *VolcengineSeedanceAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("missing provider task id for poll")
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      fmt.Sprintf("/contents/generations/tasks/%s", taskID),
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": "volcengine-seedance", "task_id": taskID},
	}, nil
}

// ParsePoll processes the query response from Seedance.
func (a *VolcengineSeedanceAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedPollResultFromRaw("volcengine-seedance", resp.StatusCode, resp.Body, nil), nil
	}

	if gjson.GetBytes(resp.Body, "data").Exists() && !gjson.GetBytes(resp.Body, "content.video_url").Exists() {
		return aigc.GenerationPollResult{}, fmt.Errorf("not a seedance poll response")
	}

	state := strings.ToLower(firstString(resp.Body, "status", "state"))
	videoURL := firstString(resp.Body,
		"content.video_url", "content.video_url.url",
		"output.0.uri", "output.0.url", "output.video_url",
		"data.0.url", "data.0.uri",
		"video_url", "download_url", "result.video_url",
	)
	lastFrameURL := firstString(resp.Body,
		"content.last_frame_url", "last_frame_url", "result.last_frame_url",
		"output.1.uri", "output.1.url",
	)

	if state == "" && videoURL == "" {
		return aigc.GenerationPollResult{}, fmt.Errorf("not a seedance poll response")
	}

	switch state {
	case "succeeded", "success", "succeed", "completed", "done":
		var artifacts []aigc.ContentGenerationArtifact
		if videoURL != "" {
			artifacts = append(artifacts, utils.CreateArtifact(
				"output_video", "volcengine", videoURL, "video/mp4", 0, nil,
			))
		}
		if lastFrameURL != "" {
			artifacts = append(artifacts, utils.CreateArtifact(
				"last_frame", "volcengine", lastFrameURL, "image/png", 0, nil,
			))
		}
		return utils.BuildCompletedPollResult(artifacts), nil

	case "failed", "error", "expired":
		return utils.BuildFailedPollResultFromRaw("volcengine-seedance", 200, resp.Body, nil), nil

	case "canceled", "cancelled":
		return utils.BuildCanceledPollResult(), nil

	case "queued", "pending":
		return utils.BuildRunningPollResult(10, aigc.StageProviderRunning), nil

	default: // running, processing, in_progress
		return utils.BuildRunningPollResult(50, aigc.StageProviderRunning), nil
	}
}

// PrepareCancel creates the request to cancel a Seedance task.
func (a *VolcengineSeedanceAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/contents/generations/tasks/%s/cancel", taskID),
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel processes the cancel response.
func (a *VolcengineSeedanceAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

type seedanceRef struct {
	Kind string
	URL  string
	Role string
}

func detectMediaKind(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if strings.HasPrefix(lower, "data:video/") || strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mov") || strings.HasSuffix(lower, ".webm") || strings.HasSuffix(lower, ".mkv") {
		return "video"
	}
	if strings.HasPrefix(lower, "data:audio/") || strings.HasSuffix(lower, ".mp3") || strings.HasSuffix(lower, ".wav") || strings.HasSuffix(lower, ".aac") || strings.HasSuffix(lower, ".m4a") || strings.HasSuffix(lower, ".ogg") || strings.HasSuffix(lower, ".flac") {
		return "audio"
	}
	return "image"
}

func extractSeedanceReferences(body []byte) []seedanceRef {
	var refs []seedanceRef

	addRef := func(rawURL, explicitKind, explicitRole string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		kind := explicitKind
		if kind == "" {
			kind = detectMediaKind(rawURL)
		}
		role := explicitRole
		if role == "" {
			switch kind {
			case "video":
				role = "reference_video"
			case "audio":
				role = "reference_audio"
			default:
				role = "reference_image"
			}
		}
		refs = append(refs, seedanceRef{
			Kind: kind,
			URL:  rawURL,
			Role: role,
		})
	}

	processNode := func(node gjson.Result) {
		if node.Type == gjson.String {
			addRef(node.String(), "", "")
			return
		}
		if !node.IsObject() {
			return
		}

		role := strings.TrimSpace(node.Get("role").String())
		nodeType := strings.TrimSpace(node.Get("type").String())

		if u := strings.TrimSpace(node.Get("video_url.url").String()); u != "" {
			addRef(u, "video", role)
			return
		} else if u := strings.TrimSpace(node.Get("video_url").String()); u != "" {
			addRef(u, "video", role)
			return
		}

		if u := strings.TrimSpace(node.Get("audio_url.url").String()); u != "" {
			addRef(u, "audio", role)
			return
		} else if u := strings.TrimSpace(node.Get("audio_url").String()); u != "" {
			addRef(u, "audio", role)
			return
		}

		if u := strings.TrimSpace(node.Get("image_url.url").String()); u != "" {
			addRef(u, "image", role)
			return
		} else if u := strings.TrimSpace(node.Get("image_url").String()); u != "" {
			addRef(u, "image", role)
			return
		}

		if u := strings.TrimSpace(node.Get("url").String()); u != "" {
			explicitKind := ""
			if nodeType == "video" || nodeType == "video_url" {
				explicitKind = "video"
			} else if nodeType == "audio" || nodeType == "audio_url" {
				explicitKind = "audio"
			} else if nodeType == "image" || nodeType == "image_url" {
				explicitKind = "image"
			}
			addRef(u, explicitKind, role)
			return
		}
	}

	for _, path := range []string{"input_reference", "extra_body.input_reference"} {
		refNode := gjson.GetBytes(body, path)
		if !refNode.Exists() {
			continue
		}
		if refNode.IsArray() {
			for _, item := range refNode.Array() {
				processNode(item)
			}
		} else {
			processNode(refNode)
		}
	}

	for _, path := range []string{"reference_images", "extra_body.reference_images", "reference_image_urls", "extra_body.reference_image_urls"} {
		refsNode := gjson.GetBytes(body, path)
		if !refsNode.Exists() {
			continue
		}
		if refsNode.IsArray() {
			for _, item := range refsNode.Array() {
				processNode(item)
			}
		} else if refsNode.Type == gjson.String {
			val := refsNode.String()
			if strings.Contains(val, ",") {
				for _, part := range strings.Split(val, ",") {
					addRef(part, "image", "reference_image")
				}
			} else {
				addRef(val, "image", "reference_image")
			}
		}
	}

	return refs
}

func buildSeedanceContentItems(body []byte) []map[string]any {
	contentArray := gjson.GetBytes(body, "content")
	if !contentArray.Exists() {
		contentArray = gjson.GetBytes(body, "extra_body.content")
	}
	if contentArray.IsArray() && len(contentArray.Array()) > 0 {
		var items []map[string]any
		for _, node := range contentArray.Array() {
			itemType := strings.TrimSpace(node.Get("type").String())
			role := strings.TrimSpace(node.Get("role").String())

			switch itemType {
			case "text":
				items = append(items, map[string]any{
					"type": "text",
					"text": node.Get("text").String(),
				})
			case "image_url":
				urlStr := node.Get("image_url.url").String()
				if urlStr == "" {
					urlStr = node.Get("image_url").String()
				}
				if urlStr == "" {
					urlStr = node.Get("url").String()
				}
				if urlStr != "" {
					item := map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": urlStr},
					}
					if role != "" {
						item["role"] = role
					}
					items = append(items, item)
				}
			case "video_url":
				urlStr := node.Get("video_url.url").String()
				if urlStr == "" {
					urlStr = node.Get("video_url").String()
				}
				if urlStr == "" {
					urlStr = node.Get("url").String()
				}
				if urlStr != "" {
					if role == "" {
						role = "reference_video"
					}
					items = append(items, map[string]any{
						"type":      "video_url",
						"video_url": map[string]any{"url": urlStr},
						"role":      role,
					})
				}
			case "audio_url":
				urlStr := node.Get("audio_url.url").String()
				if urlStr == "" {
					urlStr = node.Get("audio_url").String()
				}
				if urlStr == "" {
					urlStr = node.Get("url").String()
				}
				if urlStr != "" {
					if role == "" {
						role = "reference_audio"
					}
					items = append(items, map[string]any{
						"type":      "audio_url",
						"audio_url": map[string]any{"url": urlStr},
						"role":      role,
					})
				}
			case "draft_task":
				taskID := node.Get("draft_task.id").String()
				if taskID == "" {
					taskID = node.Get("id").String()
				}
				if taskID != "" {
					items = append(items, map[string]any{
						"type":       "draft_task",
						"draft_task": map[string]any{"id": taskID},
					})
				}
			}
		}
		if len(items) > 0 {
			return items
		}
	}

	var items []map[string]any
	if prompt := utils.ExtractPrompt(body); prompt != "" {
		items = append(items, map[string]any{
			"type": "text",
			"text": prompt,
		})
	}

	if draftID := firstString(body, "draft_task_id", "draft_task.id", "extra_body.draft_task_id", "extra_body.draft_task.id"); draftID != "" {
		items = append(items, map[string]any{
			"type":       "draft_task",
			"draft_task": map[string]any{"id": draftID},
		})
	}

	references := extractSeedanceReferences(body)
	if len(references) > 0 {
		for _, ref := range references {
			switch ref.Kind {
			case "video":
				items = append(items, map[string]any{
					"type":      "video_url",
					"video_url": map[string]any{"url": ref.URL},
					"role":      ref.Role,
				})
			case "audio":
				items = append(items, map[string]any{
					"type":      "audio_url",
					"audio_url": map[string]any{"url": ref.URL},
					"role":      ref.Role,
				})
			default: // image
				items = append(items, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": ref.URL},
					"role":      ref.Role,
				})
			}
		}
		return items
	}

	if singleImg := firstString(body, "image", "image_url", "first_frame", "extra_body.image", "extra_body.image_url", "extra_body.first_frame"); singleImg != "" {
		items = append(items, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": singleImg},
			"role":      "first_frame",
		})
	}

	return items
}

func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if val := strings.TrimSpace(gjson.GetBytes(body, path).String()); val != "" {
			return val
		}
	}
	return ""
}

func firstInt(body []byte, paths ...string) int64 {
	for _, path := range paths {
		if node := gjson.GetBytes(body, path); node.Exists() {
			if node.Type == gjson.Number {
				return node.Int()
			}
			if node.Type == gjson.String {
				if val, err := strconv.ParseInt(strings.TrimSpace(node.String()), 10, 64); err == nil {
					return val
				}
			}
		}
	}
	return 0
}

func firstBool(body []byte, paths ...string) (bool, bool) {
	for _, path := range paths {
		if node := gjson.GetBytes(body, path); node.Exists() {
			return node.Bool(), true
		}
	}
	return false, false
}

func firstValue(body []byte, paths ...string) any {
	for _, path := range paths {
		if node := gjson.GetBytes(body, path); node.Exists() {
			return node.Value()
		}
	}
	return nil
}

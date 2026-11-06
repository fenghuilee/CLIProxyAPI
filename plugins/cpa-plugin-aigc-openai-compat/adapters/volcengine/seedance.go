package volcengine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// SeedanceConfig describes the channel configuration for SeedanceAdapter.
type SeedanceConfig struct {
	Provider         string
	ArtifactProvider string
	DefaultEndpoint  string
	SupportsCancel   bool
	Prefixes         []string
}

// SeedanceAdapter adapts video tasks to Volcengine or proxy Doubao Seedance protocols.
type SeedanceAdapter struct {
	provider         string
	artifactProvider string
	defaultEndpoint  string
	supportsCancel   bool
	prefixes         []string
}

// VolcengineSeedanceAdapter is an alias to SeedanceAdapter.
type VolcengineSeedanceAdapter = SeedanceAdapter

// NewSeedanceAdapter creates a SeedanceAdapter with explicit channel configuration.
func NewSeedanceAdapter(cfg SeedanceConfig) *SeedanceAdapter {
	return &SeedanceAdapter{
		provider:         cfg.Provider,
		artifactProvider: cfg.ArtifactProvider,
		defaultEndpoint:  cfg.DefaultEndpoint,
		supportsCancel:   cfg.SupportsCancel,
		prefixes:         cfg.Prefixes,
	}
}

// NewVolcengineSeedanceAdapter creates the official Volcengine Seedance adapter.
func NewVolcengineSeedanceAdapter() *VolcengineSeedanceAdapter {
	return NewSeedanceAdapter(SeedanceConfig{
		Provider:         "volcengine-seedance",
		ArtifactProvider: "volcengine",
		DefaultEndpoint:  "/contents/generations/tasks",
		SupportsCancel:   true,
		Prefixes: []string{
			"volcengine/doubao-seedance",
		},
	})
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "volcengine-seedance",
		New:  func() driver.Adapter { return NewVolcengineSeedanceAdapter() },
	})
}

// Name returns the provider identifier.
func (a *SeedanceAdapter) Name() string {
	return a.provider
}

// Kind declares video content kind.
func (a *SeedanceAdapter) Kind() aigc.ContentKind {
	return aigc.ContentKindVideo
}

// Prefixes returns the registered model prefixes.
func (a *SeedanceAdapter) Prefixes() []string {
	return a.prefixes
}

// PrepareSubmit builds Seedance task creation request.
func (a *SeedanceAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := driver.UpstreamModel(gen.Model)

	contentItems := buildSeedanceContentItems(inputBytes)
	if len(contentItems) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or content is required")
	}

	canonical := driver.ExtractCanonical(gen)
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
		if firstInt(inputBytes, "seconds", "extra_body.seconds", "duration", "extra_body.duration") == -1 {
			seconds = -1
		}
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
	if seed := firstValue(inputBytes, "seed", "extra_body.seed"); seed != nil {
		payload["seed"] = firstInt(inputBytes, "seed", "extra_body.seed")
	}
	if taskType := firstString(inputBytes, "omni_reference_task_type", "extra_body.omni_reference_task_type"); taskType != "" {
		payload["omni_reference_task_type"] = taskType
	}
	if priority := firstValue(inputBytes, "priority", "extra_body.priority"); priority != nil {
		payload["priority"] = firstInt(inputBytes, "priority", "extra_body.priority")
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

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      a.defaultEndpoint,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": a.Name()},
	}, nil
}

// ParseSubmit parses task submission response.
func (a *SeedanceAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || seedanceResponseFailed(resp.Body) {
		return driver.BuildFailedSubmitResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), nil
	}

	if ojson.GetBytes(resp.Body, "data").Exists() && !ojson.GetBytes(resp.Body, "content.video_url").Exists() {
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
		Metadata:         map[string]any{"provider": a.Name(), "task_id": taskID},
	}, nil
}

// PreparePoll prepares status polling request with escaped task ID.
func (a *SeedanceAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("missing provider task id for poll")
	}
	endpoint := a.defaultEndpoint

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      strings.TrimRight(endpoint, "/") + "/" + url.PathEscape(taskID),
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": a.Name(), "task_id": taskID},
	}, nil
}

// ParsePoll converts channel poll response to unified generation result.
func (a *SeedanceAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || seedanceResponseFailed(resp.Body) {
		return withSeedanceUsage(driver.BuildFailedPollResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), resp.Body), nil
	}

	if ojson.GetBytes(resp.Body, "data").Exists() && !ojson.GetBytes(resp.Body, "content.video_url").Exists() {
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

	var result aigc.GenerationPollResult
	switch state {
	case "succeeded", "success", "succeed", "completed", "done":
		var artifacts []aigc.ContentGenerationArtifact
		if videoURL != "" {
			artifacts = append(artifacts, driver.CreateArtifact(
				"output_video", a.artifactProvider, videoURL, "video/mp4", 0, nil,
			))
		}
		if lastFrameURL != "" {
			artifacts = append(artifacts, driver.CreateArtifact(
				"last_frame", a.artifactProvider, lastFrameURL, "image/png", 0, nil,
			))
		}
		result = driver.BuildCompletedPollResult(artifacts)

	case "failed", "error", "expired":
		result = driver.BuildFailedPollResultFromRaw(a.Name(), 200, resp.Body, nil)

	case "canceled", "cancelled":
		result = driver.BuildCanceledPollResult()

	case "queued", "pending":
		result = driver.BuildRunningPollResult(10, aigc.StageProviderRunning)

	default:
		result = driver.BuildRunningPollResult(50, aigc.StageProviderRunning)
	}
	return withSeedanceUsage(result, resp.Body), nil
}

func withSeedanceUsage(result aigc.GenerationPollResult, body []byte) aigc.GenerationPollResult {
	if usage := ojson.GetBytes(body, "usage"); usage.IsObject() {
		if result.Metadata == nil {
			result.Metadata = make(map[string]any)
		}
		result.Metadata["seedance"] = map[string]any{"usage": usage.Value()}
	}
	return result
}

// PrepareCancel creates cancellation request if supported.
func (a *SeedanceAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	if !a.supportsCancel {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("%s: task cancellation is not supported", a.Name())
	}
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/contents/generations/tasks/%s/cancel", taskID),
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel checks cancellation support.
func (a *SeedanceAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	if !a.supportsCancel {
		return fmt.Errorf("%s: task cancellation is not supported", a.Name())
	}
	return nil
}

type seedanceRef struct {
	Kind string
	URL  string
	Role string
}

func seedanceResponseFailed(body []byte) bool {
	if errNode := ojson.GetBytes(body, "error"); errNode.Exists() && errNode.Type != ojson.Null {
		return true
	}
	code := ojson.GetBytes(body, "code")
	return code.Exists() && code.Type != ojson.Null && code.String() != "" && code.String() != "0"
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

	processNode := func(node ojson.Result) {
		if node.Type == ojson.String {
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
		refNode := ojson.GetBytes(body, path)
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
		refsNode := ojson.GetBytes(body, path)
		if !refsNode.Exists() {
			continue
		}
		if refsNode.IsArray() {
			for _, item := range refsNode.Array() {
				processNode(item)
			}
		} else if refsNode.Type == ojson.String {
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
	contentArray := ojson.GetBytes(body, "content")
	if !contentArray.Exists() {
		contentArray = ojson.GetBytes(body, "extra_body.content")
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
	if prompt := driver.ExtractPrompt(body); prompt != "" {
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
			default:
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
		if val := strings.TrimSpace(ojson.GetBytes(body, path).String()); val != "" {
			return val
		}
	}
	return ""
}

func firstInt(body []byte, paths ...string) int64 {
	for _, path := range paths {
		if node := ojson.GetBytes(body, path); node.Exists() {
			if node.Type == ojson.Number {
				return node.Int()
			}
			if node.Type == ojson.String {
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
		if node := ojson.GetBytes(body, path); node.Exists() {
			return node.Bool(), true
		}
	}
	return false, false
}

func firstValue(body []byte, paths ...string) any {
	for _, path := range paths {
		if node := ojson.GetBytes(body, path); node.Exists() {
			return node.Value()
		}
	}
	return nil
}

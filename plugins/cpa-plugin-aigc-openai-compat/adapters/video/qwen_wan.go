package video

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

// QwenWanAdapter handles Alibaba DashScope Wan 3.0 (and Wan series) video generation tasks.
type QwenWanAdapter struct {
	cfgFunc func() (models []string, endpoint string, promptExtend bool)
}

// NewQwenWanAdapter creates a new QwenWanAdapter instance.
func NewQwenWanAdapter(cfgFunc func() (models []string, endpoint string, promptExtend bool)) *QwenWanAdapter {
	return &QwenWanAdapter{cfgFunc: cfgFunc}
}

// Name returns the provider name.
func (a *QwenWanAdapter) Name() string {
	return "qwen-wan"
}

// Supports checks if this adapter supports the video model.
func (a *QwenWanAdapter) Supports(ctx context.Context, req aigc.GenerationSupportRequest) bool {
	if req.Kind != aigc.ContentKindVideo {
		return false
	}
	models, _, _ := a.cfgFunc()
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

// PrepareSubmit builds the DashScope Wan video generation request.
func (a *QwenWanAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	model := strings.TrimSpace(gen.Model)
	for _, prefix := range []string{"alibaba-cn/", "alibaba/", "qwen/", "dashscope/"} {
		if strings.HasPrefix(strings.ToLower(model), prefix) {
			model = model[len(prefix):]
			break
		}
	}

	prompt := utils.ExtractPrompt(inputBytes)
	media := buildWanMediaItems(inputBytes)

	if prompt == "" && len(media) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or media is required")
	}

	// Validate mutual exclusivity between reference_xx/file/link and first_frame/last_frame
	hasFrame := false
	hasReference := false
	for _, m := range media {
		mType, _ := m["type"].(string)
		switch mType {
		case "first_frame", "last_frame":
			hasFrame = true
		case "reference_image", "reference_video", "reference_audio", "file", "link":
			hasReference = true
		}
	}
	if hasFrame && hasReference {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("reference media and first_frame/last_frame are mutually exclusive")
	}

	inputPayload := make(map[string]any)
	if prompt != "" {
		inputPayload["prompt"] = prompt
	}
	if len(media) > 0 {
		inputPayload["media"] = media
	}

	parameters := a.buildWanParameters(gen, inputBytes)

	payload := map[string]any{
		"model":      model,
		"input":      inputPayload,
		"parameters": parameters,
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal qwen-wan submit payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("X-DashScope-Async", "enable")

	_, endpoint, _ := a.cfgFunc()
	if endpoint == "" {
		endpoint = "https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis"
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      endpoint,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "qwen-wan"},
	}, nil
}

func (a *QwenWanAdapter) buildWanParameters(gen aigc.ContentGeneration, inputBytes []byte) map[string]any {
	canonical := utils.ExtractCanonical(gen)
	params := make(map[string]any)

	// 1. Resolution: Wan 3.0 uses uppercase "480P", "720P", "1080P"
	rawRes := firstString(inputBytes, "resolution", "extra_body.resolution")
	if rawRes == "" {
		rawRes = canonical.ResolutionTier
	}
	resUpper := normalizeWanResolution(rawRes)
	if resUpper != "" {
		params["resolution"] = resUpper
	} else {
		params["resolution"] = "1080P"
	}

	// 2. Ratio: "adaptive", "16:9", "4:3", "1:1", "3:4", "9:16"
	rawRatio := firstString(inputBytes, "ratio", "extra_body.ratio")
	if rawRatio == "" {
		rawRatio = canonical.Ratio
	}
	if rawRatio != "" {
		params["ratio"] = normalizeWanRatio(rawRatio)
	} else {
		params["ratio"] = "adaptive"
	}

	// 3. Duration: [2, 30] or -1
	duration := firstInt(inputBytes, "duration", "extra_body.duration", "seconds", "extra_body.seconds")
	if duration <= 0 && duration != -1 {
		duration = canonical.DurationSec
	}
	if duration > 0 || duration == -1 {
		params["duration"] = duration
	} else {
		params["duration"] = 5
	}

	// 4. Audio: boolean (default true)
	if audio, ok := firstBool(inputBytes, "audio", "extra_body.audio", "generate_audio", "extra_body.generate_audio"); ok {
		params["audio"] = audio
	} else if canonical.GenerateAudio {
		params["audio"] = true
	} else {
		params["audio"] = true
	}

	// 5. Prompt Extend: boolean
	_, _, defaultPE := a.cfgFunc()
	if pe, ok := firstBool(inputBytes, "prompt_extend", "extra_body.prompt_extend"); ok {
		params["prompt_extend"] = pe
	} else {
		params["prompt_extend"] = defaultPE
	}

	// 6. Watermark: boolean (default false)
	if wm, ok := firstBool(inputBytes, "watermark", "extra_body.watermark"); ok {
		params["watermark"] = wm
	}

	// 7. Seed: integer [0, 2147483647]
	if seed := firstInt(inputBytes, "seed", "extra_body.seed"); seed > 0 {
		params["seed"] = seed
	}

	return params
}

// ParseSubmit processes the response from DashScope video task creation.
func (a *QwenWanAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedSubmitResultFromRaw("qwen-wan", resp.StatusCode, resp.Body, nil), nil
	}

	taskID := gjson.GetBytes(resp.Body, "output.task_id").String()
	if taskID == "" {
		taskID = firstString(resp.Body, "task_id", "id")
	}
	if taskID == "" {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("missing task_id in qwen-wan submit response")
	}

	taskStatus := strings.ToUpper(gjson.GetBytes(resp.Body, "output.task_status").String())
	switch taskStatus {
	case "SUCCEEDED":
		videoURL := firstString(resp.Body, "output.video_url", "output.video_url.url", "video_url")
		if videoURL != "" {
			artifact := utils.CreateArtifact("output_video", "dashscope", videoURL, "video/mp4", 0, nil)
			return utils.BuildCompletedSubmitResult([]aigc.ContentGenerationArtifact{artifact}, map[string]any{"provider": "qwen-wan", "task_id": taskID}), nil
		}
		return utils.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "qwen-wan", "task_id": taskID}), nil
	case "FAILED":
		return utils.BuildFailedSubmitResultFromRaw("qwen-wan", 200, resp.Body, nil), nil
	default:
		return utils.BuildAsyncSubmitResult(taskID, map[string]any{"provider": "qwen-wan", "task_id": taskID}), nil
	}
}

// PreparePoll creates the HTTP polling request for DashScope task status.
func (a *QwenWanAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
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
		Metadata: map[string]any{"provider": "qwen-wan", "task_id": taskID},
	}, nil
}

// ParsePoll processes the status polling response from DashScope.
func (a *QwenWanAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return utils.BuildFailedPollResultFromRaw("qwen-wan", resp.StatusCode, resp.Body, nil), nil
	}

	if !gjson.GetBytes(resp.Body, "output").Exists() && !gjson.GetBytes(resp.Body, "request_id").Exists() {
		return aigc.GenerationPollResult{}, fmt.Errorf("not a dashscope poll response")
	}

	taskStatus := strings.ToUpper(gjson.GetBytes(resp.Body, "output.task_status").String())
	switch taskStatus {
	case "SUCCEEDED", "SUCCESS", "SUCCEED", "COMPLETED", "DONE":
		videoURL := firstString(resp.Body, "output.video_url", "output.video_url.url", "video_url", "download_url")
		if videoURL == "" {
			return utils.BuildFailedPollResultFromRaw("qwen-wan", 200, resp.Body, nil), nil
		}

		var meta map[string]any
		if usageNode := gjson.GetBytes(resp.Body, "usage"); usageNode.Exists() {
			meta = make(map[string]any)
			if d := usageNode.Get("duration").Int(); d > 0 {
				meta["duration"] = d
			}
			if fps := usageNode.Get("fps").Int(); fps > 0 {
				meta["fps"] = fps
			}
			if sr := usageNode.Get("SR").Int(); sr > 0 {
				meta["resolution"] = sr
			}
			if ratio := usageNode.Get("ratio").String(); ratio != "" {
				meta["ratio"] = ratio
			}
		}

		artifact := utils.CreateArtifact("output_video", "dashscope", videoURL, "video/mp4", 0, meta)
		return utils.BuildCompletedPollResult([]aigc.ContentGenerationArtifact{artifact}), nil

	case "FAILED", "ERROR":
		return utils.BuildFailedPollResultFromRaw("qwen-wan", 200, resp.Body, nil), nil

	case "CANCELED", "CANCELLED":
		return utils.BuildCanceledPollResult(), nil

	case "PENDING", "QUEUED":
		return utils.BuildRunningPollResult(10, aigc.StageProviderRunning), nil

	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		return utils.BuildRunningPollResult(50, aigc.StageProviderRunning), nil

	case "UNKNOWN":
		return utils.BuildFailedPollResult("task_expired", "task query expired or unknown"), nil

	default:
		return aigc.GenerationPollResult{}, fmt.Errorf("unrecognized dashscope wan task status: %s", taskStatus)
	}
}

// PrepareCancel creates the cancel request for DashScope.
func (a *QwenWanAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
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

// ParseCancel processes the cancel response.
func (a *QwenWanAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

func normalizeWanResolution(res string) string {
	resLower := strings.ToLower(strings.TrimSpace(res))
	switch resLower {
	case "480p", "sd", "0.5k", "360p":
		return "480P"
	case "720p", "hd", "1k":
		return "720P"
	case "1080p", "fhd", "1.5k", "2k", "2.5k", "1440p", "4k", "2160p", "uhd":
		return "1080P"
	default:
		if strings.HasSuffix(resLower, "p") {
			return strings.ToUpper(resLower)
		}
		return "1080P"
	}
}

func normalizeWanRatio(ratio string) string {
	rTrim := strings.TrimSpace(ratio)
	switch rTrim {
	case "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive":
		return rTrim
	default:
		return "adaptive"
	}
}

func buildWanMediaItems(body []byte) []map[string]any {
	// 1. Check explicit "media" array
	mediaArray := gjson.GetBytes(body, "media")
	if !mediaArray.Exists() {
		mediaArray = gjson.GetBytes(body, "extra_body.media")
	}
	if !mediaArray.Exists() {
		mediaArray = gjson.GetBytes(body, "input.media")
	}
	if mediaArray.IsArray() && len(mediaArray.Array()) > 0 {
		var items []map[string]any
		for _, node := range mediaArray.Array() {
			itemType := strings.TrimSpace(node.Get("type").String())
			itemURL := strings.TrimSpace(node.Get("url").String())
			if itemURL == "" {
				itemURL = strings.TrimSpace(node.Get("image_url.url").String())
			}
			if itemURL == "" {
				itemURL = strings.TrimSpace(node.Get("video_url.url").String())
			}
			if itemURL == "" {
				itemURL = strings.TrimSpace(node.Get("audio_url.url").String())
			}
			if itemType != "" && itemURL != "" {
				items = append(items, map[string]any{
					"type": itemType,
					"url":  itemURL,
				})
			}
		}
		if len(items) > 0 {
			return items
		}
	}

	// 2. Check explicit first_frame and last_frame
	firstFrame := firstString(body, "first_frame", "extra_body.first_frame", "input.first_frame")
	lastFrame := firstString(body, "last_frame", "extra_body.last_frame", "input.last_frame")

	if firstFrame != "" || lastFrame != "" {
		var items []map[string]any
		if firstFrame != "" {
			items = append(items, map[string]any{
				"type": "first_frame",
				"url":  firstFrame,
			})
		}
		if lastFrame != "" {
			items = append(items, map[string]any{
				"type": "last_frame",
				"url":  lastFrame,
			})
		}
		return items
	}

	// 3. Check multimodal content array or input_reference references
	references := extractWanReferences(body)
	if len(references) > 0 {
		// If there is only 1 image and it was passed via image/first_frame without multi-ref intent,
		// map to first_frame. If multiple references or video/audio/file/link, use reference_xx/file/link.
		if len(references) == 1 && references[0].ExplicitRole == "first_frame" {
			return []map[string]any{
				{
					"type": "first_frame",
					"url":  references[0].URL,
				},
			}
		}

		var items []map[string]any
		for _, ref := range references {
			items = append(items, map[string]any{
				"type": ref.WanType,
				"url":  ref.URL,
			})
		}
		return items
	}

	// 4. Single image fallback (e.g. image, image_url)
	if singleImg := firstString(body, "image", "image_url", "extra_body.image", "extra_body.image_url"); singleImg != "" {
		return []map[string]any{
			{
				"type": "first_frame",
				"url":  singleImg,
			},
		}
	}

	return nil
}

type wanRef struct {
	WanType      string
	URL          string
	ExplicitRole string
}

func extractWanReferences(body []byte) []wanRef {
	var refs []wanRef

	addRef := func(rawURL, explicitType, explicitRole string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		wanType := explicitType
		if wanType == "" {
			switch strings.ToLower(explicitRole) {
			case "first_frame":
				wanType = "first_frame"
			case "last_frame":
				wanType = "last_frame"
			case "reference_video", "video":
				wanType = "reference_video"
			case "reference_audio", "audio":
				wanType = "reference_audio"
			case "file":
				wanType = "file"
			case "link":
				wanType = "link"
			default:
				wanType = detectWanMediaType(rawURL)
			}
		}
		refs = append(refs, wanRef{
			WanType:      wanType,
			URL:          rawURL,
			ExplicitRole: explicitRole,
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
			addRef(u, "reference_video", role)
			return
		} else if u := strings.TrimSpace(node.Get("video_url").String()); u != "" {
			addRef(u, "reference_video", role)
			return
		}

		if u := strings.TrimSpace(node.Get("audio_url.url").String()); u != "" {
			addRef(u, "reference_audio", role)
			return
		} else if u := strings.TrimSpace(node.Get("audio_url").String()); u != "" {
			addRef(u, "reference_audio", role)
			return
		}

		if u := strings.TrimSpace(node.Get("image_url.url").String()); u != "" {
			explicitKind := ""
			if role == "first_frame" || role == "last_frame" {
				explicitKind = role
			} else {
				explicitKind = "reference_image"
			}
			addRef(u, explicitKind, role)
			return
		} else if u := strings.TrimSpace(node.Get("image_url").String()); u != "" {
			explicitKind := ""
			if role == "first_frame" || role == "last_frame" {
				explicitKind = role
			} else {
				explicitKind = "reference_image"
			}
			addRef(u, explicitKind, role)
			return
		}

		if u := strings.TrimSpace(node.Get("url").String()); u != "" {
			explicitKind := ""
			switch nodeType {
			case "first_frame", "last_frame", "reference_image", "reference_video", "reference_audio", "file", "link":
				explicitKind = nodeType
			case "video", "video_url":
				explicitKind = "reference_video"
			case "audio", "audio_url":
				explicitKind = "reference_audio"
			case "image", "image_url":
				if role == "first_frame" || role == "last_frame" {
					explicitKind = role
				} else {
					explicitKind = "reference_image"
				}
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
					addRef(part, "reference_image", "reference_image")
				}
			} else {
				addRef(val, "reference_image", "reference_image")
			}
		}
	}

	// Content array support
	contentArray := gjson.GetBytes(body, "content")
	if !contentArray.Exists() {
		contentArray = gjson.GetBytes(body, "extra_body.content")
	}
	if contentArray.IsArray() {
		for _, node := range contentArray.Array() {
			itemType := strings.TrimSpace(node.Get("type").String())
			if itemType != "text" {
				processNode(node)
			}
		}
	}

	return refs
}

func detectWanMediaType(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if strings.HasPrefix(lower, "data:video/") || strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mov") || strings.HasSuffix(lower, ".webm") || strings.HasSuffix(lower, ".mkv") {
		return "reference_video"
	}
	if strings.HasPrefix(lower, "data:audio/") || strings.HasSuffix(lower, ".mp3") || strings.HasSuffix(lower, ".wav") || strings.HasSuffix(lower, ".aac") || strings.HasSuffix(lower, ".m4a") || strings.HasSuffix(lower, ".ogg") || strings.HasSuffix(lower, ".flac") {
		return "reference_audio"
	}
	if strings.HasSuffix(lower, ".pptx") || strings.HasSuffix(lower, ".ppt") || strings.HasSuffix(lower, ".pdf") || strings.HasSuffix(lower, ".docx") || strings.HasSuffix(lower, ".doc") || strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xls") || strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".csv") {
		return "file"
	}
	return "reference_image"
}

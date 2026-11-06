// Package lk888 provides adapter for LK888 MiniMax-H3 video generation.
package lk888

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

const (
	LK888GeneratePath     = "/media/generate"
	LK888StatusPath       = "/media/status"
	LK888DefaultModel     = "minimax-h3"
	LK888AdapterName      = "lk888-minimax-h3"
	LK888ArtifactProvider = "lk"
)

// LK888MiniMaxH3Adapter adapts LK888 media API for MiniMax-H3.
type LK888MiniMaxH3Adapter struct{}

// NewLK888MiniMaxH3Adapter creates an LK888 H3 adapter.
func NewLK888MiniMaxH3Adapter() *LK888MiniMaxH3Adapter {
	return &LK888MiniMaxH3Adapter{}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "lk888-minimax-h3",
		New:  func() driver.Adapter { return NewLK888MiniMaxH3Adapter() },
	})
}

// Name returns provider identifier.
func (a *LK888MiniMaxH3Adapter) Name() string {
	return LK888AdapterName
}

// Kind declares video content kind.
func (a *LK888MiniMaxH3Adapter) Kind() aigc.ContentKind {
	return aigc.ContentKindVideo
}

// Prefixes returns model prefixes for LK888 H3 models.
func (a *LK888MiniMaxH3Adapter) Prefixes() []string {
	return []string{
		"lk/minimax-h3",
		"lk888/minimax-h3",
	}
}

// PrepareSubmit builds /media/generate POST request.
func (a *LK888MiniMaxH3Adapter) PrepareSubmit(_ context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	raw := input.Generation.Input
	if !ojson.ValidBytes(raw) {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("invalid JSON input")
	}

	prompt := lk888Value(raw, "prompt", "extra_body.prompt")
	if prompt.Exists() && prompt.Type != ojson.String {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt must be a string")
	}
	promptText := strings.TrimSpace(prompt.String())
	if promptText == "" {
		promptText = strings.TrimSpace(driver.ExtractPrompt(raw))
	}
	if promptText == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt is required")
	}

	durationVal := lk888Value(raw, "seconds", "extra_body.duration", "extra_body.seconds", "extra_body.params.duration")
	if !durationVal.Exists() {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("duration is required (integer between 4 and 15 seconds)")
	}
	var durationInt int64
	switch durationVal.Type {
	case ojson.Number:
		if durationVal.Float() != math.Trunc(durationVal.Float()) {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("duration must be an integer between 4 and 15")
		}
		durationInt = durationVal.Int()
	case ojson.String:
		parsed, err := strconv.ParseInt(durationVal.String(), 10, 64)
		if err != nil {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("duration must be an integer between 4 and 15")
		}
		durationInt = parsed
	default:
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("duration must be an integer between 4 and 15")
	}
	if durationInt < 4 || durationInt > 15 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("duration must be between 4 and 15 seconds")
	}

	resolution, aspectRatio, err := lk888Dimensions(raw)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	if resolution == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("resolution or size is required for LK888 MiniMax H3")
	}

	mode, images, imageURLs, videoURLs, audioURLs, err := lk888ExtractMedia(raw)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}

	params := map[string]any{
		"duration":   fmt.Sprintf("%d", durationInt),
		"resolution": resolution,
	}
	if mode != "" {
		params["mode"] = mode
	}
	if len(images) > 0 {
		params["images"] = images
	}
	if len(imageURLs) > 0 {
		params["image_url"] = imageURLs
	}
	if len(videoURLs) > 0 {
		params["video_url"] = videoURLs
	}
	if len(audioURLs) > 0 {
		params["audio_url"] = audioURLs
	}
	if aspectRatio != "" {
		params["aspect_ratio"] = aspectRatio
	}

	upstreamModel := LK888DefaultModel
	if customModel := lk888Value(raw, "extra_body.model"); customModel.Exists() && strings.TrimSpace(customModel.String()) != "" {
		upstreamModel = customModel.String()
	}

	payload := map[string]any{
		"model":  upstreamModel,
		"prompt": promptText,
		"params": params,
	}

	if notify := lk888Value(raw, "extra_body.notify_url", "extra_body.callback_url"); notify.Exists() && notify.Type == ojson.String && strings.TrimSpace(notify.String()) != "" {
		payload["notify_url"] = notify.String()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}

	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    LK888GeneratePath,
		Model:  input.Generation.Model,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: body,
		Metadata: map[string]any{
			"provider":      a.Name(),
			"poll_backoff":  true,
			"upstream_mode": mode,
		},
	}, nil
}

// ParseSubmit parses asynchronous task ID from submit response.
func (a *LK888MiniMaxH3Adapter) ParseSubmit(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || lk888ResponseFailed(resp.Body) {
		return driver.BuildFailedSubmitResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), nil
	}

	taskIDNode := ojson.GetBytes(resp.Body, "data.task_id")
	if !taskIDNode.Exists() && ojson.GetBytes(resp.Body, "code").Exists() {
		taskIDNode = ojson.GetBytes(resp.Body, "task_id")
	}
	taskID := strings.TrimSpace(taskIDNode.String())
	if taskID == "" {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("LK888 submit response missing task_id")
	}

	result := driver.BuildAsyncSubmitResult(taskID, map[string]any{
		"provider": a.Name(),
	})
	result.ProviderResponse = append(json.RawMessage(nil), resp.Body...)
	return result, nil
}

// PreparePoll prepares status polling GET request.
func (a *LK888MiniMaxH3Adapter) PreparePoll(_ context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := strings.TrimSpace(input.Generation.ProviderTaskID)
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("LK888 requires a task_id for polling")
	}
	endpoint := fmt.Sprintf("%s?task_id=%s", LK888StatusPath, url.QueryEscape(taskID))
	return aigc.GenerationExecutionRequest{
		Method: http.MethodGet,
		URL:    endpoint,
		Model:  input.Generation.Model,
		Metadata: map[string]any{
			"provider":                   a.Name(),
			"task_id":                    taskID,
			"fail_on_query_client_error": true,
		},
	}, nil
}

// ParsePoll converts LK888 status response into unified poll result.
func (a *LK888MiniMaxH3Adapter) ParsePoll(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return aigc.GenerationPollResult{}, fmt.Errorf("LK888 query temporarily unavailable (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || lk888ResponseFailed(resp.Body) {
		return driver.BuildFailedPollResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), nil
	}

	taskIDNode := ojson.GetBytes(resp.Body, "task_id")
	stateNode := ojson.GetBytes(resp.Body, "state")
	if !taskIDNode.Exists() || !stateNode.Exists() {
		return aigc.GenerationPollResult{}, fmt.Errorf("invalid LK888 task response: missing task_id or state")
	}

	if expectedID, ok := resp.Metadata["task_id"].(string); ok && expectedID != "" && strings.TrimSpace(taskIDNode.String()) != expectedID {
		return aigc.GenerationPollResult{}, fmt.Errorf("LK888 task ID mismatch")
	}

	state := strings.ToLower(strings.TrimSpace(stateNode.String()))
	isFinal := ojson.GetBytes(resp.Body, "is_final").Bool()

	progress := 10
	if pNode := ojson.GetBytes(resp.Body, "progress"); pNode.Exists() {
		pStr := strings.TrimSpace(strings.TrimSuffix(pNode.String(), "%"))
		if pInt, err := strconv.Atoi(pStr); err == nil && pInt >= 0 && pInt <= 100 {
			progress = pInt
		}
	}

	meta := map[string]any{}
	if cost := ojson.GetBytes(resp.Body, "cost"); cost.Exists() {
		meta["cost"] = cost.Value()
	}
	if resType := ojson.GetBytes(resp.Body, "result_type"); resType.Exists() {
		meta["result_type"] = resType.String()
	}

	var result aigc.GenerationPollResult
	switch state {
	case "pending":
		if progress <= 0 {
			progress = 10
		}
		result = driver.BuildRunningPollResult(progress, aigc.StageProviderRunning)
	case "running":
		if progress <= 0 {
			progress = 50
		}
		result = driver.BuildRunningPollResult(progress, aigc.StageProviderRunning)
	case "success":
		resultURL := strings.TrimSpace(ojson.GetBytes(resp.Body, "result_url").String())
		if resultURL == "" {
			return aigc.GenerationPollResult{}, fmt.Errorf("LK888 succeeded task is missing result_url")
		}
		artifact := driver.CreateArtifact("output_video", LK888ArtifactProvider, resultURL, "video/mp4", 0, meta)
		result = driver.BuildCompletedPollResult([]aigc.ContentGenerationArtifact{artifact})
	case "failed":
		errMsg := strings.TrimSpace(ojson.GetBytes(resp.Body, "error").String())
		if errMsg == "" {
			errMsg = "LK888 task failed with no error details"
		}
		result = driver.BuildFailedPollResult("lk888_task_failed", errMsg)
	default:
		if isFinal {
			resultURL := strings.TrimSpace(ojson.GetBytes(resp.Body, "result_url").String())
			if resultURL != "" {
				artifact := driver.CreateArtifact("output_video", LK888ArtifactProvider, resultURL, "video/mp4", 0, meta)
				result = driver.BuildCompletedPollResult([]aigc.ContentGenerationArtifact{artifact})
			} else {
				errMsg := strings.TrimSpace(ojson.GetBytes(resp.Body, "error").String())
				if errMsg == "" {
					errMsg = fmt.Sprintf("LK888 task ended in state %q", state)
				}
				result = driver.BuildFailedPollResult("lk888_task_failed", errMsg)
			}
		} else {
			result = driver.BuildRunningPollResult(progress, aigc.StageProviderRunning)
		}
	}

	result.ProviderResponse = append(json.RawMessage(nil), resp.Body...)
	result.Metadata = meta
	return result, nil
}

// PrepareCancel rejects cancellation.
func (a *LK888MiniMaxH3Adapter) PrepareCancel(_ context.Context, _ aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, fmt.Errorf("%s: task cancellation is not supported", a.Name())
}

// ParseCancel rejects cancellation.
func (a *LK888MiniMaxH3Adapter) ParseCancel(_ context.Context, _ aigc.GenerationExecutionResponse) error {
	return fmt.Errorf("%s: task cancellation is not supported", a.Name())
}

func lk888Dimensions(raw []byte) (resolution, aspectRatio string, err error) {
	res := lk888Value(raw, "extra_body.resolution", "extra_body.params.resolution")
	if res.Exists() {
		if res.Type != ojson.String || !lk888ValidResolution(res.String()) {
			return "", "", fmt.Errorf("resolution must be 768P, 1080P, 2K, or 4K")
		}
		resolution = res.String()
	}

	r := lk888Value(raw, "extra_body.aspect_ratio", "extra_body.ratio", "extra_body.params.aspect_ratio")
	if r.Exists() {
		if r.Type != ojson.String || !lk888ValidRatio(r.String()) {
			return "", "", fmt.Errorf("invalid aspect_ratio; choose adaptive, 16:9, 9:16, 1:1, 4:3, 3:4, or 21:9")
		}
		aspectRatio = r.String()
	}

	if size := lk888Value(raw, "size", "extra_body.size"); size.Exists() && (resolution == "" || aspectRatio == "") {
		width, height, ok := driver.ParseDimensions(size.String())
		if size.Type != ojson.String || !ok {
			return "", "", fmt.Errorf("size must be positive WIDTHxHEIGHT dimensions")
		}
		if resolution == "" {
			pixels := float64(width) * float64(height)
			switch {
			case pixels <= 1_400_000:
				resolution = "768P"
			case pixels <= 2_500_000:
				resolution = "1080P"
			case pixels <= 5_500_000:
				resolution = "2K"
			case pixels <= 10_000_000:
				resolution = "4K"
			default:
				return "", "", fmt.Errorf("size exceeds supported 4K pixel tier")
			}
		}
		if aspectRatio == "" {
			aspectRatio = driver.RatioFromSize(size.String())
		}
	}

	return resolution, aspectRatio, nil
}

func lk888ValidResolution(r string) bool {
	switch r {
	case "768P", "1080P", "2K", "4K":
		return true
	}
	return false
}

func lk888ValidRatio(r string) bool {
	switch r {
	case "adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9":
		return true
	}
	return false
}

func lk888ExtractMedia(raw []byte) (mode string, images, imageURLs, videoURLs, audioURLs []string, err error) {
	if m := lk888Value(raw, "extra_body.mode", "extra_body.params.mode"); m.Exists() {
		if m.Type != ojson.String || (m.String() != "shouweizhen" && m.String() != "cankaosheng") {
			return "", nil, nil, nil, nil, fmt.Errorf("mode must be shouweizhen or cankaosheng")
		}
		mode = m.String()
	}

	if nativeImages := lk888Value(raw, "extra_body.images", "extra_body.params.images"); nativeImages.Exists() && nativeImages.IsArray() {
		for _, item := range nativeImages.Array() {
			if item.Type == ojson.String && strings.TrimSpace(item.String()) != "" {
				images = append(images, strings.TrimSpace(item.String()))
			}
		}
	}
	if nativeImageURLs := lk888Value(raw, "extra_body.image_url", "extra_body.params.image_url"); nativeImageURLs.Exists() && nativeImageURLs.IsArray() {
		for _, item := range nativeImageURLs.Array() {
			if item.Type == ojson.String && strings.TrimSpace(item.String()) != "" {
				imageURLs = append(imageURLs, strings.TrimSpace(item.String()))
			}
		}
	}
	if nativeVideoURLs := lk888Value(raw, "extra_body.video_url", "extra_body.params.video_url"); nativeVideoURLs.Exists() && nativeVideoURLs.IsArray() {
		for _, item := range nativeVideoURLs.Array() {
			if item.Type == ojson.String && strings.TrimSpace(item.String()) != "" {
				videoURLs = append(videoURLs, strings.TrimSpace(item.String()))
			}
		}
	}
	if nativeAudioURLs := lk888Value(raw, "extra_body.audio_url", "extra_body.params.audio_url"); nativeAudioURLs.Exists() && nativeAudioURLs.IsArray() {
		for _, item := range nativeAudioURLs.Array() {
			if item.Type == ojson.String && strings.TrimSpace(item.String()) != "" {
				audioURLs = append(audioURLs, strings.TrimSpace(item.String()))
			}
		}
	}

	firstFrame := lk888Value(raw, "extra_body.first_frame")
	lastFrame := lk888Value(raw, "extra_body.last_frame")
	if firstFrame.Exists() || lastFrame.Exists() {
		if lastFrame.Exists() && !firstFrame.Exists() {
			return "", nil, nil, nil, nil, fmt.Errorf("LK888 H3 requires first_frame when last_frame is provided")
		}
		if firstFrame.Exists() {
			if firstFrame.Type != ojson.String || strings.TrimSpace(firstFrame.String()) == "" {
				return "", nil, nil, nil, nil, fmt.Errorf("first_frame must be a URL string")
			}
			images = append(images, strings.TrimSpace(firstFrame.String()))
		}
		if lastFrame.Exists() {
			if lastFrame.Type != ojson.String || strings.TrimSpace(lastFrame.String()) == "" {
				return "", nil, nil, nil, nil, fmt.Errorf("last_frame must be a URL string")
			}
			images = append(images, strings.TrimSpace(lastFrame.String()))
		}
	}

	refs := lk888Value(raw, "input_reference", "extra_body.input_reference")
	var refNodes []ojson.Result
	if refs.Exists() {
		if refs.IsArray() {
			refNodes = refs.Array()
		} else {
			refNodes = append(refNodes, refs)
		}
	}

	for _, node := range refNodes {
		kind, role, uri := "", "", ""
		if node.Type == ojson.String {
			uri = strings.TrimSpace(node.String())
			parsed, _ := url.Parse(uri)
			mediaPath := uri
			if parsed != nil && parsed.Scheme != "data" {
				mediaPath = parsed.Path
			}
			kind = detectLK888MediaKind(mediaPath)
		} else if node.IsObject() {
			kind = node.Get("type").String()
			role = node.Get("role").String()
			for _, k := range []string{"image_url", "video_url", "audio_url"} {
				v := node.Get(k)
				if !v.Exists() {
					continue
				}
				kind = strings.TrimSuffix(k, "_url")
				if v.Type == ojson.String {
					uri = strings.TrimSpace(v.String())
				} else {
					uri = strings.TrimSpace(v.Get("url").String())
				}
				break
			}
			if uri == "" {
				uri = strings.TrimSpace(node.Get("url").String())
			}
		} else {
			return "", nil, nil, nil, nil, fmt.Errorf("invalid input_reference")
		}

		if uri == "" {
			continue
		}

		switch role {
		case "first_frame":
			if len(images) == 0 {
				images = append(images, uri)
			} else {
				images[0] = uri
			}
		case "last_frame":
			if len(images) == 0 {
				return "", nil, nil, nil, nil, fmt.Errorf("LK888 H3 requires first_frame when last_frame is provided")
			}
			if len(images) == 1 {
				images = append(images, uri)
			} else {
				images[1] = uri
			}
		case "reference_video":
			videoURLs = append(videoURLs, uri)
		case "reference_audio":
			audioURLs = append(audioURLs, uri)
		case "reference_image":
			imageURLs = append(imageURLs, uri)
		default:
			switch kind {
			case "video":
				videoURLs = append(videoURLs, uri)
			case "audio":
				audioURLs = append(audioURLs, uri)
			default:
				imageURLs = append(imageURLs, uri)
			}
		}
	}

	if len(images) > 2 {
		return "", nil, nil, nil, nil, fmt.Errorf("images (首尾帧) allows at most 2 items")
	}
	if len(imageURLs) > 9 {
		return "", nil, nil, nil, nil, fmt.Errorf("image_url allows at most 9 items")
	}
	if len(videoURLs) > 3 {
		return "", nil, nil, nil, nil, fmt.Errorf("video_url allows at most 3 items")
	}
	if len(audioURLs) > 3 {
		return "", nil, nil, nil, nil, fmt.Errorf("audio_url allows at most 3 items")
	}

	hasFrames := len(images) > 0
	hasRefs := len(imageURLs) > 0 || len(videoURLs) > 0 || len(audioURLs) > 0
	if hasFrames && hasRefs {
		return "", nil, nil, nil, nil, fmt.Errorf("首尾帧 (images) and 参考生素材 (image_url/video_url/audio_url) are mutually exclusive")
	}

	if mode == "" {
		if hasFrames {
			mode = "shouweizhen"
		} else if hasRefs {
			mode = "cankaosheng"
		}
	} else {
		if mode == "shouweizhen" && hasRefs {
			return "", nil, nil, nil, nil, fmt.Errorf("mode shouweizhen cannot accept reference media")
		}
		if mode == "cankaosheng" && hasFrames {
			return "", nil, nil, nil, nil, fmt.Errorf("mode cankaosheng cannot accept first/last frame images")
		}
	}

	return mode, images, imageURLs, videoURLs, audioURLs, nil
}

func detectLK888MediaKind(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".mp4"), strings.HasSuffix(lower, ".mov"), strings.HasSuffix(lower, ".avi"), strings.HasSuffix(lower, ".webm"), strings.HasSuffix(lower, ".mkv"):
		return "video"
	case strings.HasSuffix(lower, ".mp3"), strings.HasSuffix(lower, ".wav"), strings.HasSuffix(lower, ".m4a"), strings.HasSuffix(lower, ".aac"), strings.HasSuffix(lower, ".flac"), strings.HasSuffix(lower, ".ogg"):
		return "audio"
	default:
		return "image"
	}
}

func lk888Value(raw []byte, paths ...string) ojson.Result {
	for _, path := range paths {
		if node := ojson.GetBytes(raw, path); node.Exists() {
			return node
		}
	}
	return ojson.Result{}
}

func lk888ResponseFailed(raw []byte) bool {
	if !ojson.ValidBytes(raw) {
		return true
	}
	if code := ojson.GetBytes(raw, "code"); code.Exists() && code.Int() != 200 {
		return true
	}
	if errNode := ojson.GetBytes(raw, "error"); errNode.Exists() && errNode.IsObject() {
		return true
	}
	return false
}

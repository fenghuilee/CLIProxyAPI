package chinadatapay

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// ChinaDataPayBaseURL is the fixed gateway URL for H3 endpoints.
const ChinaDataPayBaseURL = "https://tp-api.chinadatapay.com:8000"

// ChinaDataPayMiniMaxH3Adapter adapts ChinaDataPay MiniMax-H3 video tasks.
type ChinaDataPayMiniMaxH3Adapter struct{}

// NewChinaDataPayMiniMaxH3Adapter creates H3 adapter.
func NewChinaDataPayMiniMaxH3Adapter() *ChinaDataPayMiniMaxH3Adapter {
	return &ChinaDataPayMiniMaxH3Adapter{}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "chinadatapay-minimax-h3",
		New:  func() driver.Adapter { return NewChinaDataPayMiniMaxH3Adapter() },
	})
}

// Name returns provider identifier.
func (a *ChinaDataPayMiniMaxH3Adapter) Name() string { return "chinadatapay-minimax-h3" }

// Kind declares video content kind.
func (a *ChinaDataPayMiniMaxH3Adapter) Kind() aigc.ContentKind {
	return aigc.ContentKindVideo
}

// Prefixes returns model prefixes for ChinaDataPay H3 models.
func (a *ChinaDataPayMiniMaxH3Adapter) Prefixes() []string {
	return []string{
		"cdp/minimax-h3",
		"chinadatapay/minimax-h3",
	}
}

func (a *ChinaDataPayMiniMaxH3Adapter) endpoint(path string) string {
	return ChinaDataPayBaseURL + path
}

func (a *ChinaDataPayMiniMaxH3Adapter) PrepareSubmit(_ context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	raw := input.Generation.Input
	if !ojson.ValidBytes(raw) {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("invalid H3 input JSON")
	}
	if prompt := h3Value(raw, "prompt", "extra_body.prompt"); prompt.Exists() && prompt.Type != ojson.String {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("H3 prompt must be a string")
	}
	taskType := firstString(raw, "extra_body.task_type")
	if taskType == "" {
		taskType = "generation"
	}
	if taskType != "generation" && taskType != "regeneration" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("unsupported H3 task_type; use generation or regeneration")
	}
	resolution, ratio, err := h3Dimensions(raw)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	if (resolution != "768P" && resolution != "2K") || (taskType == "regeneration" && resolution != "2K") {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("H3 resolution must be 768P or 2K; regeneration requires 2K")
	}
	payload := map[string]any{"model": "MiniMax-H3", "resolution": resolution}
	source := h3Value(raw, "extra_body.source_task_id")
	if taskType == "regeneration" && source.Exists() {
		if source.Type != ojson.String || !strings.HasPrefix(source.String(), "task_") {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("source_task_id must be a gateway task ID")
		}
		if h3Value(raw, "extra_body.content", "input_reference", "extra_body.input_reference", "extra_body.first_frame", "extra_body.last_frame").Exists() || strings.TrimSpace(driver.ExtractPrompt(raw)) != "" {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("source_task_id and content must be provided exclusively")
		}
		payload["source_task_id"] = source.String()
	} else {
		if source.Exists() {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("source_task_id is only supported for regeneration")
		}
		content, err := h3Content(raw, taskType == "regeneration")
		if err != nil {
			return aigc.GenerationExecutionRequest{}, err
		}
		payload["content"] = content
	}
	if taskType == "generation" {
		duration := h3Value(raw, "seconds", "extra_body.duration", "extra_body.seconds")
		if duration.Type != ojson.Number || duration.Float() != math.Trunc(duration.Float()) || duration.Float() < 4 || duration.Float() > 15 {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("H3 duration is required and must be an integer between 4 and 15")
		}
		payload["duration"] = duration.Int()
	} else if h3Value(raw, "seconds", "extra_body.duration", "extra_body.seconds").Exists() {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("regeneration does not accept duration or seconds")
	}
	if ratio != "" {
		payload["ratio"] = ratio
	}
	if watermark := h3Value(raw, "extra_body.aigc_watermark"); watermark.Exists() {
		if taskType == "regeneration" {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("regeneration does not accept aigc_watermark")
		}
		if watermark.Type != ojson.True && watermark.Type != ojson.False {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("aigc_watermark must be boolean")
		}
		payload["aigc_watermark"] = watermark.Bool()
	}
	// Ignore user-provided callback_url to prevent hijacking; only inject system-configured callback
	if sysCallback, ok := input.Generation.Metadata["aigc_system_callback_url"].(string); ok && sysCallback != "" {
		payload["callback_url"] = sysCallback
	}
	path := "/v2/video_generation"
	if taskType == "regeneration" {
		path = "/v2/video_regeneration"
	}
	endpoint := a.endpoint(path)
	body, err := json.Marshal(payload)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      endpoint,
		Model:    input.Generation.Model,
		Header:   http.Header{"Content-Type": []string{"application/json"}},
		Body:     body,
		Metadata: map[string]any{"provider": a.Name(), "cancel_requires_confirmation": true, "poll_backoff": true},
	}, nil
}

func h3Dimensions(raw []byte) (resolution, ratio string, err error) {
	res := h3Value(raw, "extra_body.resolution")
	if res.Exists() {
		if res.Type != ojson.String || (res.String() != "768P" && res.String() != "2K") {
			return "", "", fmt.Errorf("H3 resolution must be 768P or 2K; regeneration requires 2K")
		}
		resolution = res.String()
	}
	r := h3Value(raw, "extra_body.ratio")
	if r.Exists() {
		if r.Type != ojson.String || !h3Ratio(r.String()) {
			return "", "", fmt.Errorf("invalid H3 ratio")
		}
		ratio = r.String()
	}
	if size := h3Value(raw, "size", "extra_body.size"); size.Exists() && (!res.Exists() || !r.Exists()) {
		width, height, ok := driver.ParseDimensions(size.String())
		if size.Type != ojson.String || !ok {
			return "", "", fmt.Errorf("H3 size must be positive WIDTHxHEIGHT dimensions")
		}
		if !res.Exists() {
			pixels := float64(width) * float64(height)
			switch {
			case pixels <= 1_400_000:
				resolution = "768P"
			case pixels <= 5_500_000:
				resolution = "2K"
			default:
				return "", "", fmt.Errorf("H3 size exceeds the supported 2K pixel tier")
			}
		}
		if !r.Exists() {
			ratio = driver.RatioFromSize(size.String())
		}
	}
	return resolution, ratio, nil
}

func h3Value(raw []byte, paths ...string) ojson.Result {
	for _, path := range paths {
		if node := ojson.GetBytes(raw, path); node.Exists() {
			return node
		}
	}
	return ojson.Result{}
}

func h3Ratio(ratio string) bool {
	switch ratio {
	case "adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	}
	return false
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

func h3Content(raw []byte, regeneration bool) ([]map[string]any, error) {
	var items []map[string]any
	if native := h3Value(raw, "extra_body.content"); native.Exists() {
		if !native.IsArray() || json.Unmarshal([]byte(native.Raw), &items) != nil {
			return nil, fmt.Errorf("H3 content must be an array of objects")
		}
	} else {
		if prompt := driver.ExtractPrompt(raw); strings.TrimSpace(prompt) != "" {
			items = append(items, map[string]any{"type": "text", "text": prompt})
		}
		refs := h3Value(raw, "input_reference", "extra_body.input_reference")
		nodes := []ojson.Result{}
		if refs.Exists() {
			if refs.IsArray() {
				nodes = refs.Array()
			} else {
				nodes = append(nodes, refs)
			}
		}
		for _, node := range nodes {
			kind, role, uri := "", "", ""
			if node.Type == ojson.String {
				uri = node.String()
				parsed, _ := url.Parse(uri)
				mediaURL := uri
				if parsed != nil && parsed.Scheme != "data" {
					mediaURL = parsed.Path
				}
				kind = detectMediaKind(mediaURL) + "_url"
			} else if node.IsObject() {
				kind = node.Get("type").String()
				role = node.Get("role").String()
				for _, key := range []string{"image_url", "video_url", "audio_url"} {
					value := node.Get(key)
					if !value.Exists() {
						continue
					}
					if kind != "" && kind != key {
						return nil, fmt.Errorf("H3 reference type and media object disagree")
					}
					kind = key
					if value.Type == ojson.String {
						uri = value.String()
					} else {
						uri = value.Get("url").String()
					}
					break
				}
				if uri == "" {
					uri = node.Get("url").String()
				}
			} else {
				return nil, fmt.Errorf("invalid H3 input_reference")
			}
			if role == "" {
				role = "reference_" + strings.TrimSuffix(kind, "_url")
			}
			items = append(items, map[string]any{"type": kind, "role": role, kind: map[string]any{"url": uri}})
		}
		for _, frame := range []string{"first_frame", "last_frame"} {
			if uri := h3Value(raw, "extra_body."+frame); uri.Exists() {
				if uri.Type != ojson.String {
					return nil, fmt.Errorf("%s must be a URL string", frame)
				}
				items = append(items, map[string]any{"type": "image_url", "role": frame, "image_url": map[string]any{"url": uri.String()}})
			}
		}
	}
	hasText, hasFrame, hasReference := false, false, false
	for _, item := range items {
		kind, ok := item["type"].(string)
		if !ok {
			return nil, fmt.Errorf("H3 content type must be a string")
		}
		if kind == "text" {
			text, ok := item["text"].(string)
			if !ok {
				return nil, fmt.Errorf("H3 text must be a string")
			}
			if _, exists := item["role"]; exists {
				return nil, fmt.Errorf("text content must not have a role")
			}
			hasText = hasText || strings.TrimSpace(text) != ""
			continue
		}
		if kind != "image_url" && kind != "video_url" && kind != "audio_url" {
			return nil, fmt.Errorf("unsupported H3 content type %q", kind)
		}
		media, ok := item[kind].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", kind)
		}
		uri, ok := media["url"].(string)
		if !ok || !(strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "mm_file://") || strings.HasPrefix(uri, "data:")) {
			return nil, fmt.Errorf("H3 media requires an HTTP(S), mm_file or data URI")
		}
		role, ok := item["role"].(string)
		if !ok {
			return nil, fmt.Errorf("H3 media role is required")
		}
		switch {
		case kind == "image_url" && (role == "first_frame" || role == "last_frame"):
			hasFrame = true
		case role == "reference_"+strings.TrimSuffix(kind, "_url"):
			hasReference = true
		case kind == "video_url" && role == "base_video" && regeneration:
		default:
			return nil, fmt.Errorf("invalid H3 role %q for %s", role, kind)
		}
	}
	if !hasText {
		return nil, fmt.Errorf("H3 content requires at least one nonempty text item")
	}
	if hasFrame && hasReference {
		return nil, fmt.Errorf("H3 first/last frames and reference media are mutually exclusive")
	}
	return items, nil
}

func (a *ChinaDataPayMiniMaxH3Adapter) ParseSubmit(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || h3ResponseFailed(resp.Body) {
		return driver.BuildFailedSubmitResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), nil
	}
	id := ojson.GetBytes(resp.Body, "task_id")
	if id.Type != ojson.String || !strings.HasPrefix(id.String(), "task_") {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("H3 submit response missing gateway task_id")
	}
	result := driver.BuildAsyncSubmitResult(id.String(), map[string]any{"provider": a.Name()})
	result.ProviderResponse = append(json.RawMessage(nil), resp.Body...)
	return result, nil
}

func (a *ChinaDataPayMiniMaxH3Adapter) taskRequest(gen aigc.ContentGeneration, method, path string) (aigc.GenerationExecutionRequest, error) {
	if !strings.HasPrefix(gen.ProviderTaskID, "task_") {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("H3 requires a gateway task ID")
	}
	endpoint := a.endpoint(path + url.PathEscape(gen.ProviderTaskID))
	return aigc.GenerationExecutionRequest{
		Method:   method,
		URL:      endpoint,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": a.Name(), "task_id": gen.ProviderTaskID},
	}, nil
}

func (a *ChinaDataPayMiniMaxH3Adapter) PreparePoll(_ context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	req, err := a.taskRequest(input.Generation, http.MethodGet, "/v2/query/video_generation/")
	if err == nil {
		req.Metadata["fail_on_query_client_error"] = true
	}
	return req, err
}

func (a *ChinaDataPayMiniMaxH3Adapter) ParsePoll(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return aigc.GenerationPollResult{}, fmt.Errorf("H3 query temporarily unavailable (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || h3ResponseFailed(resp.Body) {
		return driver.BuildFailedPollResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), nil
	}
	task := ojson.GetBytes(resp.Body, "task")
	if !task.IsObject() || task.Get("id").Type != ojson.String || !strings.HasPrefix(task.Get("id").String(), "task_") {
		return aigc.GenerationPollResult{}, fmt.Errorf("invalid H3 task response")
	}
	if expected, ok := resp.Metadata["task_id"].(string); ok && expected != task.Get("id").String() {
		return aigc.GenerationPollResult{}, fmt.Errorf("H3 task ID mismatch")
	}
	if modality := task.Get("modality"); modality.Exists() && modality.String() != "video" {
		return aigc.GenerationPollResult{}, fmt.Errorf("H3 text tasks are not supported by the video adapter")
	}
	var result aigc.GenerationPollResult
	switch task.Get("status").String() {
	case "queued":
		result = driver.BuildRunningPollResult(10, aigc.StageProviderRunning)
	case "running":
		result = driver.BuildRunningPollResult(50, aigc.StageProviderRunning)
	case "cancelled":
		result = driver.BuildCanceledPollResult()
	case "failed":
		code, message := task.Get("error.code").String(), task.Get("error.message").String()
		if code == "" {
			code = "upstream_error_detail_missing"
		}
		if message == "" {
			message = "upstream did not return error detail"
		}
		result = driver.BuildFailedPollResult(code, message)
	case "succeeded":
		uri := task.Get("content.url")
		if uri.Type != ojson.String || strings.TrimSpace(uri.String()) == "" {
			return aigc.GenerationPollResult{}, fmt.Errorf("H3 succeeded task is missing video content.url")
		}
		meta := map[string]any{}
		for _, key := range []string{"usage", "duration", "resolution", "ratio"} {
			if node := task.Get(key); node.Exists() {
				meta[key] = node.Value()
			}
		}
		result = driver.BuildCompletedPollResult([]aigc.ContentGenerationArtifact{driver.CreateArtifact("output_video", "cdp", uri.String(), "video/mp4", 0, meta)})
	default:
		return aigc.GenerationPollResult{}, fmt.Errorf("unknown H3 task status")
	}
	result.ProviderResponse = append(json.RawMessage(nil), resp.Body...)
	result.Metadata = map[string]any{"minimax_h3": task.Value()}
	return result, nil
}

func (a *ChinaDataPayMiniMaxH3Adapter) PrepareCancel(_ context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return a.taskRequest(input.Generation, http.MethodDelete, "/v2/video_generation/")
}

func (a *ChinaDataPayMiniMaxH3Adapter) ParseCancel(_ context.Context, resp aigc.GenerationExecutionResponse) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || h3ResponseFailed(resp.Body) {
		return fmt.Errorf("H3 upstream cancellation failed (HTTP %d)", resp.StatusCode)
	}
	if ojson.GetBytes(resp.Body, "action").String() != "cancelled" || ojson.GetBytes(resp.Body, "status").String() != "cancelled" {
		return fmt.Errorf("H3 upstream did not confirm cancellation")
	}
	id := ojson.GetBytes(resp.Body, "task_id").String()
	if !strings.HasPrefix(id, "task_") {
		return fmt.Errorf("H3 cancel response missing gateway task_id")
	}
	if expected, ok := resp.Metadata["task_id"].(string); ok && expected != id {
		return fmt.Errorf("H3 cancel task ID mismatch")
	}
	return nil
}

func (a *ChinaDataPayMiniMaxH3Adapter) ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return a.ParsePoll(ctx, resp)
}

func h3ResponseFailed(raw []byte) bool {
	return !ojson.ValidBytes(raw) || ojson.GetBytes(raw, "error_code").Exists() || (ojson.GetBytes(raw, "error").Exists() && ojson.GetBytes(raw, "error").Type != ojson.Null)
}

func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if val := strings.TrimSpace(ojson.GetBytes(body, path).String()); val != "" {
			return val
		}
	}
	return ""
}

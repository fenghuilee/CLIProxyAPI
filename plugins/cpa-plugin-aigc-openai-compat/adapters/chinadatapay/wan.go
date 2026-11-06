package chinadatapay

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/qwen"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// ChinaDataPayWanAdapter adapts Wan video generation for ChinaDataPay gateway.
type ChinaDataPayWanAdapter struct {
	submitURL string
	tasksURL  string
}

// NewChinaDataPayWanAdapter creates ChinaDataPay Wan adapter.
func NewChinaDataPayWanAdapter() *ChinaDataPayWanAdapter {
	return &ChinaDataPayWanAdapter{
		submitURL: ChinaDataPayBaseURL + "/api/v1/services/aigc/video-generation/video-synthesis",
		tasksURL:  ChinaDataPayBaseURL + "/api/v1/tasks",
	}
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "chinadatapay-wan",
		New:  func() driver.Adapter { return NewChinaDataPayWanAdapter() },
	})
}

// Name returns provider identifier.
func (a *ChinaDataPayWanAdapter) Name() string {
	return "chinadatapay-wan"
}

// Kind declares video content kind.
func (a *ChinaDataPayWanAdapter) Kind() aigc.ContentKind {
	return aigc.ContentKindVideo
}

// Prefixes returns model prefixes for ChinaDataPay Wan gateway.
func (a *ChinaDataPayWanAdapter) Prefixes() []string {
	return []string{
		"cdp/wan3.0-video",
		"cdp/wan3.0-video-prime",
		"chinadatapay/wan3.0-video",
		"chinadatapay/wan3.0-video-prime",
	}
}

// PrepareSubmit transforms input into Wan gateway POST request.
func (a *ChinaDataPayWanAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	model := driver.UpstreamModel(gen.Model)

	payload, err := buildGatewayWanPayload(gen, model)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}

	bodyBytes, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal chinadatapay-wan submit payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	meta := map[string]any{
		"provider":              a.Name(),
		"poll_interval_seconds": 15,
		"poll_backoff":          true,
		"single_attempt":        true,
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      a.submitURL,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: meta,
	}, nil
}

// ParseSubmit parses submission response from ChinaDataPay Wan gateway.
func (a *ChinaDataPayWanAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || gatewayWanError(resp.Body) {
		return driver.BuildFailedSubmitResultFromRaw(a.Name(), resp.StatusCode, resp.Body, nil), nil
	}

	taskID := ojson.GetBytes(resp.Body, "output.task_id").String()
	if !validGatewayWanTaskID(taskID) {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("Wan gateway response missing public output.task_id")
	}

	taskStatus := strings.ToUpper(ojson.GetBytes(resp.Body, "output.task_status").String())
	switch taskStatus {
	case "SUCCEEDED", "FAILED", "CANCELED":
		polled, err := parseGatewayWanPoll(a.Name(), resp)
		if err != nil {
			return aigc.GenerationSubmitResult{}, err
		}
		return aigc.GenerationSubmitResult{
			ProviderTaskID:   taskID,
			Status:           polled.Status,
			Stage:            polled.Stage,
			Progress:         polled.Progress,
			ProviderResponse: polled.ProviderResponse,
			Output:           polled.Output,
			Artifacts:        polled.Artifacts,
			ErrorCode:        polled.ErrorCode,
			ErrorMessage:     polled.ErrorMessage,
			Metadata:         map[string]any{"provider": a.Name(), "wan": ojson.ParseBytes(resp.Body).Value()},
		}, nil
	default:
		result := driver.BuildAsyncSubmitResult(taskID, map[string]any{"provider": a.Name(), "task_id": taskID})
		result.ProviderResponse = append(json.RawMessage(nil), resp.Body...)
		return result, nil
	}
}

// PreparePoll prepares task status query request.
func (a *ChinaDataPayWanAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := strings.TrimSpace(input.Generation.ProviderTaskID)
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("task_id is required for polling")
	}
	if !validGatewayWanTaskID(taskID) {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("Wan requires a public gateway task ID")
	}

	taskURL := a.tasksURL + "/" + taskID
	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      taskURL,
		Header:   header,
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": a.Name(), "task_id": taskID, "fail_on_query_client_error": true},
	}, nil
}

// ParsePoll converts gateway task status to unified poll result.
func (a *ChinaDataPayWanAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return parseGatewayWanPoll(a.Name(), resp)
}

// PrepareCancel reports that cancellation is not supported on gateway.
func (a *ChinaDataPayWanAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, fmt.Errorf("%s: upstream cancellation is not supported", a.Name())
}

// ParseCancel reports that cancellation is not supported on gateway.
func (a *ChinaDataPayWanAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return fmt.Errorf("%s: upstream cancellation is not supported", a.Name())
}

// ParseCallback parses task webhook notification from provider.
func (a *ChinaDataPayWanAdapter) ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return a.ParsePoll(ctx, resp)
}

func validGatewayWanTaskID(id string) bool {
	if !strings.HasPrefix(id, "task_") || len(id) <= 5 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func buildGatewayWanPayload(gen aigc.ContentGeneration, model string) (map[string]any, error) {
	if model != "wan3.0-video" && model != "wan3.0-video-prime" {
		return nil, fmt.Errorf("unsupported Wan gateway model")
	}
	var root map[string]any
	if err := json.Unmarshal(gen.Input, &root); err != nil || root == nil {
		return nil, fmt.Errorf("Wan input must be a JSON object")
	}
	extra := map[string]any{}
	if value, ok := root["extra_body"]; ok {
		var valid bool
		extra, valid = value.(map[string]any)
		if !valid {
			return nil, fmt.Errorf("extra_body must be an object")
		}
	}
	payload := map[string]any{}
	reserved := map[string]bool{"input": true, "parameters": true, "model": true, "prompt": true, "media": true, "input_reference": true, "first_frame": true, "last_frame": true, "content": true, "reference_images": true, "reference_image_urls": true, "seconds": true, "duration": true, "resolution": true, "ratio": true, "audio": true, "generate_audio": true, "prompt_extend": true, "watermark": true, "seed": true, "size": true}
	for key, value := range extra {
		if !reserved[key] {
			payload[key] = value
		}
	}
	input := map[string]any{}
	if value, ok := extra["input"]; ok {
		var valid bool
		input, valid = value.(map[string]any)
		if !valid {
			return nil, fmt.Errorf("Wan input must be an object")
		}
	} else {
		for _, key := range []string{"prompt", "first_frame", "last_frame"} {
			if value, exists := extra[key]; exists {
				if _, ok := value.(string); !ok {
					return nil, fmt.Errorf("Wan %s must be a string", key)
				}
			}
		}
		if value, exists := root["prompt"]; exists {
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("Wan prompt must be a string")
			}
		}
		if prompt := driver.ExtractPrompt(gen.Input); strings.TrimSpace(prompt) != "" {
			input["prompt"] = prompt
		}
		if media, ok := extra["media"]; ok {
			input["media"] = media
		} else {
			if err := validateGatewayWanReferences(gen.Input); err != nil {
				return nil, err
			}
			if (extra["first_frame"] != nil || extra["last_frame"] != nil) && len(qwen.ExtractWanReferences(gen.Input)) > 0 {
				return nil, fmt.Errorf("Wan frame shortcuts and input references must not be mixed; use one explicit media array")
			}
			if media := qwen.BuildWanMediaItems(gen.Input); len(media) > 0 {
				input["media"] = media
			}
		}
	}
	if err := validateGatewayWanInput(input); err != nil {
		return nil, err
	}
	params := map[string]any{}
	if value, ok := extra["parameters"]; ok {
		var valid bool
		params, valid = value.(map[string]any)
		if !valid {
			return nil, fmt.Errorf("Wan parameters must be an object")
		}
	}
	for _, key := range []string{"resolution", "ratio", "duration", "audio", "prompt_extend", "watermark", "seed"} {
		if _, exists := params[key]; !exists {
			if value, ok := extra[key]; ok {
				params[key] = value
			}
		}
	}
	if _, exists := params["duration"]; !exists {
		if seconds, ok := root["seconds"]; ok {
			params["duration"] = seconds
		} else if seconds, ok := extra["seconds"]; ok {
			params["duration"] = seconds
		} else {
			params["duration"] = float64(5)
		}
	}
	if _, exists := params["resolution"]; !exists {
		if size, ok := root["size"].(string); ok && size != "" {
			params["resolution"] = qwen.NormalizeWanResolution(driver.ResolutionFromSize(size))
		} else {
			params["resolution"] = "1080P"
		}
	}
	if _, exists := params["audio"]; !exists {
		if value, ok := extra["generate_audio"]; ok {
			params["audio"] = value
		}
	}
	res, ok := params["resolution"].(string)
	if !ok || strings.TrimSpace(res) == "" {
		return nil, fmt.Errorf("Wan resolution must be a nonempty string")
	}
	duration, ok := params["duration"].(float64)
	if !ok || math.Trunc(duration) != duration || (duration != -1 && (duration < 2 || duration > 30)) {
		return nil, fmt.Errorf("Wan duration must be an integer from 2 to 30, or -1")
	}
	for _, key := range []string{"audio", "prompt_extend", "watermark"} {
		if value, exists := params[key]; exists {
			if _, valid := value.(bool); !valid {
				return nil, fmt.Errorf("Wan %s must be boolean", key)
			}
		}
	}
	if value, exists := params["seed"]; exists {
		seed, ok := value.(float64)
		if !ok || seed != math.Trunc(seed) || seed < 0 || seed > 2147483647 {
			return nil, fmt.Errorf("Wan seed must be an integer from 0 to 2147483647")
		}
	}
	if value, exists := params["ratio"]; exists {
		if ratio, ok := value.(string); !ok || strings.TrimSpace(ratio) == "" {
			return nil, fmt.Errorf("Wan ratio must be a nonempty string")
		}
	}
	payload["model"] = model
	payload["input"] = input
	payload["parameters"] = params
	return payload, nil
}

func validateGatewayWanInput(input map[string]any) error {
	hasPrompt := false
	if value, ok := input["prompt"]; ok {
		prompt, isStr := value.(string)
		if !isStr {
			return fmt.Errorf("Wan prompt must be a string")
		}
		hasPrompt = strings.TrimSpace(prompt) != ""
	}
	var media []map[string]any
	if value, exists := input["media"]; exists {
		raw, err := json.Marshal(value)
		if err != nil || json.Unmarshal(raw, &media) != nil || value == nil {
			return fmt.Errorf("Wan media must be an array of objects")
		}
	}
	if !hasPrompt && len(media) == 0 {
		return fmt.Errorf("Wan requires prompt or media")
	}
	counts := map[string]int{}
	hasFrame, hasRef := false, false
	for _, item := range media {
		kind, ok := item["type"].(string)
		if !ok {
			return fmt.Errorf("Wan media type must be a string")
		}
		uri, ok := item["url"].(string)
		if !ok || strings.TrimSpace(uri) == "" || strings.HasPrefix(uri, "asset://") || strings.HasPrefix(uri, "mm_file://") {
			return fmt.Errorf("Wan media URL is missing or uses another provider's asset scheme")
		}
		switch kind {
		case "first_frame", "last_frame":
			hasFrame = true
		case "reference_image", "reference_video", "reference_audio", "file", "link":
			hasRef = true
		default:
			return fmt.Errorf("invalid Wan media type %q", kind)
		}
		counts[kind]++
	}
	if hasFrame && hasRef {
		return fmt.Errorf("Wan frames and reference media are mutually exclusive")
	}
	for kind, limit := range map[string]int{"first_frame": 1, "last_frame": 1, "reference_image": 10, "reference_video": 5, "reference_audio": 5} {
		if counts[kind] > limit {
			return fmt.Errorf("too many Wan %s entries (maximum %d)", kind, limit)
		}
	}
	return nil
}

func validateGatewayWanReferences(raw []byte) error {
	refs := ojson.GetBytes(raw, "input_reference")
	if !refs.Exists() {
		refs = ojson.GetBytes(raw, "extra_body.input_reference")
	}
	if !refs.Exists() {
		return nil
	}
	nodes := []ojson.Result{refs}
	if refs.IsArray() {
		nodes = refs.Array()
	}
	for _, node := range nodes {
		if node.Type != ojson.String && !node.IsObject() {
			return fmt.Errorf("invalid Wan reference")
		}
		if kind := node.Get("type"); kind.Exists() {
			switch kind.String() {
			case "image", "image_url", "video", "video_url", "audio", "audio_url", "first_frame", "last_frame", "reference_image", "reference_video", "reference_audio", "file", "link":
			default:
				return fmt.Errorf("unsupported Wan reference type")
			}
		}
		if role := node.Get("role"); role.Exists() {
			switch role.String() {
			case "first_frame", "last_frame", "reference_image", "reference_video", "reference_audio", "file", "link":
			default:
				return fmt.Errorf("unsupported Wan reference role")
			}
		}
		wrapped := []byte(`{"input_reference":` + node.Raw + `}`)
		if len(qwen.ExtractWanReferences(wrapped)) != 1 {
			return fmt.Errorf("Wan reference is missing its URL")
		}
	}
	return nil
}

func validateGatewayWanParameters(params map[string]any) error {
	for key, value := range params {
		switch key {
		case "resolution":
			res, ok := value.(string)
			if !ok {
				return fmt.Errorf("Wan resolution must be a string")
			}
			switch res {
			case "480P", "720P", "1080P":
			default:
				return fmt.Errorf("unsupported Wan resolution %q", res)
			}
		case "ratio":
			ratio, ok := value.(string)
			if !ok {
				return fmt.Errorf("Wan ratio must be a string")
			}
			switch ratio {
			case "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive":
			default:
				return fmt.Errorf("unsupported Wan ratio %q", ratio)
			}
		case "duration":
			var d float64
			switch v := value.(type) {
			case int:
				d = float64(v)
			case int64:
				d = float64(v)
			case float64:
				d = v
			default:
				return fmt.Errorf("Wan duration must be an integer")
			}
			if math.Trunc(d) != d || (d != -1 && (d < 2 || d > 30)) {
				return fmt.Errorf("Wan duration must be an integer in [2, 30] or -1")
			}
		case "audio", "prompt_extend", "watermark":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("Wan %s must be a boolean", key)
			}
		case "seed":
			var s float64
			switch v := value.(type) {
			case int:
				s = float64(v)
			case int64:
				s = float64(v)
			case float64:
				s = v
			default:
				return fmt.Errorf("Wan seed must be an integer")
			}
			if math.Trunc(s) != s || s < 0 || s > 2147483647 {
				return fmt.Errorf("Wan seed must be an integer in [0, 2147483647]")
			}
		}
	}
	return nil
}

func gatewayWanError(raw []byte) bool {
	return !ojson.ValidBytes(raw) || (ojson.GetBytes(raw, "code").Exists() && ojson.GetBytes(raw, "code").String() != "" && ojson.GetBytes(raw, "code").String() != "0")
}

func parseGatewayWanPoll(provider string, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return aigc.GenerationPollResult{}, fmt.Errorf("temporary Wan query failure (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || gatewayWanError(resp.Body) {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && !ojson.ValidBytes(resp.Body) {
			return aigc.GenerationPollResult{}, fmt.Errorf("temporary Wan query parse failure")
		}
		return driver.BuildFailedPollResultFromRaw(provider, resp.StatusCode, resp.Body, nil), nil
	}
	id := ojson.GetBytes(resp.Body, "output.task_id").String()
	if !validGatewayWanTaskID(id) {
		return aigc.GenerationPollResult{}, fmt.Errorf("Wan query is missing public output.task_id")
	}
	if expected, ok := resp.Metadata["task_id"].(string); ok && expected != id {
		return aigc.GenerationPollResult{}, fmt.Errorf("Wan task ID mismatch")
	}
	var result aigc.GenerationPollResult
	switch ojson.GetBytes(resp.Body, "output.task_status").String() {
	case "PENDING":
		result = driver.BuildRunningPollResult(10, aigc.StageProviderRunning)
	case "RUNNING":
		result = driver.BuildRunningPollResult(50, aigc.StageProviderRunning)
	case "UNKNOWN":
		return aigc.GenerationPollResult{}, fmt.Errorf("Wan task status is temporarily UNKNOWN; keep polling existing task")
	case "CANCELED":
		result = driver.BuildCanceledPollResult()
	case "FAILED":
		code, message := ojson.GetBytes(resp.Body, "output.code").String(), ojson.GetBytes(resp.Body, "output.message").String()
		if code == "" {
			code = "upstream_error_detail_missing"
		}
		if message == "" {
			message = "upstream did not return error detail"
		}
		result = driver.BuildFailedPollResult(code, message)
	case "SUCCEEDED":
		uri := ojson.GetBytes(resp.Body, "output.video_url")
		if uri.Type != ojson.String || strings.TrimSpace(uri.String()) == "" {
			return aigc.GenerationPollResult{}, fmt.Errorf("Wan succeeded response missing video_url")
		}
		meta := map[string]any{}
		if usage := ojson.GetBytes(resp.Body, "usage"); usage.IsObject() {
			meta["usage"] = usage.Value()
			if duration := usage.Get("output_video_duration"); duration.Exists() {
				meta["duration"] = duration.Value()
			} else if duration := usage.Get("duration"); duration.Exists() {
				meta["duration"] = duration.Value()
			}
		}
		result = driver.BuildCompletedPollResult([]aigc.ContentGenerationArtifact{driver.CreateArtifact("output_video", "cdp", uri.String(), "video/mp4", 0, meta)})
	default:
		return aigc.GenerationPollResult{}, fmt.Errorf("unrecognized Wan gateway status")
	}
	result.ProviderResponse = append(json.RawMessage(nil), resp.Body...)
	result.Metadata = map[string]any{"wan": ojson.ParseBytes(resp.Body).Value()}
	return result, nil
}

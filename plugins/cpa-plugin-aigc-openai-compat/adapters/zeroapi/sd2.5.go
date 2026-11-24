package zeroapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

const (
	adapterName = "zeroapi-video-v2"
	videosPath  = "/videos"
)

type videoAdapter struct{}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: adapterName,
		New:  func() driver.Adapter { return videoAdapter{} },
	})
}

func (videoAdapter) Name() string           { return adapterName }
func (videoAdapter) Kind() aigc.ContentKind { return aigc.ContentKindVideo }
func (videoAdapter) Prefixes() []string     { return []string{"ze/sd2.5"} }

func (videoAdapter) PrepareSubmit(_ context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	body := ojson.ParseBytes(gen.Input)
	prompt := driver.ExtractPrompt(gen.Input)
	if prompt == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: prompt is required")
	}
	model := driver.UpstreamModel(gen.Model)
	duration := firstString(body, "seconds", "extra_body.duration", "extra_body.seconds")
	if strings.HasPrefix(strings.ToLower(model), "sd2.5-antiao") {
		duration = "30"
	} else if strings.EqualFold(model, "sd2.5") && duration != "" {
		seconds, err := strconv.Atoi(duration)
		if err != nil || seconds < 4 || seconds > 30 {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: sd2.5 duration must be an integer from 4 to 30 seconds")
		}
		duration = strconv.Itoa(seconds)
	}
	size := driver.NormalizeSizeToX(gen.Input)
	if size == "" {
		switch firstString(body, "extra_body.ratio", "extra_body.aspect_ratio") {
		case "9:16":
			size = "720x1280"
		case "16:9":
			size = "1280x720"
		case "1:1":
			size = "1024x1024"
		case "4:3":
			size = "960x720"
		case "3:4":
			size = "720x960"
		case "21:9":
			size = "1280x544"
		}
	}
	refs, err := collectReferences(body)
	if err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	fields := map[string]any{"model": model, "prompt": prompt, "stream": false}
	if size != "" {
		fields["size"] = size
	}
	if duration != "" {
		fields["duration"] = duration
	}
	for key, values := range refs {
		fields[key] = values
	}
	doc := ojson.NewDocString(`{}`)
	for key, value := range fields {
		if err := doc.Set(key, value); err != nil {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: encode %s: %w", key, err)
		}
	}
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost, URL: videosPath, Model: gen.Model, Body: doc.Bytes(),
		Header: http.Header{"Content-Type": {"application/json"}}, Metadata: map[string]any{"provider": adapterName},
	}, nil
}

func collectReferences(body ojson.Result) (map[string][]string, error) {
	refs := make(map[string][]string)
	add := func(field, value string) error {
		parsed, err := url.Parse(value)
		if (err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https")) &&
			!(field == "images" && strings.HasPrefix(strings.ToLower(value), "data:image/")) {
			return fmt.Errorf("zeroapi: %s requires an HTTP(S) URL or an image data URL for images", field)
		}
		refs[field] = append(refs[field], value)
		return nil
	}
	var first, last string
	var otherImages []string
	input := body.Get("input_reference")
	items := input.Array()
	if input.Type == ojson.String || input.IsObject() {
		items = []ojson.Result{input}
	} else if input.Exists() && input.Type != ojson.Null && !input.IsArray() {
		return nil, fmt.Errorf("zeroapi: invalid input_reference")
	}
	for _, ref := range items {
		if ref.Type == ojson.String {
			otherImages = append(otherImages, strings.TrimSpace(ref.String()))
			continue
		}
		kind := firstString(ref, "type")
		switch kind {
		case "image_url":
			value := firstString(ref, "image_url.url", "image_url")
			switch firstString(ref, "role") {
			case "first_frame":
				first = value
			case "last_frame":
				last = value
			default:
				otherImages = append(otherImages, value)
			}
		case "video_url":
			if err := add("reference_videos", firstString(ref, "video_url.url", "video_url")); err != nil {
				return nil, err
			}
		case "audio_url":
			if err := add("audio_urls", firstString(ref, "audio_url.url", "audio_url")); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("zeroapi: unsupported reference type %q", kind)
		}
	}
	for _, frame := range []string{first, last} {
		if frame != "" {
			if err := add("images", frame); err != nil {
				return nil, err
			}
		}
	}
	for _, value := range otherImages {
		if err := add("images", value); err != nil {
			return nil, err
		}
	}
	for _, field := range []string{"images", "reference_videos", "audio_urls"} {
		node := body.Get("extra_body." + field)
		items := node.Array()
		switch {
		case !node.Exists() || node.IsArray():
		case field != "images" && node.Type == ojson.String:
			items = []ojson.Result{node}
		default:
			return nil, fmt.Errorf("zeroapi: invalid extra_body.%s; expected an array of URLs, or a single URL for video or audio", field)
		}
		for _, ref := range items {
			if err := add(field, strings.TrimSpace(ref.String())); err != nil {
				return nil, err
			}
		}
	}
	return refs, nil
}

func (videoAdapter) PreparePoll(_ context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := strings.TrimSpace(input.Generation.ProviderTaskID)
	if taskID == "" || taskID == "." || taskID == ".." {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: invalid provider task id")
	}
	return aigc.GenerationExecutionRequest{
		Method: http.MethodGet, URL: videosPath + "/" + url.PathEscape(taskID), Model: input.Generation.Model,
		Metadata: map[string]any{"provider": adapterName, "task_id": taskID},
	}, nil
}

func (videoAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	result, err := (videoAdapter{}).ParsePoll(ctx, resp)
	if err != nil {
		return aigc.GenerationSubmitResult{}, err
	}
	if result.Status == aigc.StatusFailed {
		return driver.BuildFailedSubmitResultFromRaw(adapterName, resp.StatusCode, resp.Body, nil), nil
	}
	taskID := firstString(ojson.ParseBytes(resp.Body), "id")
	if taskID == "" {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("zeroapi: missing provider task id")
	}
	metadata := map[string]any{"provider": adapterName, "task_id": taskID}
	submit := driver.BuildAsyncSubmitResult(taskID, metadata)
	if result.Status == aigc.StatusSucceeded {
		submit = driver.BuildCompletedSubmitResult(result.Artifacts, metadata)
		submit.ProviderTaskID = taskID
	}
	submit.ProviderResponse = resp.Body
	return submit, nil
}

func (videoAdapter) ParsePoll(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	body := ojson.ParseBytes(resp.Body)
	failure := body.Get("error")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (failure.Exists() && failure.Type != ojson.Null) {
		return driver.BuildFailedPollResultFromRaw(adapterName, resp.StatusCode, resp.Body, nil), nil
	}
	if !ojson.ValidBytes(resp.Body) || !body.IsObject() {
		return aigc.GenerationPollResult{}, fmt.Errorf("zeroapi: malformed task response")
	}
	switch strings.ToLower(firstString(body, "status")) {
	case "completed", "complete", "succeeded", "success":
		value := firstString(body, "result.data.0.url", "data.0.url", "result_url", "url", "metadata.url")
		if value == "" {
			return aigc.GenerationPollResult{}, fmt.Errorf("zeroapi: completed task has no video URL")
		}
		artifact := driver.CreateArtifact("output_video", "zeroapi", value, "video/mp4", 0, nil)
		return driver.BuildCompletedPollResult([]aigc.ContentGenerationArtifact{artifact}), nil
	case "failed", "failure", "error", "canceled", "cancelled", "expired":
		return driver.BuildFailedPollResultFromRaw(adapterName, resp.StatusCode, resp.Body, nil), nil
	case "queued", "pending":
		return driver.BuildRunningPollResult(10, aigc.StageProviderRunning), nil
	default:
		return driver.BuildRunningPollResult(50, aigc.StageProviderRunning), nil
	}
}

func (videoAdapter) PrepareCancel(context.Context, aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: task cancellation is not supported")
}

func (videoAdapter) ParseCancel(context.Context, aigc.GenerationExecutionResponse) error {
	return fmt.Errorf("zeroapi: task cancellation is not supported")
}

func (a videoAdapter) ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return a.ParsePoll(ctx, resp)
}

func firstString(body ojson.Result, paths ...string) string {
	for _, path := range paths {
		node := body.Get(path)
		if node.Type == ojson.String || node.Type == ojson.Number {
			if value := strings.TrimSpace(node.String()); value != "" {
				return value
			}
		}
	}
	return ""
}

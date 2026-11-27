package zeroapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

const (
	imageAdapterName       = "zeroapi-image"
	imageGenerationsPath   = "/images/generations"
	imageEditsPath         = "/images/edits"
	imageUnsupportedFormat = "zeroapi: image task %s is not supported"
)

// imageAdapter submits ZeroAPI GPT Image requests as pure JSON.
// ZeroAPI image edits are not standard OpenAI multipart requests, so reference
// images are forwarded as URLs inside the JSON body and are never downloaded.
type imageAdapter struct{}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: imageAdapterName,
		New:  func() driver.Adapter { return imageAdapter{} },
	})
}

func (imageAdapter) Name() string           { return imageAdapterName }
func (imageAdapter) Kind() aigc.ContentKind { return aigc.ContentKindImage }
func (imageAdapter) Prefixes() []string {
	return []string{"ze/gpt-image", "zeroapi/gpt-image"}
}

func (imageAdapter) PrepareSubmit(_ context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	in := gen.Input
	prompt := driver.ExtractPrompt(in)
	images := driver.ExtractImages(in)
	mask := imageMaskURL(in)

	operation, _ := gen.Metadata["image_operation"].(string)
	path := imageGenerationsPath
	switch operation {
	case "generations":
		if len(images) > 0 || mask != "" {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: reference images require the images/edits endpoint")
		}
	case "edits":
		if prompt == "" || len(images) == 0 {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: image edits require prompt and reference images")
		}
		path = imageEditsPath
	case "":
		if len(images) > 0 {
			if prompt == "" {
				return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: image edits require prompt and reference images")
			}
			path = imageEditsPath
		}
	default:
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: unsupported image operation %q", operation)
	}
	if prompt == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("zeroapi: prompt is required for image generation")
	}

	doc := ojson.NewDocString(`{}`)
	set := func(key string, value any) error {
		if err := doc.Set(key, value); err != nil {
			return fmt.Errorf("zeroapi: encode %s: %w", key, err)
		}
		return nil
	}
	if err := set("model", driver.UpstreamModel(gen.Model)); err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	if err := set("prompt", prompt); err != nil {
		return aigc.GenerationExecutionRequest{}, err
	}
	for _, key := range []string{"size", "quality", "response_format", "output_format", "background"} {
		value := strings.TrimSpace(ojson.GetBytes(in, key).String())
		if key == "size" {
			value = driver.NormalizeSizeToX(in)
		}
		if value == "" {
			continue
		}
		if err := set(key, value); err != nil {
			return aigc.GenerationExecutionRequest{}, err
		}
	}
	if n := ojson.GetBytes(in, "n").Int(); n > 0 {
		if err := set("n", n); err != nil {
			return aigc.GenerationExecutionRequest{}, err
		}
	}
	if path == imageEditsPath {
		if err := set("images", images); err != nil {
			return aigc.GenerationExecutionRequest{}, err
		}
		if len(images) == 1 {
			if err := set("image", images[0]); err != nil {
				return aigc.GenerationExecutionRequest{}, err
			}
		}
		if mask != "" {
			if err := set("mask", mask); err != nil {
				return aigc.GenerationExecutionRequest{}, err
			}
		}
	}

	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost, URL: path, Model: gen.Model, Body: doc.Bytes(),
		Header:   http.Header{"Content-Type": {"application/json"}},
		Metadata: map[string]any{"provider": imageAdapterName, "kind": "image"},
	}, nil
}

func (imageAdapter) ParseSubmit(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedSubmitResultFromRaw(imageAdapterName, resp.StatusCode, resp.Body, nil), nil
	}
	var artifacts []aigc.ContentGenerationArtifact
	for _, item := range ojson.GetBytes(resp.Body, "data").Array() {
		rawURL := strings.TrimSpace(item.Get("url").String())
		rawB64 := strings.TrimSpace(item.Get("b64_json").String())
		if rawURL != "" || rawB64 != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "zeroapi", rawURL, rawB64, nil))
		}
	}
	if len(artifacts) == 0 {
		return aigc.GenerationSubmitResult{}, fmt.Errorf("zeroapi: image response has no output")
	}
	meta := map[string]any{"provider": imageAdapterName}
	usage := driver.ExtractStandardUsage(resp.Body)
	if usage != nil {
		meta["usage"] = map[string]any{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
			"total_tokens":  usage.TotalTokens,
		}
	}
	result := driver.BuildCompletedSubmitResult(artifacts, meta)
	result.Usage = usage
	return result, nil
}

// GetUsage extracts token usage from the ZeroAPI image response.
func (imageAdapter) GetUsage(body []byte) *aigc.GenerationUsage {
	return driver.ExtractStandardUsage(body)
}

func (imageAdapter) PreparePoll(context.Context, aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, fmt.Errorf(imageUnsupportedFormat, "polling")
}

func (imageAdapter) ParsePoll(context.Context, aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{}, fmt.Errorf(imageUnsupportedFormat, "polling")
}

func (imageAdapter) PrepareCancel(context.Context, aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{}, fmt.Errorf(imageUnsupportedFormat, "cancellation")
}

func (imageAdapter) ParseCancel(context.Context, aigc.GenerationExecutionResponse) error {
	return fmt.Errorf(imageUnsupportedFormat, "cancellation")
}

func (imageAdapter) ParseCallback(context.Context, aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return aigc.GenerationPollResult{}, fmt.Errorf(imageUnsupportedFormat, "callbacks")
}

// imageMaskURL returns the mask reference URL from either a string or an image_url object.
func imageMaskURL(body []byte) string {
	mask := ojson.GetBytes(body, "mask")
	if !mask.Exists() {
		return ""
	}
	if mask.IsObject() {
		for _, path := range []string{"image_url.url", "image_url", "url"} {
			if value := strings.TrimSpace(mask.Get(path).String()); value != "" {
				return value
			}
		}
		return ""
	}
	return strings.TrimSpace(mask.String())
}

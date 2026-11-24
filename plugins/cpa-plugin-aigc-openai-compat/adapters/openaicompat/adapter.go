// Package openaicompat 提供通用的 OpenAI 兼容图像与视频生成适配器。
package openaicompat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// ImageRequest 代表标准 OpenAI 兼容的图像生成与编辑请求体载荷。
type ImageRequest struct {
	// --- OpenAI 官方标准生成字段 ---
	Model          string `json:"model"`                     // 模型名称（必填）
	Prompt         string `json:"prompt,omitempty"`          // 提示词描述
	N              *int64 `json:"n,omitempty"`               // 生成数量（1-10，默认 1）
	Quality        string `json:"quality,omitempty"`         // 画质设置（如 standard, hd, high）
	Size           string `json:"size,omitempty"`            // 分辨率尺寸（如 1024x1024, auto）
	Style          string `json:"style,omitempty"`           // 风格预设（如 vivid, natural）
	ResponseFormat string `json:"response_format,omitempty"` // 响应格式（url, b64_json）
	User           string `json:"user,omitempty"`            // 终端用户标识符

	// --- GPT-Image 系列官方标准扩展字段 ---
	OutputFormat string `json:"output_format,omitempty"` // 输出文件格式（如 png, jpeg, webp）
	Background   string `json:"background,omitempty"`    // 背景模式（如 transparent, opaque）

	// --- 第三方网关 / 动图模型控制扩展字段 ---
	NegativePrompt  string `json:"negative_prompt,omitempty"`   // 负向提示词
	Seed            *int64 `json:"seed,omitempty"`              // 随机种子
	FrameCount      *int64 `json:"frame_count,omitempty"`       // 动图/动画帧数（GPT Image 2.6 / GIF）
	FrameDurationMs *int64 `json:"frame_duration_ms,omitempty"` // 单帧持续时间毫秒数
	Complexity      string `json:"complexity,omitempty"`        // 复杂度控制（如 medium, high）

	// --- JSON 模式图生图 / 图像编辑扩展（非 multipart 场景）---
	Image  string `json:"image,omitempty"`  // 单图参考输入（如图片变体模式）
	Images any    `json:"images,omitempty"` // 多图参考输入（对象数组或字符串数组）
	Mask   any    `json:"mask,omitempty"`   // 蒙版输入（支持对象或 URL 字符串）
}

// ImageURLRef 代表图像编辑请求中的参考图对象结构（包含 image_url 键）。
type ImageURLRef struct {
	ImageURL string `json:"image_url"` // 图片 URL
}

// MaskURLRef 代表图像编辑请求中的蒙版对象结构（包含 image_url 键）。
type MaskURLRef struct {
	ImageURL string `json:"image_url"` // 蒙版图片 URL
}

// VideoRequest 代表标准 OpenAI 兼容的视频生成请求体载荷。
type VideoRequest struct {
	// --- OpenAI 视频生成基础字段 ---
	Model    string `json:"model"`              // 视频模型名称（必填）
	Prompt   string `json:"prompt,omitempty"`   // 提示词描述
	Duration int64  `json:"duration,omitempty"` // 视频时长（秒，归一化自 duration 或 seconds）
	FPS      int64  `json:"fps,omitempty"`      // 视频帧率
	Size     string `json:"size,omitempty"`     // 视频分辨率尺寸（如 1280x720）
	Quality  string `json:"quality,omitempty"`  // 生成质量设置

	// --- 网关扩展与首尾帧推导字段 ---
	ImageURL          string `json:"image_url,omitempty"`            // 首帧图片 URL（第一张参考图）
	LastFrameImageURL string `json:"last_frame_image_url,omitempty"` // 尾帧图片 URL（第二张参考图）
	Ratio             string `json:"ratio,omitempty"`                // 画幅比例（根据 size 自动推导，如 16:9）
	Seed              *int64 `json:"seed,omitempty"`                 // 随机种子
}

// Usage 代表响应中的 Token 消耗信息（兼容 OpenAI 官方规范及第三方网关通用字段）。
type Usage struct {
	PromptTokens       int64               `json:"prompt_tokens,omitempty"`        // 通用网关提示词 Token
	CompletionTokens   int64               `json:"completion_tokens,omitempty"`    // 通用网关生成 Token
	TotalTokens        int64               `json:"total_tokens,omitempty"`         // 总 Token 数
	InputTokens        int64               `json:"input_tokens,omitempty"`         // 官方 GPT-Image 输入 Token
	OutputTokens       int64               `json:"output_tokens,omitempty"`        // 官方 GPT-Image 输出 Token
	InputTokensDetails *InputTokensDetails `json:"input_tokens_details,omitempty"` // 输入 Token 明细
}

// InputTokensDetails 包含输入文本与图像的 Token 明细。
type InputTokensDetails struct {
	TextTokens  int64 `json:"text_tokens,omitempty"`  // 文本 Token 数
	ImageTokens int64 `json:"image_tokens,omitempty"` // 图像 Token 数
}

// ImageResponse 代表 OpenAI 图像操作的标注文档响应结构。
type ImageResponse struct {
	Created int64       `json:"created"`         // 创建时间戳
	Data    []ImageData `json:"data"`            // 生成的数据项列表
	Usage   *Usage      `json:"usage,omitempty"` // Token 消耗统计
}

// ImageData 代表图像响应中包含的具体媒体产物数据。
type ImageData struct {
	URL           string `json:"url,omitempty"`            // 产物 URL
	B64JSON       string `json:"b64_json,omitempty"`       // Base64 编码的图片数据
	RevisedPrompt string `json:"revised_prompt,omitempty"` // 模型修正后的提示词
}

// OpenAICompatAdapter 通用 OpenAI 兼容协议适配器，负责图像与视频请求的转换与响应解析。
type OpenAICompatAdapter struct{}

// NewOpenAICompatAdapter 创建一个通用 OpenAI 兼容适配器实例。
func NewOpenAICompatAdapter() *OpenAICompatAdapter {
	return &OpenAICompatAdapter{}
}

func init() {
	// 注册到驱动层工厂目录，标记为 Fallback 兜底适配器
	driver.MustRegisterFactory(driver.Factory{
		Name:     "openai-compat",
		New:      func() driver.Adapter { return NewOpenAICompatAdapter() },
		Fallback: true,
	})
}

// Name 返回适配器的唯一标识符，用于路由分发与元数据持久化。
func (a *OpenAICompatAdapter) Name() string {
	return "openai-compat"
}

// Supports 判断是否支持当前请求：接收模型名非空的图片与视频生成请求（作为全局兜底）。
func (a *OpenAICompatAdapter) Supports(_ context.Context, req aigc.GenerationSupportRequest) bool {
	return strings.TrimSpace(req.Model) != "" &&
		(req.Kind == aigc.ContentKindImage || req.Kind == aigc.ContentKindVideo)
}

// PrepareSubmit 构建发送给上游模型的 HTTP 执行请求。
func (a *OpenAICompatAdapter) PrepareSubmit(ctx context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	gen := input.Generation
	inputBytes := gen.Input

	if gen.Kind == aigc.ContentKindVideo {
		return a.prepareVideoSubmit(gen, inputBytes, "/videos/generations")
	}

	return a.prepareImageSubmit(ctx, gen, inputBytes, "/images/generations", "/images/edits")
}

// prepareImageSubmit 组装图片生成的 HTTP 请求体与路由目标端点。
func (a *OpenAICompatAdapter) prepareImageSubmit(ctx context.Context, gen aigc.ContentGeneration, inputBytes []byte, submitPath, editPath string) (aigc.GenerationExecutionRequest, error) {
	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)
	hasMask := ojson.GetBytes(inputBytes, "mask").Exists()

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required for image generation")
	}

	hasPrompt := prompt != ""
	hasImages := len(images) > 0

	targetPath := submitPath
	operation, _ := gen.Metadata["image_operation"].(string)
	switch operation {
	case "generations":
		// 显式文生图：不允许携带参考图或蒙版
		if hasImages || hasMask {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("reference images require the images/edits endpoint")
		}
	case "edits":
		// 显式图像编辑：必须同时携带提示词与参考图
		if !hasImages || !hasPrompt {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("image edits require prompt and reference images")
		}
		targetPath = editPath
	case "":
		// 兼容未显式指定操作的旧调用方：按入参自动推断路由路径
		if hasImages && hasPrompt && editPath != "" {
			targetPath = editPath
		} else if hasImages && !hasPrompt {
			targetPath = "/images/variations"
		}
	default:
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("unsupported image operation")
	}

	// 构造标准强类型 ImageRequest 结构体
	req := ImageRequest{
		Model:          gen.Model,
		Prompt:         prompt,
		Size:           driver.NormalizeSizeToX(inputBytes),
		Quality:        strings.TrimSpace(ojson.GetBytes(inputBytes, "quality").String()),
		ResponseFormat: strings.TrimSpace(ojson.GetBytes(inputBytes, "response_format").String()),
		Style:          strings.TrimSpace(ojson.GetBytes(inputBytes, "style").String()),
		NegativePrompt: driver.ExtractNegativePrompt(inputBytes),
		User:           strings.TrimSpace(ojson.GetBytes(inputBytes, "user").String()),
		OutputFormat:   strings.TrimSpace(ojson.GetBytes(inputBytes, "output_format").String()),
		Complexity:     strings.TrimSpace(ojson.GetBytes(inputBytes, "complexity").String()),
		Background:     strings.TrimSpace(ojson.GetBytes(inputBytes, "background").String()),
	}

	// 针对数值型可选参数，使用指针以保证未提供时 omitempty 生效
	if n := ojson.GetBytes(inputBytes, "n").Int(); n > 0 {
		req.N = &n
	}
	if seed := ojson.GetBytes(inputBytes, "seed"); seed.Exists() {
		v := seed.Int()
		req.Seed = &v
	}
	if fc := ojson.GetBytes(inputBytes, "frame_count"); fc.Exists() {
		v := fc.Int()
		req.FrameCount = &v
	}
	if fd := ojson.GetBytes(inputBytes, "frame_duration_ms"); fd.Exists() {
		v := fd.Int()
		req.FrameDurationMs = &v
	}

	// 编辑路径下图片与蒙版采用对象数组包装，非编辑路径采用字符串形式
	if targetPath == editPath {
		var imageObjects []ImageURLRef
		for _, img := range images {
			imageObjects = append(imageObjects, ImageURLRef{ImageURL: img})
		}
		if len(imageObjects) > 0 {
			req.Images = imageObjects
		}
		if hasMask {
			maskVal := ojson.GetBytes(inputBytes, "mask")
			if maskVal.IsObject() && maskVal.Get("image_url").Exists() {
				req.Mask = maskVal.Value()
			} else if maskStr := strings.TrimSpace(maskVal.String()); maskStr != "" {
				req.Mask = MaskURLRef{ImageURL: maskStr}
			}
		}
	} else {
		if len(images) > 0 {
			if len(images) == 1 {
				req.Image = images[0]
			} else {
				req.Images = images
			}
		}
		if hasMask {
			req.Mask = ojson.GetBytes(inputBytes, "mask").Value()
		}
	}

	bodyBytes, errMarshal := json.Marshal(req)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal openai-compat image payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	if targetPath == editPath && ShouldMultipartImageEdit(gen.Model) {
		multipartBody, contentType, errMultipart := PrepareImageEditMultipart(ctx, bodyBytes)
		if errMultipart != nil {
			return aigc.GenerationExecutionRequest{}, fmt.Errorf("prepare multipart image edit: %w", errMultipart)
		}
		bodyBytes = multipartBody
		header.Set("Content-Type", contentType)
	}

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      targetPath,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "openai-compat", "kind": "image"},
	}, nil
}

// prepareVideoSubmit 组装视频生成的 HTTP 请求体。
func (a *OpenAICompatAdapter) prepareVideoSubmit(gen aigc.ContentGeneration, inputBytes []byte, submitPath string) (aigc.GenerationExecutionRequest, error) {
	prompt := driver.ExtractPrompt(inputBytes)
	images := driver.ExtractImages(inputBytes)

	if prompt == "" && len(images) == 0 {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("prompt or image is required for video generation")
	}

	req := VideoRequest{
		Model:   gen.Model,
		Prompt:  prompt,
		FPS:     ojson.GetBytes(inputBytes, "fps").Int(),
		Size:    driver.NormalizeSizeToX(inputBytes),
		Ratio:   driver.RatioFromSize(ojson.GetBytes(inputBytes, "size").String()),
		Quality: strings.TrimSpace(ojson.GetBytes(inputBytes, "quality").String()),
	}

	// 映射首尾帧图片引用
	if len(images) > 0 {
		req.ImageURL = images[0]
		if len(images) > 1 {
			req.LastFrameImageURL = images[1]
		}
	}

	// 提取视频时长，兼容 duration 与 seconds 字段
	if duration := ojson.GetBytes(inputBytes, "duration").Int(); duration > 0 {
		req.Duration = duration
	} else if seconds := ojson.GetBytes(inputBytes, "seconds").Int(); seconds > 0 {
		req.Duration = seconds
	}

	if seed := ojson.GetBytes(inputBytes, "seed"); seed.Exists() {
		v := seed.Int()
		req.Seed = &v
	}

	bodyBytes, errMarshal := json.Marshal(req)
	if errMarshal != nil {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("marshal openai-compat video payload: %w", errMarshal)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return aigc.GenerationExecutionRequest{
		Method:   http.MethodPost,
		URL:      submitPath,
		Header:   header,
		Body:     bodyBytes,
		Model:    gen.Model,
		Metadata: map[string]any{"provider": "openai-compat", "kind": "video"},
	}, nil
}

// ParseSubmit 解析任务创建响应，提取同步产物或异步任务 ID。
func (a *OpenAICompatAdapter) ParseSubmit(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedSubmitResultFromRaw("openai-compat", resp.StatusCode, resp.Body, nil), nil
	}

	meta := map[string]any{"provider": "openai-compat"}
	usage := driver.ExtractStandardUsage(resp.Body)
	if usageMeta := extractUsageMetadata(resp.Body); len(usageMeta) > 0 {
		meta["usage"] = usageMeta
	}

	// 优先检查响应是否直接包含同步图片或视频产物
	artifacts := extractOpenAICompatArtifacts(resp.Body)
	if len(artifacts) > 0 {
		res := driver.BuildCompletedSubmitResult(artifacts, meta)
		res.Usage = usage
		return res, nil
	}

	// 提取上游异步任务 ID
	taskID := firstString(resp.Body, "id", "task_id", "video_id", "task.id")
	if taskID != "" {
		meta["task_id"] = taskID
		status := strings.ToLower(firstString(resp.Body, "status", "state"))
		if status == "completed" || status == "succeeded" || status == "success" {
			artifacts = extractOpenAICompatArtifacts(resp.Body)
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

// PreparePoll 构造异步任务状态轮询的 GET 请求。
func (a *OpenAICompatAdapter) PreparePoll(ctx context.Context, input aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	if taskID == "" {
		return aigc.GenerationExecutionRequest{}, fmt.Errorf("provider task_id is required for poll")
	}

	pollURL := fmt.Sprintf("/videos/generations/%s", taskID)
	return aigc.GenerationExecutionRequest{
		Method:   http.MethodGet,
		URL:      pollURL,
		Model:    input.Generation.Model,
		Metadata: map[string]any{"provider": "openai-compat", "task_id": taskID},
	}, nil
}

// ParsePoll 解析轮询结果，映射为系统统一的生成状态与产物。
func (a *OpenAICompatAdapter) ParsePoll(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return driver.BuildFailedPollResultFromRaw("openai-compat", resp.StatusCode, resp.Body, nil), nil
	}

	var result aigc.GenerationPollResult
	state := strings.ToLower(firstString(resp.Body, "status", "state"))
	switch state {
	case "completed", "succeeded", "success", "done":
		artifacts := extractOpenAICompatArtifacts(resp.Body)
		result = driver.BuildCompletedPollResult(artifacts)
	case "failed", "error":
		result = driver.BuildFailedPollResultFromRaw("openai-compat", 200, resp.Body, nil)
	case "canceled", "cancelled":
		result = driver.BuildCanceledPollResult()
	case "queued", "pending":
		result = driver.BuildRunningPollResult(10, aigc.StageProviderRunning)
	default:
		progress := int(ojson.GetBytes(resp.Body, "progress").Int())
		if progress <= 0 {
			progress = 50
		}
		result = driver.BuildRunningPollResult(progress, aigc.StageProviderRunning)
	}

	if usageMeta := extractUsageMetadata(resp.Body); len(usageMeta) > 0 {
		if result.Metadata == nil {
			result.Metadata = make(map[string]any)
		}
		result.Metadata["usage"] = usageMeta
	}
	result.Usage = a.GetUsage(resp.Body)

	return result, nil
}

// GetUsage extracts token usage from OpenAI-compatible response body.
func (a *OpenAICompatAdapter) GetUsage(body []byte) *aigc.GenerationUsage {
	return driver.ExtractStandardUsage(body)
}

// PrepareCancel 构造异步视频任务的取消请求。
func (a *OpenAICompatAdapter) PrepareCancel(ctx context.Context, input aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	taskID := input.Generation.ProviderTaskID
	return aigc.GenerationExecutionRequest{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("/videos/generations/%s/cancel", taskID),
		Model:  input.Generation.Model,
	}, nil
}

// ParseCancel 处理取消响应。
func (a *OpenAICompatAdapter) ParseCancel(ctx context.Context, resp aigc.GenerationExecutionResponse) error {
	return nil
}

// ParseCallback 解析来自厂商的 Webhook 回调通知（复用轮询解析逻辑）。
func (a *OpenAICompatAdapter) ParseCallback(ctx context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	return a.ParsePoll(ctx, resp)
}

// extractOpenAICompatArtifacts 从响应载荷中提取生成的图片或视频产物。
func extractOpenAICompatArtifacts(body []byte) []aigc.ContentGenerationArtifact {
	var artifacts []aigc.ContentGenerationArtifact

	dataItems := ojson.GetBytes(body, "data").Array()
	for _, item := range dataItems {
		u := item.Get("url").String()
		b64 := item.Get("b64_json").String()

		if strings.Contains(u, ".mp4") || strings.Contains(u, "video") {
			artifacts = append(artifacts, driver.CreateArtifact("output_video", "openai-compat", u, "video/mp4", 0, nil))
		} else if u != "" || b64 != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "openai-compat", u, b64, nil))
		}
	}

	if len(artifacts) == 0 {
		if videoURL := firstString(body, "output.0.uri", "output.0.url", "output.video_url", "video_url", "result.video_url"); videoURL != "" && (strings.Contains(videoURL, ".mp4") || strings.Contains(videoURL, "video") || (!strings.HasSuffix(videoURL, ".png") && !strings.HasSuffix(videoURL, ".jpg") && !strings.HasSuffix(videoURL, ".jpeg") && !strings.HasSuffix(videoURL, ".webp"))) {
			artifacts = append(artifacts, driver.CreateArtifact("output_video", "openai-compat", videoURL, "video/mp4", 0, nil))
		} else if videoURL := firstString(body, "video_url", "output.video_url", "result.video_url"); videoURL != "" {
			artifacts = append(artifacts, driver.CreateArtifact("output_video", "openai-compat", videoURL, "video/mp4", 0, nil))
		} else if u := firstString(body, "output.0.uri", "output.0.url", "url"); u != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "openai-compat", u, "", nil))
		} else if b64 := firstString(body, "b64_json"); b64 != "" {
			artifacts = append(artifacts, driver.ProcessImageArtifact("output_image", "openai-compat", "", b64, nil))
		}
	}

	return artifacts
}

// firstString 扫描多个 JSON 路径并返回第一个非空的字符串值。
func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if val := strings.TrimSpace(ojson.GetBytes(body, path).String()); val != "" {
			return val
		}
	}
	return ""
}

// extractUsageMetadata 从响应载荷中提取 usage 对象映射。
func extractUsageMetadata(body []byte) map[string]any {
	if u := ojson.GetBytes(body, "usage"); u.IsObject() {
		if val, ok := u.Value().(map[string]any); ok {
			return val
		}
	}
	return nil
}

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	internalaigc "github.com/router-for-me/CLIProxyAPI/v7/internal/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type openAIImageItem struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageGenerationUsage defines the official OpenAI image generation usage specification.
type ImageGenerationUsage struct {
	InputTokens         int64                `json:"input_tokens"`
	InputTokensDetails  *InputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokens        int64                `json:"output_tokens"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details,omitempty"`
	TotalTokens         int64                `json:"total_tokens"`
}

type InputTokensDetails struct {
	ImageTokens int64 `json:"image_tokens"`
	TextTokens  int64 `json:"text_tokens"`
}

type OutputTokensDetails struct {
	ImageTokens int64 `json:"image_tokens"`
	TextTokens  int64 `json:"text_tokens"`
}

type openAIImageResponse struct {
	Created      int64                 `json:"created"`
	Data         []openAIImageItem     `json:"data"`
	Background   string                `json:"background,omitempty"`
	OutputFormat string                `json:"output_format,omitempty"`
	Quality      string                `json:"quality,omitempty"`
	Size         string                `json:"size,omitempty"`
	Model        string                `json:"model,omitempty"`
	Usage        *ImageGenerationUsage `json:"usage,omitempty"`
}

type aigcResponse struct {
	ID        string                `json:"id"`
	Object    string                `json:"object"`
	Kind      string                `json:"kind"`
	Model     string                `json:"model,omitempty"`
	Status    string                `json:"status"`
	Stage     string                `json:"stage"`
	Progress  int                   `json:"progress"`
	CreatedAt int64                 `json:"created_at"`
	UpdatedAt int64                 `json:"updated_at,omitempty"`
	Artifacts []aigcArtifact        `json:"artifacts,omitempty"`
	Error     *aigcErrorField       `json:"error,omitempty"`
	Usage     *ImageGenerationUsage `json:"usage,omitempty"`
}

type aigcArtifact struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type aigcErrorField struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func normalizeImageUsage(payload []byte) *ImageGenerationUsage {
	if len(payload) == 0 {
		return nil
	}

	var uNode gjson.Result
	if u := gjson.GetBytes(payload, "usage"); u.Exists() && u.IsObject() {
		uNode = u
	} else if u := gjson.GetBytes(payload, "usageMetadata"); u.Exists() && u.IsObject() {
		uNode = u
	} else if u := gjson.GetBytes(payload, "response.usage"); u.Exists() && u.IsObject() {
		uNode = u
	} else if u := gjson.GetBytes(payload, "response.tool_usage.image_gen"); u.Exists() && u.IsObject() {
		uNode = u
	} else {
		return nil
	}

	// 1. Input tokens
	var inputTokens int64
	if v := uNode.Get("input_tokens"); v.Exists() {
		inputTokens = v.Int()
	} else if v := uNode.Get("prompt_tokens"); v.Exists() {
		inputTokens = v.Int()
	} else if v := uNode.Get("promptTokenCount"); v.Exists() {
		inputTokens = v.Int()
	}

	// 2. Output tokens
	var outputTokens int64
	if v := uNode.Get("output_tokens"); v.Exists() {
		outputTokens = v.Int()
	} else if v := uNode.Get("completion_tokens"); v.Exists() {
		outputTokens = v.Int()
	} else if v := uNode.Get("candidatesTokenCount"); v.Exists() {
		outputTokens = v.Int()
	}

	// 3. Input tokens details
	var inputTextTokens, inputImageTokens int64
	if v := uNode.Get("input_tokens_details.text_tokens"); v.Exists() {
		inputTextTokens = v.Int()
	} else if v := uNode.Get("prompt_tokens_details.text_tokens"); v.Exists() {
		inputTextTokens = v.Int()
	} else {
		inputTextTokens = inputTokens
	}

	if v := uNode.Get("input_tokens_details.image_tokens"); v.Exists() {
		inputImageTokens = v.Int()
	} else if v := uNode.Get("prompt_tokens_details.image_tokens"); v.Exists() {
		inputImageTokens = v.Int()
	} else if v := uNode.Get("input_images"); v.Exists() {
		inputImageTokens = v.Int()
	}

	if inputTokens == 0 && (inputTextTokens > 0 || inputImageTokens > 0) {
		inputTokens = inputTextTokens + inputImageTokens
	}

	// 4. Output tokens details
	var outputTextTokens, outputImageTokens int64
	if v := uNode.Get("output_tokens_details.text_tokens"); v.Exists() {
		outputTextTokens = v.Int()
	} else if v := uNode.Get("completion_tokens_details.text_tokens"); v.Exists() {
		outputTextTokens = v.Int()
	}

	if v := uNode.Get("output_tokens_details.image_tokens"); v.Exists() {
		outputImageTokens = v.Int()
	} else if v := uNode.Get("completion_tokens_details.image_tokens"); v.Exists() {
		outputImageTokens = v.Int()
	} else if outputTokens > 0 {
		outputImageTokens = outputTokens - outputTextTokens
	}

	if outputTokens == 0 && (outputTextTokens > 0 || outputImageTokens > 0) {
		outputTokens = outputTextTokens + outputImageTokens
	}

	// 5. Total tokens
	var totalTokens int64
	if v := uNode.Get("total_tokens"); v.Exists() {
		totalTokens = v.Int()
	} else if v := uNode.Get("totalTokenCount"); v.Exists() {
		totalTokens = v.Int()
	}
	if totalTokens == 0 || totalTokens < (inputTokens+outputTokens) {
		totalTokens = inputTokens + outputTokens
	}

	return &ImageGenerationUsage{
		InputTokens: inputTokens,
		InputTokensDetails: &InputTokensDetails{
			ImageTokens: inputImageTokens,
			TextTokens:  inputTextTokens,
		},
		OutputTokens: outputTokens,
		OutputTokensDetails: &OutputTokensDetails{
			ImageTokens: outputImageTokens,
			TextTokens:  outputTextTokens,
		},
		TotalTokens: totalTokens,
	}
}

func formatAIGCResponse(gen aigc.ContentGeneration) aigcResponse {
	resp := aigcResponse{
		ID:        gen.ID,
		Object:    "content_generation",
		Kind:      string(gen.Kind),
		Model:     gen.Model,
		Status:    string(gen.Status),
		Stage:     string(gen.Stage),
		Progress:  gen.Progress,
		CreatedAt: gen.CreatedAt.Unix(),
	}
	if !gen.UpdatedAt.IsZero() {
		resp.UpdatedAt = gen.UpdatedAt.Unix()
	}
	if gen.ErrorCode != "" || gen.ErrorMessage != "" {
		resp.Error = &aigcErrorField{
			Code:    gen.ErrorCode,
			Message: gen.ErrorMessage,
		}
	}
	if len(gen.Artifacts) > 0 {
		resp.Artifacts = make([]aigcArtifact, 0, len(gen.Artifacts))
		for _, a := range gen.Artifacts {
			artifactType := a.ArtifactType
			if artifactType == "" {
				artifactType = string(gen.Kind)
			}
			resp.Artifacts = append(resp.Artifacts, aigcArtifact{
				Type: artifactType,
				URL:  a.URI,
			})
		}
	} else if len(gen.Output) > 0 {
		var artifacts []aigc.ContentGenerationArtifact
		if err := json.Unmarshal(gen.Output, &artifacts); err == nil && len(artifacts) > 0 {
			resp.Artifacts = make([]aigcArtifact, 0, len(artifacts))
			for _, a := range artifacts {
				artifactType := a.ArtifactType
				if artifactType == "" {
					artifactType = string(gen.Kind)
				}
				uri := a.URI
				resp.Artifacts = append(resp.Artifacts, aigcArtifact{
					Type: artifactType,
					URL:  uri,
				})
			}
		} else {
			var fallback []aigcArtifact
			if errFallback := json.Unmarshal(gen.Output, &fallback); errFallback == nil && len(fallback) > 0 {
				resp.Artifacts = fallback
			}
		}
	}
	if len(gen.Output) > 0 {
		resp.Usage = normalizeImageUsage(gen.Output)
	}
	if resp.Usage == nil && len(gen.ProviderResponse) > 0 {
		resp.Usage = normalizeImageUsage(gen.ProviderResponse)
	}
	return resp
}

func (s *Server) getAIGCCoordinator() *internalaigc.Coordinator {
	if s.aigcCoordinator != nil {
		return s.aigcCoordinator
	}
	if s.pluginHost != nil {
		s.aigcCoordinator = internalaigc.NewCoordinator(s.pluginHost, s.handlers)
		worker := internalaigc.NewRecoveryWorker(s.aigcCoordinator, 10*time.Second)
		worker.Start(context.Background())
		return s.aigcCoordinator
	}
	return nil
}

func (s *Server) getAIGCSyncPipeline() *internalaigc.SyncPipeline {
	if s.aigcSyncPipeline != nil {
		return s.aigcSyncPipeline
	}
	if s.pluginHost != nil {
		s.aigcSyncPipeline = internalaigc.NewSyncPipeline(s.pluginHost, s.handlers)
		return s.aigcSyncPipeline
	}
	return nil
}

func (s *Server) handleAIGCAsyncCreate(c *gin.Context, kind aigc.ContentKind) {
	coordinator := s.getAIGCCoordinator()
	if coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "aigc service unavailable (no plugin host)"})
		return
	}

	canonicalHdr := strings.TrimSpace(c.GetHeader("X-Canonical-Body"))
	if canonicalHdr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "missing_canonical_body",
				"message": "X-Canonical-Body header is required for AIGC task creation",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(canonicalHdr), &parsed); err != nil || len(parsed) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "invalid_canonical_body",
				"message": "X-Canonical-Body header must be a valid non-empty JSON object",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	bodyBytes, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	apiKey := c.GetString("userApiKey")
	clientIP := c.ClientIP()
	reqID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if reqID == "" {
		reqID = strings.TrimSpace(c.GetString("request_id"))
	}

	metadata := map[string]any{
		"canonical": parsed,
	}

	draft := aigc.ContentGenerationDraft{
		RequestID: reqID,
		Kind:      kind,
		APIKey:    apiKey,
		ClientIP:  clientIP,
		Input:     bodyBytes,
		Metadata:  metadata,
	}

	gen, errCreate := coordinator.CreateGeneration(c.Request.Context(), kind, draft)
	if errCreate != nil {
		if errors.Is(errCreate, aigc.ErrStoreNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "aigc store plugin not configured"})
			return
		}
		if errors.Is(errCreate, aigc.ErrMutationRejected) {
			c.JSON(http.StatusForbidden, gin.H{"error": errCreate.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errCreate.Error()})
		return
	}

	c.JSON(http.StatusAccepted, formatAIGCResponse(gen))
}

func (s *Server) handleAIGCAsyncGet(c *gin.Context) {
	coordinator := s.getAIGCCoordinator()
	if coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "aigc service unavailable"})
		return
	}

	genID := strings.TrimSpace(c.Param("generation_id"))
	if genID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing generation_id parameter"})
		return
	}

	gen, errGet := coordinator.GetGeneration(c.Request.Context(), genID)
	if errGet != nil {
		if errors.Is(errGet, aigc.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "generation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errGet.Error()})
		return
	}
	if gen.ID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "generation not found"})
		return
	}

	callerAPIKey := strings.TrimSpace(c.GetString("userApiKey"))
	if gen.APIKey != "" && callerAPIKey != "" && gen.APIKey != callerAPIKey {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied: generation belongs to another key"})
		return
	}

	c.JSON(http.StatusOK, formatAIGCResponse(gen))
}

func (s *Server) handleAIGCAsyncGetContent(c *gin.Context) {
	coordinator := s.getAIGCCoordinator()
	if coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "aigc service unavailable"})
		return
	}

	genID := strings.TrimSpace(c.Param("generation_id"))
	if genID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing generation_id parameter"})
		return
	}

	gen, errGet := coordinator.GetGeneration(c.Request.Context(), genID)
	if errGet != nil {
		if errors.Is(errGet, aigc.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "generation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errGet.Error()})
		return
	}
	if gen.ID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "generation not found"})
		return
	}

	callerAPIKey := strings.TrimSpace(c.GetString("userApiKey"))
	if gen.APIKey != "" && callerAPIKey != "" && gen.APIKey != callerAPIKey {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied: generation belongs to another key"})
		return
	}

	if gen.Status != aigc.StatusSucceeded {
		c.JSON(http.StatusBadRequest, gin.H{"error": "generation is not completed", "status": gen.Status})
		return
	}

	// Try extracting artifact URL
	var artifacts []aigcArtifact
	if len(gen.Output) > 0 {
		_ = json.Unmarshal(gen.Output, &artifacts)
	}

	if len(artifacts) > 0 && artifacts[0].URL != "" {
		c.Redirect(http.StatusFound, artifacts[0].URL)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": gen.ID, "output": gen.Output})
}

func (s *Server) handleAIGCAsyncCancel(c *gin.Context) {
	coordinator := s.getAIGCCoordinator()
	if coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "aigc service unavailable"})
		return
	}

	genID := strings.TrimSpace(c.Param("generation_id"))
	if genID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing generation_id parameter"})
		return
	}

	callerAPIKey := strings.TrimSpace(c.GetString("userApiKey"))
	if callerAPIKey != "" {
		genCheck, errCheck := coordinator.GetGeneration(c.Request.Context(), genID)
		if errCheck == nil && genCheck.APIKey != "" && genCheck.APIKey != callerAPIKey {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied: generation belongs to another key"})
			return
		}
	}

	gen, errCancel := coordinator.CancelGeneration(c.Request.Context(), genID)
	if errCancel != nil {
		if errors.Is(errCancel, aigc.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "generation not found"})
			return
		}
		if errors.Is(errCancel, aigc.ErrGenerationCompleted) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot cancel completed generation"})
			return
		}
		log.Warnf("aigc cancel failed for %s: %v", genID, errCancel)
		c.JSON(http.StatusInternalServerError, gin.H{"error": errCancel.Error()})
		return
	}
	if gen.ID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "generation not found"})
		return
	}

	c.JSON(http.StatusOK, formatAIGCResponse(gen))
}

func multipartFormToJSON(form *multipart.Form) ([]byte, error) {
	result := make(map[string]any)
	for k, values := range form.Value {
		if len(values) == 1 {
			result[k] = values[0]
		} else if len(values) > 1 {
			result[k] = values
		}
	}
	var images []string
	for _, fieldName := range []string{"images[]", "images", "image", "image[]"} {
		if files := form.File[fieldName]; len(files) > 0 {
			for _, fh := range files {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				data, errRead := io.ReadAll(f)
				_ = f.Close()
				if errRead == nil && len(data) > 0 {
					mimeType := http.DetectContentType(data)
					b64 := base64.StdEncoding.EncodeToString(data)
					images = append(images, fmt.Sprintf("data:%s;base64,%s", mimeType, b64))
				}
			}
		}
	}
	if len(images) > 0 {
		result["images"] = images
	}
	if maskFiles := form.File["mask"]; len(maskFiles) > 0 && maskFiles[0] != nil {
		f, err := maskFiles[0].Open()
		if err == nil {
			data, errRead := io.ReadAll(f)
			_ = f.Close()
			if errRead == nil && len(data) > 0 {
				mimeType := http.DetectContentType(data)
				b64 := base64.StdEncoding.EncodeToString(data)
				result["mask"] = fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
			}
		}
	}
	return json.Marshal(result)
}

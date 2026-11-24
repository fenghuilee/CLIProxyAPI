package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MoeForge/moego/base64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	internalaigc "github.com/router-for-me/CLIProxyAPI/v8/internal/aigc"
)

func (s *Server) aigcGenerateImageSyncHandler(c *gin.Context) {
	c.Set("aigc_image_operation", "generations")
	s.handleAIGCSync(c)
}

func (s *Server) aigcEditImageSyncHandler(c *gin.Context) {
	c.Set("aigc_image_operation", "edits")
	s.handleAIGCSync(c)
}

func (s *Server) handleAIGCSync(c *gin.Context) {
	pipeline := s.getAIGCSyncPipeline()
	if pipeline == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "aigc service unavailable (no plugin host)",
				"type":    "server_error",
			},
		})
		return
	}

	if _, ok := readAIGCCanonicalMetadata(c); !ok {
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	var bodyBytes []byte
	var errRead error

	if strings.HasPrefix(contentType, "multipart/form-data") {
		form, errForm := c.MultipartForm()
		if errForm != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf("invalid multipart form: %v", errForm),
					"type":    "invalid_request_error",
				},
			})
			return
		}
		bodyBytes, errRead = multipartFormToJSON(form)
		if errRead != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf("failed to parse multipart form: %v", errRead),
					"type":    "invalid_request_error",
				},
			})
			return
		}
	} else {
		bodyBytes, errRead = io.ReadAll(c.Request.Body)
		if errRead != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": "failed to read request body",
					"type":    "invalid_request_error",
				},
			})
			return
		}
	}

	model := strings.TrimSpace(ojson.GetBytes(bodyBytes, "model").String())
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "model is required",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	prompt := strings.TrimSpace(ojson.GetBytes(bodyBytes, "prompt").String())
	hasImages := ojson.GetBytes(bodyBytes, "images").Exists() ||
		ojson.GetBytes(bodyBytes, "image").Exists() ||
		ojson.GetBytes(bodyBytes, "input_reference").Exists() ||
		ojson.GetBytes(bodyBytes, "reference_images").Exists() ||
		ojson.GetBytes(bodyBytes, "content").Exists() ||
		ojson.GetBytes(bodyBytes, "text").Exists()

	if prompt == "" && !hasImages {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "prompt or image is required",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	desiredFormat := strings.ToLower(strings.TrimSpace(ojson.GetBytes(bodyBytes, "response_format").String()))
	if desiredFormat == "" {
		desiredFormat = "url"
	}

	callerAPIKey := strings.TrimSpace(c.GetString("userApiKey"))
	clientIP := c.ClientIP()
	reqID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if reqID == "" {
		reqID = strings.TrimSpace(c.GetString("request_id"))
	}
	var userID uint64
	if uStr := strings.TrimSpace(c.GetHeader("X-User-ID")); uStr != "" {
		userID, _ = strconv.ParseUint(uStr, 10, 64)
	}

	// Execute through SyncPipeline
	result, execErr := pipeline.Execute(c.Request.Context(), internalaigc.SyncImageRequest{
		RequestID:     reqID,
		UserID:        userID,
		Model:         model,
		Input:         bodyBytes,
		DesiredFormat: desiredFormat,
		APIKey:        callerAPIKey,
		ClientIP:      clientIP,
		Metadata:      map[string]any{"image_operation": c.GetString("aigc_image_operation")},
	})

	if execErr != nil {
		statusCode := execErr.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("%v", execErr.Error),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Format response items
	dataItems := make([]openAIImageItem, 0, len(result.Artifacts))
	for i, art := range result.Artifacts {
		revisedPrompt := ojson.GetBytes(result.RawBody, fmt.Sprintf("data.%d.revised_prompt", i)).String()
		if revisedPrompt == "" && art.Metadata != nil {
			if rp, ok := art.Metadata["revised_prompt"].(string); ok {
				revisedPrompt = rp
			}
		}
		if revisedPrompt == "" {
			revisedPrompt = ojson.GetBytes(bodyBytes, "prompt").String()
		}

		item := openAIImageItem{
			RevisedPrompt: revisedPrompt,
		}

		if desiredFormat == "b64_json" {
			if art.Metadata != nil {
				if b64, ok := art.Metadata["b64_json"].(string); ok && b64 != "" {
					item.B64JSON = b64
				}
			}
			if item.B64JSON == "" && strings.HasPrefix(art.URI, "data:") {
				parts := strings.Split(art.URI, ",")
				if len(parts) == 2 {
					item.B64JSON = parts[1]
				}
			}
			if item.B64JSON == "" && (strings.HasPrefix(art.URI, "http://") || strings.HasPrefix(art.URI, "https://")) {
				item.B64JSON = fetchImageURLToBase64(c.Request.Context(), art.URI)
			}
			if item.B64JSON == "" {
				item.URL = art.URI
			}
		} else {
			item.URL = art.URI
		}

		dataItems = append(dataItems, item)
	}

	// Fallback to RawOutput or RawBody if no artifacts produced
	if len(dataItems) == 0 {
		var outputData struct {
			Data []openAIImageItem `json:"data"`
		}
		if len(result.RawOutput) > 0 && json.Unmarshal(result.RawOutput, &outputData) == nil && len(outputData.Data) > 0 {
			dataItems = outputData.Data
		} else if len(result.RawBody) > 0 && json.Unmarshal(result.RawBody, &outputData) == nil && len(outputData.Data) > 0 {
			dataItems = outputData.Data
		}
		if desiredFormat == "b64_json" {
			for idx := range dataItems {
				if dataItems[idx].B64JSON == "" && (strings.HasPrefix(dataItems[idx].URL, "http://") || strings.HasPrefix(dataItems[idx].URL, "https://")) {
					if b64 := fetchImageURLToBase64(c.Request.Context(), dataItems[idx].URL); b64 != "" {
						dataItems[idx].B64JSON = b64
						dataItems[idx].URL = ""
					}
				}
			}
		}
	}

	var normUsage *ImageGenerationUsage
	if len(result.RawOutput) > 0 {
		normUsage = normalizeImageUsage(result.RawOutput)
	}
	if normUsage == nil && len(result.RawBody) > 0 {
		normUsage = normalizeImageUsage(result.RawBody)
	}

	respObj := openAIImageResponse{
		Created:      time.Now().Unix(),
		Data:         dataItems,
		Background:   ojson.GetBytes(bodyBytes, "background").String(),
		OutputFormat: desiredFormat,
		Quality:      ojson.GetBytes(bodyBytes, "quality").String(),
		Size:         ojson.GetBytes(bodyBytes, "size").String(),
		Model:        result.Model,
		Usage:        normUsage,
	}

	if result.ID != "" {
		c.Header("X-Generation-ID", result.ID)
	}

	c.JSON(http.StatusOK, respObj)
}

func fetchImageURLToBase64(ctx context.Context, imgURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return ""
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

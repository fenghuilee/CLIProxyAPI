package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"golang.org/x/sync/errgroup"
)

var dataURIRegex = regexp.MustCompile(`^data:([a-zA-Z0-9]+/[a-zA-Z0-9\-\+\.]+);base64,(.+)$`)

// Mutator intercepts generation lifecycle to handle both Ingress (Data URIs to TOS) and Egress (Base64 artifacts to TOS).
type Mutator struct {
	cfg          Config
	uploader     ObjectUploader
	inputPrefix  string
	outputPrefix string
}

// NewMutator creates a new Data URI to TOS mutator.
func NewMutator(cfg Config, uploader ObjectUploader) *Mutator {
	inPrefix := cfg.InputPrefix
	if inPrefix == "" {
		inPrefix = cfg.ObjectPrefix
	}
	if inPrefix == "" {
		inPrefix = "aigc/inputs"
	}

	outPrefix := cfg.OutputPrefix
	if outPrefix == "" {
		outPrefix = "aigc/outputs"
	}

	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = 20 * 1024 * 1024
	}
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = 50 * 1024 * 1024
	}
	return &Mutator{
		cfg:          cfg,
		uploader:     uploader,
		inputPrefix:  strings.Trim(inPrefix, "/"),
		outputPrefix: strings.Trim(outPrefix, "/"),
	}
}

// MutateContentGeneration implements aigc.ContentGenerationMutator.
func (m *Mutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch req.Phase {
	case aigc.PhaseBeforeCreate:
		return m.handleBeforeCreate(ctx, req)
	case aigc.PhaseBeforeComplete:
		return m.handleBeforeComplete(ctx, req)
	default:
		return aigc.GenerationMutationResponse{
			Draft:      req.Draft,
			Generation: &req.Generation,
			Artifacts:  req.Generation.Artifacts,
		}, nil
	}
}

// handleBeforeCreate processes Ingress: scans request input JSON and replaces Data URIs with TOS URLs.
func (m *Mutator) handleBeforeCreate(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Draft == nil || len(req.Draft.Input) == 0 || m.uploader == nil {
		return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
	}

	var root any
	if err := json.Unmarshal(req.Draft.Input, &root); err != nil {
		// If input is not valid JSON, pass through
		return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
	}

	urlCache := make(map[string]string)
	uploadedKeys := make([]string, 0)
	var totalBytes int64

	// Wrap the root so scalar JSON values can also be edited through Doc.
	doc := ojson.NewDocString(`{"value":` + string(req.Draft.Input) + `}`)
	errTransform := m.transformNode(ctx, doc, "value", root, urlCache, &uploadedKeys, &totalBytes, m.inputPrefix)
	if errTransform != nil {
		return aigc.GenerationMutationResponse{
			Reject:       true,
			ErrorCode:    aigc.ErrCodeAssetUploadFailed,
			ErrorMessage: errTransform.Error(),
		}, nil
	}

	newDraft := *req.Draft
	serialized := doc.Bytes()
	newDraft.Input = serialized[len(`{"value":`) : len(serialized)-1]
	if len(uploadedKeys) > 0 {
		if newDraft.Metadata == nil {
			newDraft.Metadata = make(map[string]any)
		}
		newDraft.Metadata["data-uri-to-tos"] = map[string]any{
			"objects": uploadedKeys,
		}
	}

	return aigc.GenerationMutationResponse{
		Draft: &newDraft,
	}, nil
}

// handleBeforeComplete processes Egress: scans generation artifacts and Output JSON, uploading Base64 to TOS.
func (m *Mutator) handleBeforeComplete(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if m.uploader == nil {
		return aigc.GenerationMutationResponse{
			Generation: &req.Generation,
			Artifacts:  req.Generation.Artifacts,
		}, nil
	}
	updatedGen := req.Generation
	artifacts := req.Generation.Artifacts
	if len(artifacts) == 0 && req.Metadata != nil {
		if rawArts, ok := req.Metadata["artifacts"].([]aigc.ContentGenerationArtifact); ok {
			artifacts = rawArts
		}
	}

	newArtifacts := make([]aigc.ContentGenerationArtifact, len(artifacts))
	var uploadedEgressKeys []string
	var egressMu sync.Mutex
	var g errgroup.Group
	g.SetLimit(4)

	for i, art := range artifacts {
		idx := i
		item := art
		g.Go(func() error {
			b64Data := ""
			if item.Metadata != nil {
				if b, ok := item.Metadata["b64_json"].(string); ok && b != "" {
					b64Data = b
				} else if b, ok := item.Metadata["image_base64"].(string); ok && b != "" {
					b64Data = b
				} else if b, ok := item.Metadata["base64"].(string); ok && b != "" {
					b64Data = b
				}
			}
			if b64Data == "" && strings.HasPrefix(item.URI, "data:") {
				b64Data = item.URI
			}

			if (item.StorageProvider == "inline" || strings.HasPrefix(item.URI, "data:") || strings.HasPrefix(item.URI, "inline://")) && b64Data != "" {
				tosURL, objKey, mimeType, sizeBytes, sha256Hex, errUp := m.uploadBase64Data(ctx, b64Data, m.outputPrefix)
				if errUp == nil && tosURL != "" {
					item.StorageProvider = "tos"
					item.URI = tosURL
					item.MIMEType = mimeType
					item.SizeBytes = sizeBytes
					item.SHA256 = sha256Hex
					if item.Metadata != nil {
						delete(item.Metadata, "b64_json")
						delete(item.Metadata, "image_base64")
						delete(item.Metadata, "image_b64")
						delete(item.Metadata, "base64")
						delete(item.Metadata, "file_base64")
					}
					if objKey != "" {
						egressMu.Lock()
						uploadedEgressKeys = append(uploadedEgressKeys, objKey)
						egressMu.Unlock()
					}
				}
			}
			newArtifacts[idx] = item
			return nil
		})
	}
	_ = g.Wait()
	updatedGen.Artifacts = newArtifacts

	// Keep one document across output mutation stages and serialize at the exit.
	var outputDoc *ojson.Doc
	if len(updatedGen.Output) > 0 {
		outputDoc = ojson.NewDocString(`{"value":` + string(updatedGen.Output) + `}`)
	}
	// Synchronize Output JSON:
	// If updatedGen.Output was the serialized artifact list from driver, update it with newArtifacts.
	var isArtifactList bool
	if outputDoc != nil && len(newArtifacts) > 0 {
		var rawArts []aigc.ContentGenerationArtifact
		if err := json.Unmarshal(updatedGen.Output, &rawArts); err == nil && len(rawArts) > 0 && rawArts[0].ArtifactType != "" {
			isArtifactList = true
			if errSet := outputDoc.Set("value", newArtifacts); errSet != nil {
				outputDoc = nil
			}
		}
	}

	// Transform Output JSON if present and not an artifact list (e.g. raw provider payload with Data URIs or b64_json)
	if outputDoc != nil && !isArtifactList {
		var root any
		if err := json.Unmarshal(updatedGen.Output, &root); err == nil {
			urlCache := make(map[string]string)
			var totalBytes int64
			if errT := m.transformNode(ctx, outputDoc, "value", root, urlCache, &uploadedEgressKeys, &totalBytes, m.outputPrefix); errT != nil {
				outputDoc = nil
			}
		} else {
			outputDoc = nil
		}
	}
	if outputDoc != nil {
		serialized := outputDoc.Bytes()
		updatedGen.Output = serialized[len(`{"value":`) : len(serialized)-1]
	}

	// Record uploaded egress objects into generation metadata
	if len(uploadedEgressKeys) > 0 {
		if updatedGen.Metadata == nil {
			updatedGen.Metadata = make(map[string]any)
		}
		if existing, ok := updatedGen.Metadata["data-uri-to-tos"].(map[string]any); ok {
			var objs []string
			if rawObjs, ok := existing["objects"].([]any); ok {
				for _, o := range rawObjs {
					if s, ok := o.(string); ok {
						objs = append(objs, s)
					}
				}
			} else if rawObjs, ok := existing["objects"].([]string); ok {
				objs = append(objs, rawObjs...)
			}
			objs = append(objs, uploadedEgressKeys...)
			existing["objects"] = objs
			updatedGen.Metadata["data-uri-to-tos"] = existing
		} else {
			updatedGen.Metadata["data-uri-to-tos"] = map[string]any{
				"objects": uploadedEgressKeys,
			}
		}
	}

	return aigc.GenerationMutationResponse{
		Generation: &updatedGen,
		Artifacts:  newArtifacts,
	}, nil
}

func (m *Mutator) uploadBase64Data(ctx context.Context, b64Str, prefix string) (string, string, string, int64, string, error) {
	rawB64 := strings.TrimSpace(b64Str)
	mimeType := "image/png"
	if matches := dataURIRegex.FindStringSubmatch(rawB64); len(matches) == 3 {
		mimeType = matches[1]
		rawB64 = matches[2]
	}

	data, errDecode := base64.StdEncoding.DecodeString(rawB64)
	if errDecode != nil {
		var errRaw error
		data, errRaw = base64.RawStdEncoding.DecodeString(rawB64)
		if errRaw != nil {
			return "", "", "", 0, "", fmt.Errorf("invalid base64: %w", errDecode)
		}
	}

	if detected := http.DetectContentType(data); detected != "application/octet-stream" && strings.HasPrefix(detected, "image/") {
		mimeType = detected
	}

	ext := mimeToExtension(mimeType)
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])
	dateFolder := time.Now().Format("20060102")

	key := path.Join(prefix, dateFolder, fmt.Sprintf("%s%s", hashHex, ext))

	uploadedURL, errUpload := m.uploader.Upload(ctx, key, mimeType, data)
	if errUpload != nil {
		return "", "", "", 0, "", fmt.Errorf("upload to tos (%s): %w", key, errUpload)
	}

	return uploadedURL, key, mimeType, int64(len(data)), hashHex, nil
}

func (m *Mutator) transformNode(ctx context.Context, doc *ojson.Doc, jsonPath string, node any, urlCache map[string]string, uploadedKeys *[]string, totalBytes *int64, prefix string) error {
	switch v := node.(type) {
	case string:
		transformed, err := m.processString(ctx, v, urlCache, uploadedKeys, totalBytes, prefix)
		if err != nil {
			return err
		}
		if transformed != v {
			return doc.Set(jsonPath, transformed)
		}
	case []any:
		for i, item := range v {
			if err := m.transformNode(ctx, doc, fmt.Sprintf("%s.%d", jsonPath, i), item, urlCache, uploadedKeys, totalBytes, prefix); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, item := range v {
			childPath := jsonPath + ".:" + ojson.Escape(k)
			lowerK := strings.ToLower(k)
			if lowerK == "b64_json" || lowerK == "image_base64" || lowerK == "image_b64" || lowerK == "file_base64" {
				if s, ok := item.(string); ok && len(s) > 0 {
					if existingURL, okURL := v["url"].(string); okURL && (strings.HasPrefix(existingURL, "http://") || strings.HasPrefix(existingURL, "https://")) {
						if err := doc.Delete(childPath); err != nil {
							return err
						}
						continue
					}
					tosURL, objKey, _, _, _, errUp := m.uploadBase64Data(ctx, s, prefix)
					if errUp == nil && tosURL != "" {
						if err := doc.Set(jsonPath+".url", tosURL); err != nil {
							return err
						}
						if err := doc.Delete(childPath); err != nil {
							return err
						}
						if uploadedKeys != nil && objKey != "" {
							*uploadedKeys = append(*uploadedKeys, objKey)
						}
						continue
					}
				}
			}
			if err := m.transformNode(ctx, doc, childPath, item, urlCache, uploadedKeys, totalBytes, prefix); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Mutator) processString(ctx context.Context, s string, urlCache map[string]string, uploadedKeys *[]string, totalBytes *int64, prefix string) (string, error) {
	matches := dataURIRegex.FindStringSubmatch(s)
	if len(matches) != 3 {
		return s, nil
	}

	mimeType := strings.ToLower(matches[1])
	base64Data := matches[2]

	data, errDecode := base64.StdEncoding.DecodeString(base64Data)
	if errDecode != nil {
		var errRaw error
		data, errRaw = base64.RawStdEncoding.DecodeString(base64Data)
		if errRaw != nil {
			return "", fmt.Errorf("invalid base64 in data URI: %w", errDecode)
		}
	}

	dataLen := int64(len(data))
	if dataLen > m.cfg.MaxImageBytes {
		return "", fmt.Errorf("media size %d exceeds max allowed %d bytes", dataLen, m.cfg.MaxImageBytes)
	}

	*totalBytes += dataLen
	if *totalBytes > m.cfg.MaxTotalBytes {
		return "", fmt.Errorf("total media size %d exceeds max allowed %d bytes", *totalBytes, m.cfg.MaxTotalBytes)
	}

	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	if cachedURL, ok := urlCache[hashHex]; ok {
		return cachedURL, nil
	}

	ext := mimeToExtension(mimeType)
	dateFolder := time.Now().Format("20060102")
	key := path.Join(prefix, dateFolder, fmt.Sprintf("%s%s", hashHex, ext))

	uploadedURL, errUpload := m.uploader.Upload(ctx, key, mimeType, data)
	if errUpload != nil {
		return "", fmt.Errorf("upload to tos (%s): %w", key, errUpload)
	}

	urlCache[hashHex] = uploadedURL
	*uploadedKeys = append(*uploadedKeys, key)

	return uploadedURL, nil
}

func mimeToExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	default:
		return ".bin"
	}
}

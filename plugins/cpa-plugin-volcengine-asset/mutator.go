package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

// Mutator handles Volcengine Ark Asset transformation during generation lifecycle.
type Mutator struct {
	cfg    Config
	client ArkAssetClient
	store  *AssetStore
}

// NewMutator creates a new Volcengine Asset Mutator.
func NewMutator(cfg Config, client ArkAssetClient, store *AssetStore) *Mutator {
	return &Mutator{
		cfg:    cfg,
		client: client,
		store:  store,
	}
}

// MutateContentGeneration intercepts lifecycle phases to create and await Active status for Volcengine assets.
func (m *Mutator) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch req.Phase {
	case aigc.PhaseBeforeCreate:
		return m.handleBeforeCreate(ctx, req)
	case aigc.PhaseBeforeSubmit:
		return m.handleBeforeSubmit(ctx, req)
	default:
		return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
	}
}

func (m *Mutator) handleBeforeCreate(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Draft == nil || len(req.Draft.Input) == 0 {
		return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
	}

	model := req.Draft.Model
	if model == "" {
		model = gjson.GetBytes(req.Draft.Input, "model").String()
	}
	if !m.cfg.MatchesModel(model) {
		return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
	}

	var root any
	if err := json.Unmarshal(req.Draft.Input, &root); err != nil {
		return aigc.GenerationMutationResponse{Draft: req.Draft}, nil
	}

	urlCache := make(map[string]string)
	var assetIDs []string

	transformed, errTransform := m.transformNode(ctx, "", root, urlCache, &assetIDs)
	if errTransform != nil {
		return aigc.GenerationMutationResponse{
			Reject:       true,
			ErrorCode:    aigc.ErrCodeAssetUploadFailed,
			ErrorMessage: errTransform.Error(),
		}, nil
	}

	newInput, errMarshal := json.Marshal(transformed)
	if errMarshal != nil {
		return aigc.GenerationMutationResponse{
			Reject:       true,
			ErrorCode:    aigc.ErrCodeInvalidParameter,
			ErrorMessage: errMarshal.Error(),
		}, nil
	}

	newDraft := *req.Draft
	newDraft.Input = newInput
	if len(assetIDs) > 0 {
		if newDraft.Metadata == nil {
			newDraft.Metadata = make(map[string]any)
		}
		newDraft.Metadata["volcengine-asset"] = map[string]any{
			"asset_ids": assetIDs,
		}
	}

	return aigc.GenerationMutationResponse{
		Draft: &newDraft,
	}, nil
}

func (m *Mutator) handleBeforeSubmit(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	model := req.Generation.Model
	if !m.cfg.MatchesModel(model) {
		return aigc.GenerationMutationResponse{}, nil
	}

	assetIDs := m.extractAssetIDs(req.Generation)
	if len(assetIDs) == 0 {
		return aigc.GenerationMutationResponse{}, nil
	}

	timeout := m.cfg.PollTimeout()
	interval := m.cfg.PollInterval()

	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		allActive, errCheck := m.checkAndRefreshAssets(ctxTimeout, assetIDs)
		if errCheck != nil {
			return aigc.GenerationMutationResponse{
				Reject:       true,
				ErrorCode:    aigc.ErrCodeAssetUploadFailed,
				ErrorMessage: errCheck.Error(),
			}, nil
		}
		if allActive {
			return aigc.GenerationMutationResponse{}, nil
		}

		select {
		case <-ctxTimeout.Done():
			return aigc.GenerationMutationResponse{
				Reject:       true,
				ErrorCode:    aigc.ErrCodeAssetProcessTimeout,
				ErrorMessage: fmt.Sprintf("timed out waiting for volcengine assets to become active (timeout: %v)", timeout),
			}, nil
		case <-ticker.C:
		}
	}
}

func (m *Mutator) checkAndRefreshAssets(ctx context.Context, assetIDs []string) (bool, error) {
	allActive := true
	for _, id := range assetIDs {
		// 1. Check Store first
		if m.store != nil {
			if rec, found, _ := m.store.GetByID(ctx, id); found && rec != nil {
				if strings.EqualFold(rec.Status, "Active") {
					continue
				}
				if strings.EqualFold(rec.Status, "Failed") {
					return false, fmt.Errorf("asset %s processing failed: %s", id, rec.ErrorMessage)
				}
			}
		}

		// 2. Query Ark API
		if m.client == nil {
			continue
		}
		item, errGet := m.client.GetAsset(ctx, id)
		if errGet != nil {
			// Network error during poll: continue polling if not terminal
			allActive = false
			continue
		}

		if strings.EqualFold(item.Status, "Active") {
			if m.store != nil {
				_ = m.store.UpdateStatus(ctx, id, "Active", "")
			}
		} else if strings.EqualFold(item.Status, "Failed") {
			if m.store != nil {
				_ = m.store.UpdateStatus(ctx, id, "Failed", item.ErrorMessage)
			}
			return false, fmt.Errorf("asset %s processing failed: %s", id, item.ErrorMessage)
		} else {
			allActive = false
		}
	}
	return allActive, nil
}

func (m *Mutator) transformNode(ctx context.Context, key string, node any, urlCache map[string]string, assetIDs *[]string) (any, error) {
	switch v := node.(type) {
	case string:
		return m.processString(ctx, key, v, urlCache, assetIDs)
	case []any:
		res := make([]any, len(v))
		for i, item := range v {
			t, err := m.transformNode(ctx, key, item, urlCache, assetIDs)
			if err != nil {
				return nil, err
			}
			res[i] = t
		}
		return res, nil
	case map[string]any:
		res := make(map[string]any, len(v))
		for k, val := range v {
			t, err := m.transformNode(ctx, k, val, urlCache, assetIDs)
			if err != nil {
				return nil, err
			}
			res[k] = t
		}
		return res, nil
	default:
		return node, nil
	}
}

func (m *Mutator) processString(ctx context.Context, key, str string, urlCache map[string]string, assetIDs *[]string) (string, error) {
	strTrim := strings.TrimSpace(str)
	if !isHTTPURL(strTrim) {
		// If it's already an asset URI, record its asset ID for status polling
		if strings.HasPrefix(strTrim, "asset://") {
			*assetIDs = appendUnique(*assetIDs, strings.TrimPrefix(strTrim, "asset://"))
		}
		return str, nil
	}

	// Check local transformation cache for this request
	if cachedURI, ok := urlCache[strTrim]; ok {
		*assetIDs = appendUnique(*assetIDs, strings.TrimPrefix(cachedURI, "asset://"))
		return cachedURI, nil
	}

	hash := sha256Hex(strTrim)
	fileType := inferFileType(key, strTrim)

	// Check DB / Memory Store
	if m.store != nil {
		if rec, found, _ := m.store.GetByHash(ctx, hash); found && rec != nil && rec.AssetID != "" {
			assetURI := "asset://" + rec.AssetID
			urlCache[strTrim] = assetURI
			*assetIDs = appendUnique(*assetIDs, rec.AssetID)
			return assetURI, nil
		}
	}

	// Create Asset on Ark
	if m.client == nil {
		return str, nil
	}

	assetName := fmt.Sprintf("asset-%s", hash[:12])
	item, errCreate := m.client.CreateAsset(ctx, assetName, fileType, strTrim)
	if errCreate != nil {
		return "", fmt.Errorf("create volcengine asset for %s: %w", strTrim, errCreate)
	}

	// Save into store
	if m.store != nil {
		_ = m.store.Save(ctx, &AssetRecord{
			URLSHA256:   hash,
			OriginalURL: strTrim,
			AssetID:     item.ID,
			FileType:    fileType,
			Status:      item.Status,
		})
	}

	assetURI := "asset://" + item.ID
	urlCache[strTrim] = assetURI
	*assetIDs = appendUnique(*assetIDs, item.ID)
	return assetURI, nil
}

func (m *Mutator) extractAssetIDs(gen aigc.ContentGeneration) []string {
	var ids []string
	if meta, ok := gen.Metadata["volcengine-asset"].(map[string]any); ok {
		if rawIDs, ok := meta["asset_ids"].([]any); ok {
			for _, id := range rawIDs {
				if idStr, ok := id.(string); ok && idStr != "" {
					ids = appendUnique(ids, idStr)
				}
			}
		} else if rawIDs, ok := meta["asset_ids"].([]string); ok {
			for _, idStr := range rawIDs {
				if idStr != "" {
					ids = appendUnique(ids, idStr)
				}
			}
		}
	}

	if len(ids) > 0 {
		return ids
	}

	// Fallback: extract any string starting with "asset-" from gen.Input or gen.PreparedInput
	inputBytes := gen.Input
	if len(gen.PreparedInput) > 0 {
		inputBytes = gen.PreparedInput
	}
	var root any
	if err := json.Unmarshal(inputBytes, &root); err == nil {
		scanAssetIDs(root, &ids)
	}

	return ids
}

func scanAssetIDs(node any, ids *[]string) {
	switch v := node.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(v), "asset-") {
			*ids = appendUnique(*ids, strings.TrimSpace(v))
		}
	case []any:
		for _, item := range v {
			scanAssetIDs(item, ids)
		}
	case map[string]any:
		for _, val := range v {
			scanAssetIDs(val, ids)
		}
	}
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func inferFileType(key, rawURL string) string {
	keyLower := strings.ToLower(key)
	if strings.Contains(keyLower, "video") {
		return "video"
	}
	if strings.Contains(keyLower, "audio") {
		return "audio"
	}

	parsed, err := url.Parse(rawURL)
	if err == nil {
		ext := strings.ToLower(path.Ext(parsed.Path))
		switch ext {
		case ".mp4", ".mov", ".avi", ".mkv", ".webm":
			return "video"
		case ".mp3", ".wav", ".aac", ".m4a", ".ogg", ".flac":
			return "audio"
		}
	}
	return "image"
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func appendUnique(slice []string, val string) []string {
	for _, item := range slice {
		if item == val {
			return slice
		}
	}
	return append(slice, val)
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
	"github.com/volcengine/volcengine-go-sdk/volcengine/universal"
)

// AssetItem represents an asset created or queried on Volcengine Ark.
type AssetItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	FileType     string `json:"file_type"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// ArkAssetClient defines the contract for interacting with Volcengine Ark Asset OpenAPI.
type ArkAssetClient interface {
	CreateAsset(ctx context.Context, name, fileType, url string) (AssetItem, error)
	GetAsset(ctx context.Context, assetID string) (AssetItem, error)
}

// VolcSDKArkAssetClient is the production client using official volcengine-go-sdk.
type VolcSDKArkAssetClient struct {
	accessKey string
	secretKey string
	region    string
	project   string
	groupID   string
	universal *universal.Universal
}

// NewVolcSDKArkAssetClient creates a new VolcSDKArkAssetClient.
func NewVolcSDKArkAssetClient(cfg Config) (*VolcSDKArkAssetClient, error) {
	cfg.Normalize()
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("volcengine asset client requires access_key and secret_key")
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("volcengine asset client requires group_id")
	}

	vCfg := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, "")).
		WithRegion(cfg.Region)

	sess, errSess := session.NewSession(vCfg)
	if errSess != nil {
		return nil, fmt.Errorf("create volcengine session: %w", errSess)
	}

	u := universal.New(sess)
	return &VolcSDKArkAssetClient{
		accessKey: cfg.AccessKey,
		secretKey: cfg.SecretKey,
		region:    cfg.Region,
		project:   cfg.Project,
		groupID:   cfg.GroupID,
		universal: u,
	}, nil
}

// CreateAsset registers an external URL as an asset in Volcengine Ark.
func (c *VolcSDKArkAssetClient) CreateAsset(_ context.Context, name, fileType, urlStr string) (AssetItem, error) {
	if c == nil || c.universal == nil {
		return AssetItem{}, fmt.Errorf("volcengine sdk client is not initialized")
	}
	if c.groupID == "" {
		return AssetItem{}, fmt.Errorf("group_id is not configured")
	}

	normType := normalizeAssetType(fileType)
	if name == "" {
		name = "asset-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}

	payload := map[string]any{
		"GroupId":     c.groupID,
		"URL":         urlStr,
		"AssetType":   normType,
		"ProjectName": c.project,
		"Name":        name,
	}

	resp, errCall := c.universal.DoCall(
		universal.RequestUniversal{
			ServiceName: "ark",
			Action:      "CreateAsset",
			Version:     "2024-01-01",
			HttpMethod:  universal.POST,
			ContentType: universal.ApplicationJSON,
		},
		&payload,
	)
	if errCall != nil {
		return AssetItem{}, fmt.Errorf("ark create asset failed: %w", errCall)
	}

	respBytes, errMarshal := json.Marshal(resp)
	if errMarshal != nil {
		return AssetItem{}, fmt.Errorf("marshal ark create asset response: %w", errMarshal)
	}

	assetID := gjson.GetBytes(respBytes, "Result.Id").String()
	if assetID == "" {
		assetID = gjson.GetBytes(respBytes, "Id").String()
	}
	if assetID == "" {
		errMsg := gjson.GetBytes(respBytes, "ResponseMetadata.Error.Message").String()
		if errMsg == "" {
			errMsg = string(respBytes)
		}
		return AssetItem{}, fmt.Errorf("ark create asset returned no id: %s", errMsg)
	}

	return AssetItem{
		ID:       assetID,
		Name:     name,
		FileType: fileType,
		Status:   "Processing",
	}, nil
}

// GetAsset queries the status and details of an asset from Volcengine Ark.
func (c *VolcSDKArkAssetClient) GetAsset(_ context.Context, assetID string) (AssetItem, error) {
	if c == nil || c.universal == nil {
		return AssetItem{}, fmt.Errorf("volcengine sdk client is not initialized")
	}

	payload := map[string]any{
		"Id":          assetID,
		"ProjectName": c.project,
	}

	resp, errCall := c.universal.DoCall(
		universal.RequestUniversal{
			ServiceName: "ark",
			Action:      "GetAsset",
			Version:     "2024-01-01",
			HttpMethod:  universal.POST,
			ContentType: universal.ApplicationJSON,
		},
		&payload,
	)
	if errCall != nil {
		return AssetItem{}, fmt.Errorf("ark get asset failed: %w", errCall)
	}

	respBytes, errMarshal := json.Marshal(resp)
	if errMarshal != nil {
		return AssetItem{}, fmt.Errorf("marshal ark get asset response: %w", errMarshal)
	}

	status := gjson.GetBytes(respBytes, "Result.Status").String()
	if status == "" {
		status = gjson.GetBytes(respBytes, "Status").String()
	}
	if status == "" {
		errMsg := gjson.GetBytes(respBytes, "ResponseMetadata.Error.Message").String()
		if errMsg == "" {
			errMsg = string(respBytes)
		}
		return AssetItem{}, fmt.Errorf("ark get asset returned unknown status: %s", errMsg)
	}

	name := gjson.GetBytes(respBytes, "Result.Name").String()
	if name == "" {
		name = gjson.GetBytes(respBytes, "Name").String()
	}

	assetType := gjson.GetBytes(respBytes, "Result.AssetType").String()
	if assetType == "" {
		assetType = gjson.GetBytes(respBytes, "AssetType").String()
	}

	errMsg := gjson.GetBytes(respBytes, "Result.ErrorMessage").String()
	if errMsg == "" {
		errMsg = gjson.GetBytes(respBytes, "ErrorMessage").String()
	}

	return AssetItem{
		ID:           assetID,
		Name:         name,
		FileType:     strings.ToLower(assetType),
		Status:       status,
		ErrorMessage: errMsg,
	}, nil
}

func normalizeAssetType(fileType string) string {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "image":
		return "Image"
	case "video":
		return "Video"
	case "audio":
		return "Audio"
	default:
		return "Image"
	}
}

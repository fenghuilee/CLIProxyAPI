package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const chinaDataPayAssetEndpoint = "https://tp-api.chinadatapay.com:8000/v1/api/assets/"

// ChinaDataPayAdapter owns its platform Token and native Action requests.
type ChinaDataPayAdapter struct {
	endpoint       string
	token, groupID string
	httpClient     *http.Client
}

func NewChinaDataPayAdapter(cfg ChinaDataPayConfig) (*ChinaDataPayAdapter, error) {
	cfg.normalize()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &ChinaDataPayAdapter{
		endpoint: chinaDataPayAssetEndpoint, token: strings.TrimSpace(cfg.Token), groupID: cfg.GroupID,
		httpClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *ChinaDataPayAdapter) call(ctx context.Context, action string, payload map[string]any) (AssetItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return AssetItem{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AssetItem{}, &AssetAPIError{Code: "invalid_request"}
	}
	query := url.Values{"Action": {action}, "Version": {"2024-01-01"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return AssetItem{}, &AssetAPIError{Code: "invalid_request"}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return AssetItem{}, ctx.Err()
		}
		return AssetItem{}, &AssetAPIError{Status: 503, Code: "transport_failure"}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AssetItem{}, &AssetAPIError{Status: resp.StatusCode, Code: "http_error"}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		if ctx.Err() != nil {
			return AssetItem{}, ctx.Err()
		}
		return AssetItem{}, &AssetAPIError{Status: 503, Code: "transport_failure"}
	}
	if len(raw) > 1<<20 {
		return AssetItem{}, &AssetAPIError{Status: resp.StatusCode, Code: "invalid_response"}
	}
	var envelope struct {
		ResponseMetadata struct{ Error *json.RawMessage }
		Result           struct {
			ID                                    string `json:"Id"`
			Status, Name, AssetType, ErrorMessage string
		}
		Data struct {
			ID                                    string `json:"Id"`
			Status, Name, AssetType, ErrorMessage string
		} `json:"data"`
		Code int `json:"code"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return AssetItem{}, &AssetAPIError{Status: resp.StatusCode, Code: "invalid_response"}
	}
	if envelope.ResponseMetadata.Error != nil || envelope.Code != 0 {
		return AssetItem{}, &AssetAPIError{Status: resp.StatusCode, Code: "platform_error"}
	}
	result := envelope.Result
	if result.ID == "" && result.Status == "" {
		result = envelope.Data
	}
	if action == "CreateAsset" && result.ID != "" && result.Status == "" {
		result.Status = "Processing"
	}
	if result.Status == "" || (action == "CreateAsset" && result.ID == "") {
		return AssetItem{}, &AssetAPIError{Status: resp.StatusCode, Code: "missing_id_or_status"}
	}
	return AssetItem{ID: result.ID, Status: result.Status, Name: result.Name, FileType: strings.ToLower(result.AssetType), ErrorMessage: result.ErrorMessage}, nil
}

func (c *ChinaDataPayAdapter) CreateAsset(ctx context.Context, name, fileType, sourceURL string) (AssetItem, error) {
	payload := map[string]any{"URL": sourceURL, "AssetType": normalizeAssetType(fileType), "Name": name}
	if c.groupID != "" {
		payload["GroupId"] = c.groupID
	}
	item, err := c.call(ctx, "CreateAsset", payload)
	item.FileType = fileType
	return item, err
}

func (c *ChinaDataPayAdapter) GetAsset(ctx context.Context, id string) (AssetItem, error) {
	item, err := c.call(ctx, "GetAsset", map[string]any{"Id": id})
	if err != nil {
		return AssetItem{}, err
	}
	if item.ID != "" && item.ID != id {
		return AssetItem{}, &AssetAPIError{Status: 200, Code: "asset_id_mismatch"}
	}
	if item.ID == "" {
		item.ID = id
	}
	return item, nil
}

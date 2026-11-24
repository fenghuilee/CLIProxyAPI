package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
	"github.com/volcengine/volcengine-go-sdk/volcengine/universal"
	"github.com/volcengine/volcengine-go-sdk/volcengine/volcengineerr"
)

// arkSDKClient shares Ark protocol code, but each adapter owns its SDK session.
type arkSDKClient struct {
	accessKey, region, project, groupID string
	session                             *session.Session
}

func newArkSDKClient(cfg ChannelConfig, region, endpoint string) (*arkSDKClient, error) {
	vCfg := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey, "")).
		WithRegion(region).
		WithMaxRetries(0).
		WithHTTPClient(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	if endpoint != "" {
		vCfg.Endpoint = volcengine.String(endpoint)
		vCfg.DisableRestProtocolURICleaning = volcengine.Bool(true)
	}
	sess, err := session.NewSession(vCfg)
	if err != nil {
		return nil, fmt.Errorf("initialize Ark SDK session")
	}
	return &arkSDKClient{accessKey: cfg.AccessKey, region: region, project: cfg.Project, groupID: cfg.GroupID, session: sess}, nil
}

func (c *arkSDKClient) call(ctx context.Context, action string, payload map[string]any) (AssetItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return AssetItem{}, err
	}
	// This SDK version has no DoCallWithContext; attach context to a per-call copy.
	sess := c.session.Copy()
	sess.Handlers.Build.PushFrontNamed(request.NamedHandler{Name: "asset.context", Fn: func(r *request.Request) { r.SetContext(ctx) }})
	status := 0
	sess.Handlers.Complete.PushBackNamed(request.NamedHandler{Name: "asset.status", Fn: func(r *request.Request) {
		if r.HTTPResponse != nil {
			status = r.HTTPResponse.StatusCode
		}
	}})
	resp, err := universal.New(sess).DoCall(universal.RequestUniversal{
		ServiceName: "ark", Action: action, Version: "2024-01-01",
		HttpMethod: universal.POST, ContentType: universal.ApplicationJSON,
	}, &payload)
	if status >= 300 {
		return AssetItem{}, &AssetAPIError{Status: status, Code: "http_error"}
	}
	if err != nil {
		if ctx.Err() != nil {
			return AssetItem{}, ctx.Err()
		}
		var failure volcengineerr.RequestFailure
		if errors.As(err, &failure) {
			return AssetItem{}, &AssetAPIError{Status: failure.StatusCode(), Code: "upstream_error"}
		}
		if status > 0 {
			return AssetItem{}, &AssetAPIError{Status: status, Code: "invalid_response"}
		}
		var sdkErr volcengineerr.Error
		if errors.As(err, &sdkErr) && sdkErr.Code() != "RequestError" {
			return AssetItem{}, &AssetAPIError{Code: "invalid_response"}
		}
		return AssetItem{}, &AssetAPIError{Status: 503, Code: "transport_failure"}
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return AssetItem{}, &AssetAPIError{Code: "invalid_response"}
	}
	if ojson.GetBytes(raw, "ResponseMetadata.Error").Exists() && ojson.GetBytes(raw, "ResponseMetadata.Error").Type != ojson.Null {
		return AssetItem{}, &AssetAPIError{Status: 200, Code: "platform_error"}
	}
	result := ojson.GetBytes(raw, "Result")
	if !result.Exists() {
		result = ojson.ParseBytes(raw)
	}
	item := AssetItem{ID: result.Get("Id").String(), Name: result.Get("Name").String(),
		Status: result.Get("Status").String(), FileType: strings.ToLower(result.Get("AssetType").String()), ErrorMessage: result.Get("ErrorMessage").String()}
	if action == "CreateAsset" && item.ID != "" && item.Status == "" {
		item.Status = "Processing"
	}
	if item.Status == "" || (action == "CreateAsset" && item.ID == "") {
		return AssetItem{}, &AssetAPIError{Status: 200, Code: "missing_id_or_status"}
	}
	return item, nil
}

func (c *arkSDKClient) CreateAsset(ctx context.Context, name, fileType, sourceURL string) (AssetItem, error) {
	item, err := c.call(ctx, "CreateAsset", map[string]any{
		"GroupId": c.groupID, "URL": sourceURL, "AssetType": normalizeAssetType(fileType), "Name": name, "ProjectName": c.project,
	})
	item.FileType = fileType
	return item, err
}

func (c *arkSDKClient) GetAsset(ctx context.Context, id string) (AssetItem, error) {
	item, err := c.call(ctx, "GetAsset", map[string]any{"Id": id, "ProjectName": c.project})
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

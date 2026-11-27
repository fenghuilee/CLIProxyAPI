package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/json"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
)

func TestVolcengineSDKOwnsAccountAndRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=direct-ak/") || !strings.Contains(r.Header.Get("Authorization"), "/cn-shanghai/ark/request") {
			t.Error("direct signing credentials/region lost")
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(`{}`), &body); err != nil {
			t.Error(err)
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil {
			t.Error(err)
		}
		if body["ProjectName"] != "direct-project" || body["GroupId"] != "direct-group" {
			t.Error("direct project/group lost")
		}
		_, _ = w.Write([]byte(`{"Result":{"Id":"asset-direct"}}`))
	}))
	defer server.Close()
	client, err := NewVolcengineAdapter(VolcengineConfig{ChannelConfig: ChannelConfig{AccessKey: "direct-ak", SecretKey: "direct-sk", Project: "direct-project", GroupID: "direct-group"}, Region: "cn-shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	client.session.Config.Endpoint = volcengine.String(server.URL)
	item, err := client.CreateAsset(context.Background(), "test", "audio", "https://example.com/audio")
	if err != nil || item.Status != "Processing" || item.ID != "asset-direct" || item.FileType != "audio" {
		t.Fatalf("direct creation: %+v %v", item, err)
	}
}

func TestVolcengineSDKPropagatesDetailedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-123","Action":"CreateAsset","Version":"2024-01-01","Service":"ark","Region":"cn-beijing","Error":{"Code":"InvalidParameter.DownloadFailed","Message":"Failed to download media from URL with key direct-ak and secret direct-sk"}}}`))
	}))
	defer server.Close()
	client, err := NewVolcengineAdapter(VolcengineConfig{ChannelConfig: ChannelConfig{AccessKey: "direct-ak", SecretKey: "direct-sk", Project: "direct-project", GroupID: "direct-group"}, Region: "cn-beijing"})
	if err != nil {
		t.Fatal(err)
	}
	client.session.Config.Endpoint = volcengine.String(server.URL)
	_, err = client.CreateAsset(context.Background(), "test", "image", "https://example.com/image.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *AssetAPIError, got %T: %v", err, err)
	}
	if apiErr.Status != 400 {
		t.Fatalf("expected status 400, got %d", apiErr.Status)
	}
	if apiErr.Code != "InvalidParameter.DownloadFailed" {
		t.Fatalf("expected code InvalidParameter.DownloadFailed, got %s", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "Failed to download media") {
		t.Fatalf("expected message to contain 'Failed to download media', got %q", apiErr.Message)
	}
	if strings.Contains(err.Error(), "direct-ak") || strings.Contains(err.Error(), "direct-sk") {
		t.Fatalf("credentials leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in error string: %v", err)
	}
}

func TestVolcengineSDKPlatformErrorOn200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"req-456","Error":{"Code":"ResourceNotFound","Message":"Asset group not found"}}}`))
	}))
	defer server.Close()
	client, err := NewVolcengineAdapter(VolcengineConfig{ChannelConfig: ChannelConfig{AccessKey: "direct-ak", SecretKey: "direct-sk", Project: "direct-project", GroupID: "direct-group"}, Region: "cn-beijing"})
	if err != nil {
		t.Fatal(err)
	}
	client.session.Config.Endpoint = volcengine.String(server.URL)
	_, err = client.CreateAsset(context.Background(), "test", "image", "https://example.com/image.png")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *AssetAPIError, got %T: %v", err, err)
	}
	if apiErr.Status != 200 {
		t.Fatalf("expected status 200, got %d", apiErr.Status)
	}
	if apiErr.Code != "ResourceNotFound" {
		t.Fatalf("expected code ResourceNotFound, got %s", apiErr.Code)
	}
	if apiErr.Message != "Asset group not found" {
		t.Fatalf("expected message 'Asset group not found', got %q", apiErr.Message)
	}
}

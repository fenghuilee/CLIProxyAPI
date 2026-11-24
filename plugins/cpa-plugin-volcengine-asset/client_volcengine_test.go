package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-go-sdk/volcengine"
)

func TestVolcengineSDKOwnsAccountAndRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=direct-ak/") || !strings.Contains(r.Header.Get("Authorization"), "/cn-shanghai/ark/request") {
			t.Error("direct signing credentials/region lost")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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

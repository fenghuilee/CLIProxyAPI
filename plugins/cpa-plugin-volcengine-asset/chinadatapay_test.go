package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
)

func testChinaDataPayConfig() ChinaDataPayConfig {
	return ChinaDataPayConfig{Token: "test-token", GroupID: "test-group"}
}

func localChinaDataPayAdapter(t *testing.T, endpoint string) *ChinaDataPayAdapter {
	t.Helper()
	adapter, err := NewChinaDataPayAdapter(testChinaDataPayConfig())
	if err != nil {
		t.Fatal(err)
	}
	if adapter.endpoint != chinaDataPayAssetEndpoint {
		t.Fatal("gateway endpoint is not fixed")
	}
	// Only tests override the endpoint; public config has no address option.
	adapter.endpoint = endpoint + "/v1/api/assets/"
	return adapter
}

func TestChinaDataPayCreationDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"ServiceUnavailable","Message":"private-upstream-details"}}}`))
	}))
	defer server.Close()
	client := localChinaDataPayAdapter(t, server.URL)
	_, err := client.CreateAsset(context.Background(), "test", "image", "https://example.com/image.png")
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 502 || calls.Load() != 1 {
		t.Fatalf("creation retried or error lost: calls=%d error=%v", calls.Load(), err)
	}
	if strings.Contains(err.Error(), "private-upstream-details") {
		t.Fatal("upstream response details leaked")
	}
}

func TestChinaDataPayContextCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)
	client := localChinaDataPayAdapter(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.GetAsset(ctx, "asset-test"); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Action request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Action request ignored lifecycle cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Action request did not stop")
	}
}

func TestChinaDataPayDoesNotFollowRedirects(t *testing.T) {
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := localChinaDataPayAdapter(t, server.URL).GetAsset(context.Background(), "asset-test")
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 307 || forwarded.Load() != 0 {
		t.Fatalf("Bearer request followed redirect: %v", err)
	}
}

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

func TestChinaDataPayActionClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth := r.Header.Get("Authorization")
		if r.Method != "POST" || auth != "Bearer test-token" || r.Header.Get("X-Date") != "" || r.Header.Get("Content-Type") != "application/json" || r.URL.Query().Get("Version") != "2024-01-01" || r.URL.Path != "/v1/api/assets/" {
			t.Errorf("incorrect Bearer Action request")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, ok := payload["model"]; ok {
			t.Error("unexpected model")
		}
		if _, exists := payload["ProjectName"]; exists {
			t.Error("project must be owned by the server")
		}
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			if payload["AssetType"] != "Video" || payload["URL"] != "https://example.com/video" {
				t.Errorf("wrong create payload: %v", payload)
			}
			if payload["GroupId"] != "test-group" {
				t.Error("missing asset group")
			}
		case "GetAsset":
			if payload["Id"] != "asset-relay" || len(payload) != 1 {
				t.Error("incorrect asset id")
			}
		default:
			t.Error("incorrect action")
		}
		_, _ = w.Write([]byte(`{"Result":{"Id":"asset-relay","Status":"Active","AssetType":"Video"}}`))
	}))
	defer server.Close()
	client := localChinaDataPayAdapter(t, server.URL)
	item, err := client.CreateAsset(context.Background(), "video", "video", "https://example.com/video")
	if err != nil || item.ID != "asset-relay" || item.FileType != "video" {
		t.Fatalf("create: %+v, %v", item, err)
	}
	item, err = client.GetAsset(context.Background(), item.ID)
	if err != nil || item.Status != "Active" || calls != 2 {
		t.Fatalf("get: %+v, %v", item, err)
	}
}

func TestChinaDataPayErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"not found", 404, `{"ResponseMetadata":{"Error":{"Code":"NotFound.asset_id"}}}`, false},
		{"unauthorized", 401, `{"code":1}`, false},
		{"bad request", 400, `{"code":1}`, false},
		{"forbidden", 403, `{"code":1,"message":"upstream not configured"}`, false},
		{"limited", 429, `{"code":1}`, true},
		{"bad gateway", 502, `not json`, true},
		{"business", 200, `{"ResponseMetadata":{"Error":{"Code":"InvalidParameter"}}}`, false},
		{"platform envelope", 200, `{"code":1,"message":"test-token"}`, false},
		{"mismatched id", 200, `{"Result":{"Id":"asset-other","Status":"Active"}}`, false},
		{"invalid JSON", 200, `not json`, false},
		{"missing status", 200, `{"Result":{"Id":"asset-test"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := localChinaDataPayAdapter(t, server.URL)
			_, err := client.GetAsset(context.Background(), "asset-test")
			var apiErr *AssetAPIError
			if !errors.As(err, &apiErr) || apiErr.Retryable() != tc.retry {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), server.URL) {
				t.Fatal("sensitive error")
			}
		})
	}
}

func TestRouterNestedAdapters(t *testing.T) {
	cfg := DefaultConfig()
	router := NewAssetRouter(cfg, nil)
	if len(router.routes) != 0 {
		t.Fatal("omitted channels must be disabled")
	}
	cfg.Volcengine = &VolcengineConfig{ChannelConfig: ChannelConfig{AccessKey: "nested-ak", SecretKey: "nested-sk", GroupID: "nested-group"}}
	relay := testChinaDataPayConfig()
	cfg.ChinaDataPay = &relay
	router = NewAssetRouter(cfg, nil)
	if len(router.routes) != 2 || !router.routes[0].cfg.MatchesModel("volcengine/doubao-seedance-2.0") || !router.routes[1].cfg.MatchesModel("chinadatapay/doubao-seedance-2.0") {
		t.Fatal("nested routing failed")
	}
	direct := router.routes[0].mutator.client.(*VolcengineAdapter)
	if direct.accessKey != "nested-ak" || direct.groupID != "nested-group" {
		t.Fatal("nested credentials not loaded")
	}
	if _, ok := router.routes[1].mutator.client.(*ChinaDataPayAdapter); !ok {
		t.Fatal("relay chose wrong client")
	}
}

func TestRelayRegistrationAndReadiness(t *testing.T) {
	creates, gets := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			creates++
			_, _ = w.Write([]byte(`{"Result":{"Id":"asset-test","Status":"Processing"}}`))
		case "GetAsset":
			gets++
			_, _ = w.Write([]byte(`{"Result":{"Id":"asset-test","Status":"Active"}}`))
		default:
			t.Error("unexpected action")
		}
	}))
	defer server.Close()
	cfg := DefaultConfig()
	relay := testChinaDataPayConfig()
	cfg.ChinaDataPay = &relay
	router := NewAssetRouter(cfg, nil)
	router.routes[0].mutator.client = localChinaDataPayAdapter(t, server.URL)
	draft := &aigc.ContentGenerationDraft{Model: "cdp/doubao-seedance-2.0-aicc", Input: json.RawMessage(`{"input_reference":{"type":"video_url","video_url":{"url":"https://example.com/clip"}}}`)}
	resp, err := router.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeCreate, Draft: draft})
	if err != nil || resp.Reject {
		t.Fatalf("register: %+v %v", resp, err)
	}
	if ojson.GetBytes(resp.Draft.Input, "input_reference.video_url.url").String() != "asset://asset-test" {
		t.Fatal("asset replacement failed")
	}
	gen := aigc.ContentGeneration{Model: draft.Model, Input: resp.Draft.Input, Metadata: resp.Draft.Metadata}
	resp, err = router.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeSubmit, Generation: gen})
	if err != nil || resp.Reject || creates != 1 || gets != 1 {
		t.Fatalf("readiness: %+v %v (creates %d, gets %d)", resp, err, creates, gets)
	}
}

func TestReferenceTypePrecedence(t *testing.T) {
	for _, tc := range []struct{ key, url, want string }{
		{"image_url", "https://example.com/not-really.mp4", "image"},
		{"video_url", "https://example.com/not-really.png", "video"},
		{"audio_url", "https://example.com/no-extension", "audio"},
		{"input_reference", "https://example.com/video.mp4?signature=test", "video"},
	} {
		if got := inferFileType(tc.key, tc.url); got != tc.want {
			t.Errorf("%s: %s != %s", tc.key, got, tc.want)
		}
	}
}

func TestReferenceObjectKeepsURLString(t *testing.T) {
	cfg := DefaultWorkflowConfig()
	client := &mockArkClient{}
	mutator := NewMutator(cfg, client, nil)
	draft := &aigc.ContentGenerationDraft{Model: "volcengine/doubao-seedance-test", Input: json.RawMessage(`{"input_reference":{"url":"https://example.com/video.mp4","role":"reference_video"}}`)}
	resp, err := mutator.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeCreate, Draft: draft})
	if err != nil || resp.Reject {
		t.Fatalf("mutation: %+v, %v", resp, err)
	}
	node := ojson.GetBytes(resp.Draft.Input, "input_reference.url")
	if node.Type != ojson.String || !strings.HasPrefix(node.String(), "asset://") || ojson.GetBytes(resp.Draft.Input, "input_reference.type").String() != "video_url" {
		t.Fatalf("URL shape or type lost: %s", resp.Draft.Input)
	}
}

func TestRouterRoutesReferencesAndIsolatesCache(t *testing.T) {
	router := &AssetRouter{}
	clients := []*mockArkClient{{}, {}}
	for i, prefix := range []string{"volcengine", "chinadatapay"} {
		cfg := DefaultWorkflowConfig()
		cfg.Models = []string{prefix + "/*"}
		router.add(cfg, prefix, clients[i], nil, nil)
	}
	for _, prefix := range []string{"volcengine", "chinadatapay"} {
		draft := &aigc.ContentGenerationDraft{Model: prefix + "/seedance", Input: json.RawMessage(`{
   "callback_url":"https://example.com/callback", "prompt":"https://example.com/prompt",
   "input_reference":[{"type":"video_url","video_url":{"url":"https://example.com/no-extension"}},"https://example.com/signed.mp4?token=x"],
   "extra_body":{"content":[{"type":"audio_url","url":"https://example.com/no-extension"}]}
  }`)}
		for repeat := 0; repeat < 2; repeat++ {
			resp, err := router.MutateContentGeneration(context.Background(), aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeCreate, Draft: draft})
			if err != nil || resp.Reject {
				t.Fatalf("mutation: %v, %+v", err, resp)
			}
			if ojson.GetBytes(resp.Draft.Input, "callback_url").String() != "https://example.com/callback" || ojson.GetBytes(resp.Draft.Input, "prompt").String() != "https://example.com/prompt" {
				t.Fatal("non-media URL mutated")
			}
			if !strings.HasPrefix(ojson.GetBytes(resp.Draft.Input, "input_reference.0.video_url.url").String(), "asset://") {
				t.Fatal("video not registered")
			}
			if ojson.GetBytes(resp.Draft.Input, "input_reference.1.type").String() != "video_url" {
				t.Fatal("bare video type lost")
			}
		}
	}
	for _, client := range clients {
		if client.createCalls != 3 {
			t.Fatalf("expected channel-specific cache, got %d creates", client.createCalls)
		}
	}
	for _, route := range router.routes {
		for _, entry := range route.mutator.store.cache {
			if entry.record.OriginalURL == "https://example.com/no-extension" && entry.record.FileType != "video" && entry.record.FileType != "audio" {
				t.Fatal("explicit media type lost")
			}
		}
	}
}

func TestRouterConfigurationAndScope(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ChinaDataPay = &ChinaDataPayConfig{}
	router := NewAssetRouter(cfg, nil)
	req := aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeCreate, Draft: &aigc.ContentGenerationDraft{Model: "chinadatapay/doubao-seedance-2.0", Input: json.RawMessage(`{}`)}}
	if _, err := router.MutateContentGeneration(context.Background(), req); err == nil {
		t.Fatal("missing Token silently accepted")
	}
	req.Draft.Model = "other/model"
	if _, err := router.MutateContentGeneration(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	*cfg.ChinaDataPay = testChinaDataPayConfig()
	router = NewAssetRouter(cfg, nil)
	if len(router.routes) != 1 || router.routes[0].cfg.PollInterval() < time.Second {
		t.Fatal("incorrect relay defaults")
	}
	req = aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeSubmit, Generation: aigc.ContentGeneration{Model: "chinadatapay/doubao-seedance-2.0", Metadata: map[string]any{"volcengine-asset": map[string]any{"scope": "old-account"}}}}
	if _, err := router.MutateContentGeneration(context.Background(), req); err == nil {
		t.Fatal("account scope mismatch accepted")
	}
}

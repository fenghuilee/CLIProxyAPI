package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestCDPAssetModelPatterns(t *testing.T) {
	for _, patterns := range [][]string{nil, {"chinadatapay/doubao-seedance-*"}, {"cdp/doubao-seedance-*"}} {
		cfg := DefaultConfig()
		relay := testChinaDataPayConfig()
		relay.Models = patterns
		cfg.ChinaDataPay = &relay
		router := NewAssetRouter(cfg, nil)
		if len(router.routes) != 1 || router.routes[0].initErr != nil {
			t.Fatal("CDP route failed to initialize")
		}
		for _, prefix := range []string{"cdp/", "CDP/", "chinadatapay/"} {
			if !router.routes[0].cfg.MatchesModel(prefix + "doubao-seedance-2.0-aicc") {
				t.Fatalf("CDP Seedance alias not matched: %s", prefix)
			}
		}
		for _, model := range []string{"cdp/MiniMax-H3", "cdp/wan3.0-video", "volcengine/doubao-seedance-2.0-aicc"} {
			if router.routes[0].cfg.MatchesModel(model) {
				t.Fatalf("asset route widened to %s", model)
			}
		}
		before := len(cfg.ChinaDataPay.Models)
		cfg.Normalize()
		if len(cfg.ChinaDataPay.Models) != before {
			t.Fatal("normalization duplicated aliases")
		}
	}
}

func TestCDPMediaBypassesArkRegistration(t *testing.T) {
	for _, configPrefix := range []string{"cdp", "chinadatapay"} {
		cfg := DefaultConfig()
		cfg.ChinaDataPay = &ChinaDataPayConfig{Models: []string{configPrefix + "/*"}}
		router := NewAssetRouter(cfg, nil)
		for _, prefix := range []string{"cdp/", "CDP/", "chinadatapay/"} {
			for _, suffix := range []string{"MiniMax-H3", "wan3.0-video", "wan3.0-video-prime"} {
				for _, phase := range []aigc.GenerationLifecyclePhase{aigc.PhaseBeforeCreate, aigc.PhaseBeforeSubmit} {
					model := prefix + suffix
					input := []byte(`{"input_reference":"https://example.com/media.mp4"}`)
					req := aigc.GenerationMutationRequest{Phase: phase, Generation: aigc.ContentGeneration{Model: model, Input: input}}
					if phase == aigc.PhaseBeforeCreate {
						req.Draft = &aigc.ContentGenerationDraft{Model: model, Input: input}
					}
					result, err := router.MutateContentGeneration(context.Background(), req)
					if err != nil || result.Reject {
						t.Fatalf("%s reached Ark registration: %+v %v", model, result, err)
					}
					if phase == aigc.PhaseBeforeCreate && string(result.Draft.Input) != string(input) {
						t.Fatalf("%s media was rewritten", model)
					}
					if phase == aigc.PhaseBeforeSubmit && string(result.Generation.Input) != string(input) {
						t.Fatalf("%s media was rewritten", model)
					}
				}
			}
		}
	}
}

func TestMiniMaxDoesNotRegisterArkAssets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ChinaDataPay = &ChinaDataPayConfig{Models: []string{"chinadatapay/*"}}
	// Missing Ark credentials must not block H3 or trigger asset registration.
	router := NewAssetRouter(cfg, nil)
	for _, phase := range []aigc.GenerationLifecyclePhase{aigc.PhaseBeforeCreate, aigc.PhaseBeforeSubmit} {
		draft := &aigc.ContentGenerationDraft{Model: "chinadatapay/MiniMax-H3", Input: []byte(`{"input_reference":"https://example.com/image.png"}`)}
		req := aigc.GenerationMutationRequest{Phase: phase, Generation: aigc.ContentGeneration{Model: draft.Model}}
		if phase == aigc.PhaseBeforeCreate {
			req.Draft = draft
		}
		resp, err := router.MutateContentGeneration(context.Background(), req)
		if err != nil || resp.Reject {
			t.Fatalf("H3 reached Ark: %+v %v", resp, err)
		}
		if resp.Draft != nil && string(resp.Draft.Input) != string(draft.Input) {
			t.Fatal("H3 reference was rewritten")
		}
	}
}

func TestWanDoesNotRegisterArkAssets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ChinaDataPay = &ChinaDataPayConfig{Models: []string{"chinadatapay/*"}}
	router := NewAssetRouter(cfg, nil)
	for _, model := range []string{"chinadatapay/wan3.0-video", "chinadatapay/wan3.0-video-prime"} {
		for _, phase := range []aigc.GenerationLifecyclePhase{aigc.PhaseBeforeCreate, aigc.PhaseBeforeSubmit} {
			draft := &aigc.ContentGenerationDraft{Model: model, Input: []byte(`{"input_reference":"https://example.com/image.png"}`)}
			req := aigc.GenerationMutationRequest{Phase: phase, Generation: aigc.ContentGeneration{Model: model}}
			if phase == aigc.PhaseBeforeCreate {
				req.Draft = draft
			}
			res, err := router.MutateContentGeneration(context.Background(), req)
			if err != nil || res.Reject {
				t.Fatalf("Wan reached Ark: %+v %v", res, err)
			}
			if res.Draft != nil && string(res.Draft.Input) != string(draft.Input) {
				t.Fatal("Wan media was rewritten")
			}
		}
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

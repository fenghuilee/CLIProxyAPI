package main

import (
	"context"
	"testing"

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

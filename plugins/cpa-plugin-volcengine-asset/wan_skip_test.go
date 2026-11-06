package main

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"testing"
)

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

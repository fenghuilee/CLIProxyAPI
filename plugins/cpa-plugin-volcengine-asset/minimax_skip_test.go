package main

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"testing"
)

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

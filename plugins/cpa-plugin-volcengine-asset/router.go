package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

type assetRoute struct {
	cfg     WorkflowConfig
	scope   string
	mutator *Mutator
	initErr error
}

// AssetRouter selects a preconfigured client without exposing authentication to the lifecycle code.
type AssetRouter struct{ routes []assetRoute }

func NewAssetRouter(cfg Config, db DBClient) *AssetRouter {
	cfg.Normalize()
	router := &AssetRouter{}
	if direct := cfg.Volcengine; direct != nil && direct.enabled() {
		client, err := NewVolcengineAdapter(*direct)
		scope := "volcengine:" + sha256Hex(direct.AccessKey+"\x00"+direct.Region+"\x00"+direct.Project+"\x00"+direct.GroupID)
		router.add(WorkflowConfig{Models: direct.Models, RuntimeConfig: cfg.Runtime}, scope, client, err, db)
	}
	if relay := cfg.ChinaDataPay; relay != nil && relay.enabled() {
		client, err := NewChinaDataPayAdapter(*relay)
		scope := "chinadatapay:" + sha256Hex(chinaDataPayAssetEndpoint+"\x00"+strings.TrimSpace(relay.Token)+"\x00"+relay.GroupID)
		router.add(WorkflowConfig{Models: relay.Models, RuntimeConfig: cfg.Runtime}, scope, client, err, db)
	}
	return router
}

func (r *AssetRouter) add(cfg WorkflowConfig, scope string, client AssetAdapter, err error, db DBClient) {
	store := NewAssetStore(db, cfg.CacheTTL())
	store.scope = scope
	mutator := NewMutator(cfg, client, store)
	mutator.scope = scope
	r.routes = append(r.routes, assetRoute{cfg: cfg, scope: scope, mutator: mutator, initErr: err})
}

func (r *AssetRouter) MutateContentGeneration(ctx context.Context, req aigc.GenerationMutationRequest) (aigc.GenerationMutationResponse, error) {
	if req.Phase != aigc.PhaseBeforeCreate && req.Phase != aigc.PhaseBeforeSubmit {
		return aigc.GenerationMutationResponse{Draft: req.Draft, Generation: &req.Generation}, nil
	}
	model := req.Generation.Model
	if req.Draft != nil {
		model = req.Draft.Model
		if model == "" {
			model = ojson.GetBytes(req.Draft.Input, "model").String()
		}
	}
	// MiniMax and Wan do not use Ark asset identifiers, even under broad relay patterns.
	for _, prefix := range []string{"cdp/", "chinadatapay/"} {
		switch strings.ToLower(strings.TrimSpace(model)) {
		case prefix + "minimax-h3", prefix + "wan3.0-video", prefix + "wan3.0-video-prime":
			return aigc.GenerationMutationResponse{Draft: req.Draft, Generation: &req.Generation}, nil
		}
	}
	var selected *assetRoute
	for i := range r.routes {
		if r.routes[i].cfg.MatchesModel(model) {
			if selected != nil {
				return aigc.GenerationMutationResponse{}, fmt.Errorf("ambiguous asset channel model match")
			}
			selected = &r.routes[i]
		}
	}
	if selected == nil {
		return aigc.GenerationMutationResponse{Draft: req.Draft, Generation: &req.Generation}, nil
	}
	if selected.initErr != nil {
		return aigc.GenerationMutationResponse{}, selected.initErr
	}
	if meta, ok := req.Generation.Metadata["volcengine-asset"].(map[string]any); ok {
		if scope, ok := meta["scope"].(string); ok && scope != selected.scope {
			return aigc.GenerationMutationResponse{}, fmt.Errorf("asset channel account, project or group changed; restore the task's original channel configuration")
		}
	}
	return selected.mutator.MutateContentGeneration(ctx, req)
}

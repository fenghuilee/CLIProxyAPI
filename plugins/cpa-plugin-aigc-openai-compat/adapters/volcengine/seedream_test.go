package volcengine_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/volcengine"
	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestDriver_VolcengineSeedreamFlow_WithLayerDecomposition(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	// 1. PrepareSubmit
	input := aigc.GenerationSubmitInput{
		Generation: aigc.ContentGeneration{
			Kind:     aigc.ContentKindImage,
			Model:    "volcengine/doubao-seedream-5-0-pro",
			Provider: "volcengine-seedream",
			Input:    []byte(`{"prompt":"a fantasy castle","size":"2048x2048","extra_body":{"layer_decomposition":true}}`),
		},
	}

	req, err := driver.PrepareSubmit(ctx, input)
	if err != nil {
		t.Fatalf("PrepareSubmit error: %v", err)
	}
	if ojson.GetBytes(req.Body, "model").String() != "doubao-seedream-5-0-pro" {
		t.Fatalf("model = %q, want doubao-seedream-5-0-pro", ojson.GetBytes(req.Body, "model").String())
	}
	if ojson.GetBytes(req.Body, "size").String() != "2048x2048" {
		t.Fatalf("size = %q, want 2048x2048", ojson.GetBytes(req.Body, "size").String())
	}
	if !ojson.GetBytes(req.Body, "layer_decomposition").Bool() {
		t.Fatalf("layer_decomposition = false, want true")
	}

	// 2. ParseSubmit with Layer Decomposition data
	submitResp := aigc.GenerationExecutionResponse{
		StatusCode: http.StatusOK,
		Metadata:   map[string]any{"provider": "volcengine-seedream"},
		Body: []byte(`{
			"data": [
				{"url": "https://tos.volces.com/full.png"},
				{"url": "https://tos.volces.com/layer_bg.png", "layer_id": 1, "layer_name": "background", "layer_type": "background"},
				{"url": "https://tos.volces.com/layer_castle.png", "layer_id": 2, "layer_name": "castle", "layer_type": "foreground"}
			]
		}`),
	}

	submitRes, errSubmit := driver.ParseSubmit(ctx, submitResp)
	if errSubmit != nil {
		t.Fatalf("ParseSubmit error: %v", errSubmit)
	}
	if submitRes.Status != aigc.StatusSucceeded {
		t.Fatalf("Status = %v, want Succeeded", submitRes.Status)
	}
	if len(submitRes.Artifacts) != 3 {
		t.Fatalf("Artifacts count = %d, want 3", len(submitRes.Artifacts))
	}
	if submitRes.Artifacts[1].ArtifactType != "layer" || submitRes.Artifacts[1].Metadata["layer_name"] != "background" {
		t.Fatalf("Layer 1 metadata = %+v", submitRes.Artifacts[1].Metadata)
	}
}

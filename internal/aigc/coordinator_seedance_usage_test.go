package aigc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

type seedanceUsageDriver struct {
	testDriver
	usage map[string]any
}

func (d *seedanceUsageDriver) ParsePoll(context.Context, aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	result := aigc.GenerationPollResult{Status: aigc.StatusSucceeded, Stage: aigc.StageCompleted, Progress: 100, Artifacts: []aigc.ContentGenerationArtifact{{ArtifactType: "output_video", URI: "https://example.com/video.mp4"}}}
	if d.usage != nil {
		result.Metadata = map[string]any{"seedance": map[string]any{"usage": d.usage}}
	}
	return result, nil
}

func TestCoordinatorPersistsSeedanceUsageWithoutChangingOtherMetadata(t *testing.T) {
	ctx := context.Background()
	for _, adapter := range []string{"volcengine-seedance", "chinadatapay-seedance"} {
		for _, withUsage := range []bool{false, true} {
			name := adapter + "/absent"
			if withUsage {
				name = adapter + "/usage"
			}
			t.Run(name, func(t *testing.T) {
				store := newInMemoryStore()
				driver := &seedanceUsageDriver{}
				if withUsage {
					driver.usage = map[string]any{"completion_tokens": float64(123456), "tool_usage": map[string]any{"web_search": float64(0)}}
				}
				host := pluginhost.NewTestHost()
				coordinator := NewCoordinator(host, nil)
				coordinator.SetExecutor(&testExecutor{})
				metadata := map[string]any{"aigc_adapter": adapter, "canonical": map[string]any{"resolution": "720p", "duration": 5}, "volcengine-asset": map[string]any{"scope": "keep"}}
				if !withUsage {
					metadata["seedance"] = map[string]any{"usage": map[string]any{"completion_tokens": float64(77)}}
				}
				gen, err := store.Create(ctx, aigc.GenerationCreateRequest{Draft: aigc.ContentGenerationDraft{ID: "usage-test", Kind: aigc.ContentKindVideo, Model: "doubao-seedance-2-5", Metadata: metadata}})
				if err != nil {
					t.Fatal(err)
				}
				gen, err = store.Patch(ctx, aigc.GenerationPatchRequest{ID: gen.ID, Set: map[string]any{"status": aigc.StatusRunning, "provider_task_id": "task-test"}})
				if err != nil {
					t.Fatal(err)
				}
				coordinator.pollGeneration(ctx, store, driver, gen)
				saved, err := store.Get(ctx, aigc.GenerationGetRequest{ID: gen.ID})
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(saved.Metadata)
				if saved.Status != aigc.StatusSucceeded || ojson.GetBytes(raw, "aigc_adapter").String() != adapter || ojson.GetBytes(raw, "canonical.duration").Int() != 5 || ojson.GetBytes(raw, "volcengine-asset.scope").String() != "keep" {
					t.Fatalf("existing fields lost: %+v", saved)
				}
				usage := ojson.GetBytes(raw, "seedance.usage")
				if !usage.Exists() {
					t.Fatalf("usage not persisted correctly: %s", raw)
				}
				if !withUsage && usage.Get("completion_tokens").Int() != 77 {
					t.Fatalf("legacy response erased stored usage: %s", raw)
				}
				if withUsage && (usage.Get("completion_tokens").Int() != 123456 || !usage.Get("tool_usage.web_search").Exists() || usage.Get("total_tokens").Exists()) {
					t.Fatalf("usage fields changed: %s", raw)
				}
			})
		}
	}
}

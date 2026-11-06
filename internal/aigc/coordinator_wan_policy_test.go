package aigc

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"testing"
	"time"
)

func TestWanDriverPollInterval(t *testing.T) {
	for _, value := range []any{15, float64(15), int64(15)} {
		if interval := generationPollInterval(0, map[string]any{"aigc_poll_interval_seconds": value}); interval != 15*time.Second {
			t.Fatalf("interval=%v", interval)
		}
	}
	if interval := generationPollInterval(time.Millisecond, map[string]any{"aigc_poll_interval_seconds": 15}); interval != time.Millisecond {
		t.Fatal("explicit test/config override ignored")
	}
	if interval := generationPollInterval(0, nil); interval != 5*time.Second {
		t.Fatal("other driver default changed")
	}
	if delay := nextGenerationPollDelay(15*time.Second, 15*time.Second, true); delay != 30*time.Second {
		t.Fatal("Wan backoff did not increase")
	}
}

type wanPolicyDriver struct{ confirmedCancelDriver }

func (d *wanPolicyDriver) PrepareSubmit(context.Context, aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": "chinadatapay-wan", "single_attempt": true, "poll_interval_seconds": float64(15), "poll_backoff": true}}, nil
}
func TestCoordinatorPersistsWanPolicy(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryStore()
	driver := &wanPolicyDriver{confirmedCancelDriver: confirmedCancelDriver{testDriver: testDriver{kind: aigc.ContentKindVideo, model: "chinadatapay/wan3.0-video"}}}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{ID: "store", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: store}}},
		pluginhost.TestCapabilityRecord{ID: "driver", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}},
	)
	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})
	gen, err := store.Create(ctx, aigc.GenerationCreateRequest{Draft: aigc.ContentGenerationDraft{ID: "wan-policy", Kind: aigc.ContentKindVideo, Model: driver.model}})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.AsyncSubmit(gen.ID)
	saved, err := store.Get(ctx, aigc.GenerationGetRequest{ID: gen.ID})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Metadata["aigc_adapter"] != "chinadatapay-wan" || saved.Metadata["aigc_poll_backoff"] != true || generationPollInterval(0, saved.Metadata) != 15*time.Second {
		t.Fatalf("Wan policy lost: %+v", saved)
	}
}

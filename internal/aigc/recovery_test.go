package aigc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type recoveryTestStore struct {
	*inMemoryStore
	claimedIDs []string
}

func (s *recoveryTestStore) Claim(ctx context.Context, req aigc.GenerationClaimRequest) ([]aigc.ContentGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []aigc.ContentGeneration
	for _, gen := range s.generations {
		if (gen.Status == aigc.StatusAccepted || gen.Status == aigc.StatusRunning) && (gen.LeaseUntil == nil || gen.LeaseUntil.Before(time.Now())) {
			leaseUntil := time.Now().Add(req.LeaseDuration)
			gen.WorkerID = req.WorkerID
			gen.LeaseUntil = &leaseUntil
			s.generations[gen.ID] = gen
			result = append(result, gen)
			s.claimedIDs = append(s.claimedIDs, gen.ID)
		}
	}
	return result, nil
}

func TestRecoveryWorker(t *testing.T) {
	baseStore := newInMemoryStore()
	store := &recoveryTestStore{inMemoryStore: baseStore}

	driver := &testDriver{
		kind:  aigc.ContentKindVideo,
		model: "doubao-seedance-2-0-mini",
	}

	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
		pluginhost.TestCapabilityRecord{
			ID:       "driver-plugin",
			Priority: 50,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver},
			},
		},
	)

	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})

	// Add an accepted task into store directly
	_, _ = store.Create(context.Background(), aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{
			ID:    "cg_recover_1",
			Kind:  aigc.ContentKindVideo,
			Model: "doubao-seedance-2-0-mini",
			Input: json.RawMessage(`{"prompt":"recover this"}`),
		},
	})

	worker := NewRecoveryWorker(coordinator, 100*time.Millisecond)

	count, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if count != 1 {
		t.Errorf("recovered count = %d, want 1", count)
	}

	// Verify the worker can start and stop cleanly
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	worker.Stop()
}

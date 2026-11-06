package aigc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
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
		if gen.Status == aigc.StatusAccepted && gen.Stage == aigc.StageSubmitting {
			continue
		}
		if (gen.Status == aigc.StatusAccepted || gen.Status == aigc.StatusRunning) && (gen.LeaseUntil == nil || gen.LeaseUntil.Before(time.Now())) {
			leaseUntil := time.Now().Add(req.LeaseDuration)
			gen.WorkerID = req.WorkerID
			gen.LeaseUntil = &leaseUntil
			gen.Revision++
			s.generations[gen.ID] = gen
			result = append(result, gen)
			s.claimedIDs = append(s.claimedIDs, gen.ID)
		}
	}
	return result, nil
}

func (s *recoveryTestStore) Release(ctx context.Context, req aigc.GenerationReleaseRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen := s.generations[req.ID]
	if gen.WorkerID != req.WorkerID || (gen.Status == aigc.StatusAccepted && gen.Stage == aigc.StageSubmitting) {
		return nil
	}
	gen.WorkerID = ""
	gen.LeaseUntil = nil
	gen.Revision++
	s.generations[gen.ID] = gen
	return nil
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

func TestRecoveryWorker_ReapOnce(t *testing.T) {
	baseStore := newInMemoryStore()
	store := &recoveryTestStore{inMemoryStore: baseStore}

	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID:       "store-plugin",
			Priority: 100,
			Plugin: pluginapi.Plugin{
				Capabilities: pluginapi.Capabilities{ContentGenerationStore: store},
			},
		},
	)

	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})

	now := time.Now()
	// 1. Stale submitting task -> should be reaped
	staleGen, _ := store.Create(context.Background(), aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{
			ID:    "cg_stale_1",
			Kind:  aigc.ContentKindVideo,
			Model: "doubao-seedance-2-0-mini",
		},
	})
	staleGen.Stage = aigc.StageSubmitting
	staleGen.UpdatedAt = now.Add(-15 * time.Minute)
	store.generations[staleGen.ID] = staleGen

	// 2. Fresh submitting task -> should NOT be reaped
	freshGen, _ := store.Create(context.Background(), aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{
			ID:    "cg_fresh_1",
			Kind:  aigc.ContentKindVideo,
			Model: "doubao-seedance-2-0-mini",
		},
	})
	freshGen.Stage = aigc.StageSubmitting
	freshGen.UpdatedAt = now.Add(-2 * time.Minute)
	store.generations[freshGen.ID] = freshGen

	// 3. Running task with task ID -> should NOT be reaped
	runningGen, _ := store.Create(context.Background(), aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{
			ID:    "cg_running_1",
			Kind:  aigc.ContentKindVideo,
			Model: "doubao-seedance-2-0-mini",
		},
	})
	runningGen.Status = aigc.StatusRunning
	runningGen.Stage = aigc.StageProviderRunning
	runningGen.ProviderTaskID = "upstream_task_123"
	runningGen.UpdatedAt = now.Add(-20 * time.Minute)
	store.generations[runningGen.ID] = runningGen

	worker := NewRecoveryWorker(coordinator, 100*time.Millisecond)
	worker.SetStaleThreshold(10 * time.Minute)

	count, err := worker.ReapOnce(context.Background())
	if err != nil {
		t.Fatalf("ReapOnce failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 reaped task, got %d", count)
	}

	reaped, _ := store.Get(context.Background(), aigc.GenerationGetRequest{ID: "cg_stale_1"})
	if reaped.Status != aigc.StatusFailed || reaped.Stage != aigc.StageFailed {
		t.Fatalf("expected failed status, got status=%s stage=%s", reaped.Status, reaped.Stage)
	}
	if reaped.ErrorCode != "upstream_submission_timeout" {
		t.Fatalf("expected upstream_submission_timeout, got %s", reaped.ErrorCode)
	}

	fresh, _ := store.Get(context.Background(), aigc.GenerationGetRequest{ID: "cg_fresh_1"})
	if fresh.Status != aigc.StatusAccepted || fresh.Stage != aigc.StageSubmitting {
		t.Fatalf("fresh task was modified: %+v", fresh)
	}

	running, _ := store.Get(context.Background(), aigc.GenerationGetRequest{ID: "cg_running_1"})
	if running.Status != aigc.StatusRunning {
		t.Fatalf("running task was modified: %+v", running)
	}
}

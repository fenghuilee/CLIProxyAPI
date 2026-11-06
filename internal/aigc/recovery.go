package aigc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// RecoveryWorker periodically claims unleased active generations and advances their lifecycle.
type RecoveryWorker struct {
	coordinator    *Coordinator
	workerID       string
	interval       time.Duration
	leaseDuration  time.Duration
	batchLimit     int
	reapInterval   time.Duration
	staleThreshold time.Duration
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// NewRecoveryWorker creates a new background recovery worker.
func NewRecoveryWorker(coordinator *Coordinator, interval time.Duration) *RecoveryWorker {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	hostname, _ := os.Hostname()
	workerID := fmt.Sprintf("worker-%s-%s", hostname, uuid.New().String()[:8])

	return &RecoveryWorker{
		coordinator:    coordinator,
		workerID:       workerID,
		interval:       interval,
		leaseDuration:  3 * time.Minute,
		batchLimit:     20,
		reapInterval:   time.Minute,
		staleThreshold: 10 * time.Minute,
		stopChan:       make(chan struct{}),
	}
}

// SetReapInterval overrides the reap ticker interval (useful for testing).
func (w *RecoveryWorker) SetReapInterval(interval time.Duration) {
	w.reapInterval = interval
}

// SetStaleThreshold overrides the submission timeout duration (useful for testing).
func (w *RecoveryWorker) SetStaleThreshold(threshold time.Duration) {
	w.staleThreshold = threshold
}

// Start launches the background recovery worker loop.
func (w *RecoveryWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		claimTicker := time.NewTicker(w.interval)
		defer claimTicker.Stop()

		reapInterval := w.reapInterval
		if reapInterval <= 0 {
			reapInterval = time.Minute
		}
		reapTicker := time.NewTicker(reapInterval)
		defer reapTicker.Stop()

		for {
			select {
			case <-w.stopChan:
				return
			case <-ctx.Done():
				return
			case <-claimTicker.C:
				_, _ = w.RunOnce(ctx)
			case <-reapTicker.C:
				_, _ = w.ReapOnce(ctx)
			}
		}
	}()
}

// Stop gracefully shuts down the recovery worker.
func (w *RecoveryWorker) Stop() {
	close(w.stopChan)
	w.wg.Wait()
}

// RunOnce performs a single claim-and-recover cycle. Returns count of tasks recovered.
func (w *RecoveryWorker) RunOnce(ctx context.Context) (int, error) {
	store, _, okStore := w.coordinator.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return 0, aigc.ErrStoreNotConfigured
	}

	claimed, errClaim := store.Claim(ctx, aigc.GenerationClaimRequest{
		WorkerID:      w.workerID,
		LeaseDuration: w.leaseDuration,
		Limit:         w.batchLimit,
	})
	if errClaim != nil {
		return 0, fmt.Errorf("recovery claim: %w", errClaim)
	}

	if len(claimed) == 0 {
		return 0, nil
	}

	recoveredCount := 0
	for _, gen := range claimed {
		w.recoverGeneration(ctx, store, gen)
		recoveredCount++
	}

	return recoveredCount, nil
}

func (w *RecoveryWorker) recoverGeneration(ctx context.Context, store aigc.ContentGenerationStore, gen aigc.ContentGeneration) {
	defer func() {
		_ = store.Release(ctx, aigc.GenerationReleaseRequest{
			ID:       gen.ID,
			WorkerID: w.workerID,
		})
	}()

	switch gen.Status {
	case aigc.StatusAccepted:
		w.coordinator.AsyncSubmit(gen.ID)
	case aigc.StatusRunning:
		if driver, _, okDriver := w.coordinator.pluginHost.ContentGenerationDriverFor(ctx, gen.Kind, gen.Model); okDriver && driver != nil {
			_ = w.coordinator.pollGeneration(ctx, store, driver, gen)
		}
	}

	w.coordinator.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
		GenerationID: gen.ID,
		Revision:     gen.Revision,
		EventType:    aigc.EventGenerationRecovered,
		Stage:        gen.Stage,
		Status:       gen.Status,
		CreatedAt:    time.Now(),
	})
}

// ReapOnce scans for generations stuck in submitting beyond the stale threshold and marks them as failed.
func (w *RecoveryWorker) ReapOnce(ctx context.Context) (int, error) {
	store, _, okStore := w.coordinator.pluginHost.ContentGenerationStore()
	if !okStore || store == nil {
		return 0, aigc.ErrStoreNotConfigured
	}

	staleThreshold := w.staleThreshold
	if staleThreshold <= 0 {
		staleThreshold = 10 * time.Minute
	}

	reaped, errReap := store.ReapStaleSubmissions(ctx, aigc.GenerationReapStaleRequest{
		StaleDuration: staleThreshold,
		Limit:         w.batchLimit,
		ErrorCode:     "upstream_submission_timeout",
		ErrorMessage:  "submission timed out after 10 minutes without provider confirmation",
	})
	if errReap != nil {
		return 0, fmt.Errorf("reap stale submissions: %w", errReap)
	}

	for _, gen := range reaped {
		w.coordinator.pluginHost.EmitContentGenerationEvent(ctx, aigc.ContentGenerationEvent{
			GenerationID: gen.ID,
			Revision:     gen.Revision,
			EventType:    aigc.EventGenerationFailed,
			Stage:        gen.Stage,
			Status:       gen.Status,
			CreatedAt:    time.Now(),
		})
		_, _ = w.coordinator.pluginHost.MutateContentGeneration(ctx, aigc.GenerationMutationRequest{
			Phase:      aigc.PhaseOnFailed,
			Generation: gen,
		})
	}

	return len(reaped), nil
}

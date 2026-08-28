package aigc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

// RecoveryWorker periodically claims unleased active generations and advances their lifecycle.
type RecoveryWorker struct {
	coordinator   *Coordinator
	workerID      string
	interval      time.Duration
	leaseDuration time.Duration
	batchLimit    int
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// NewRecoveryWorker creates a new background recovery worker.
func NewRecoveryWorker(coordinator *Coordinator, interval time.Duration) *RecoveryWorker {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	hostname, _ := os.Hostname()
	workerID := fmt.Sprintf("worker-%s-%s", hostname, uuid.New().String()[:8])

	return &RecoveryWorker{
		coordinator:   coordinator,
		workerID:      workerID,
		interval:      interval,
		leaseDuration: 3 * time.Minute,
		batchLimit:    20,
		stopChan:      make(chan struct{}),
	}
}

// Start launches the background recovery worker loop.
func (w *RecoveryWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChan:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = w.RunOnce(ctx)
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

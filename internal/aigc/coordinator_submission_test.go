package aigc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type submissionTestDriver struct {
	*testDriver
	parseErr error
}

func (d *submissionTestDriver) ParseSubmit(context.Context, aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	if d.parseErr != nil {
		return aigc.GenerationSubmitResult{}, d.parseErr
	}
	return aigc.GenerationSubmitResult{
		ProviderTaskID: "upstream-task",
		Status:         aigc.StatusSucceeded,
		Stage:          aigc.StageCompleted,
	}, nil
}

func submissionTestHost(store aigc.ContentGenerationStore, driver aigc.ContentGenerationDriver) *pluginhost.Host {
	return pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{
			ID: "store-plugin", Priority: 100,
			Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: store}},
		},
		pluginhost.TestCapabilityRecord{
			ID: "driver-plugin", Priority: 50,
			Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}},
		},
	)
}

func createSubmissionTestGeneration(t *testing.T, store aigc.ContentGenerationStore) aigc.ContentGeneration {
	t.Helper()
	gen, err := store.Create(context.Background(), aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{
			ID: "cg_submission", Kind: aigc.ContentKindVideo, Model: "test-video",
			Input: json.RawMessage(`{"prompt":"test"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return gen
}

func waitForSubmissionTest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for submission")
	}
}

func TestSubmissionRecoveryDoesNotResubmitInFlight(t *testing.T) {
	for _, blockPhase := range []string{"prepare", "execute"} {
		t.Run(blockPhase, func(t *testing.T) {
			store := &recoveryTestStore{inMemoryStore: newInMemoryStore()}
			gen := createSubmissionTestGeneration(t, store)
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var blocked atomic.Bool
			block := func() {
				if blocked.CompareAndSwap(false, true) {
					close(started)
					<-release
				}
			}
			driver := &submissionTestDriver{testDriver: &testDriver{kind: gen.Kind, model: gen.Model}}
			if blockPhase == "prepare" {
				driver.submitFn = func(aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
					block()
					return aigc.GenerationExecutionRequest{Body: json.RawMessage(`{"task":"submit"}`)}, nil
				}
			}
			exec := &testExecutor{executeFn: func(context.Context, string, string, []byte, string) ([]byte, http.Header, *interfaces.ErrorMessage) {
				if blockPhase == "execute" {
					block()
				}
				return []byte(`{"ok":true}`), nil, nil
			}}
			coordinator := NewCoordinator(submissionTestHost(store, driver), nil)
			coordinator.SetExecutor(exec)
			go func() {
				defer close(done)
				coordinator.AsyncSubmit(gen.ID)
			}()
			defer func() {
				close(release)
				waitForSubmissionTest(t, done)
				final, err := store.Get(context.Background(), aigc.GenerationGetRequest{ID: gen.ID})
				if err != nil || final.Status != aigc.StatusSucceeded || final.ProviderTaskID != "upstream-task" {
					t.Errorf("submission result lost: %+v, err=%v", final, err)
				}
				if exec.calls != 1 {
					t.Errorf("total upstream calls = %d, want 1", exec.calls)
				}
			}()
			waitForSubmissionTest(t, started)

			before, _ := store.Get(context.Background(), aigc.GenerationGetRequest{ID: gen.ID})
			worker := NewRecoveryWorker(coordinator, time.Second)
			count, err := worker.RunOnce(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("recovered %d in-flight tasks, want 0", count)
			}
			after, _ := store.Get(context.Background(), aigc.GenerationGetRequest{ID: gen.ID})
			if after.Revision != before.Revision {
				t.Errorf("recovery changed in-flight revision: %d -> %d", before.Revision, after.Revision)
			}
			// A second coordinator must also honor the durable submission claim.
			other := NewCoordinator(coordinator.pluginHost, nil)
			other.SetExecutor(exec)
			other.AsyncSubmit(gen.ID)
			exec.mu.Lock()
			calls := exec.calls
			exec.mu.Unlock()
			wantCalls := 0
			if blockPhase == "execute" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("upstream calls while blocked = %d, want %d", calls, wantCalls)
			}
		})
	}
}

type submissionReadBarrierStore struct {
	*inMemoryStore
	reads atomic.Int32
	ready chan struct{}
}

func (s *submissionReadBarrierStore) Get(ctx context.Context, req aigc.GenerationGetRequest) (aigc.ContentGeneration, error) {
	gen, err := s.inMemoryStore.Get(ctx, req)
	if n := s.reads.Add(1); n <= 2 {
		if n == 2 {
			close(s.ready)
		}
		<-s.ready
	}
	return gen, err
}

func TestSubmissionConcurrentCoordinatorsUseCAS(t *testing.T) {
	store := &submissionReadBarrierStore{inMemoryStore: newInMemoryStore(), ready: make(chan struct{})}
	gen := createSubmissionTestGeneration(t, store)
	driver := &submissionTestDriver{testDriver: &testDriver{kind: gen.Kind, model: gen.Model}}
	host := submissionTestHost(store, driver)
	exec := &testExecutor{}
	var wg sync.WaitGroup
	for range 2 {
		coordinator := NewCoordinator(host, nil)
		coordinator.SetExecutor(exec)
		wg.Add(1)
		go func() {
			defer wg.Done()
			coordinator.AsyncSubmit(gen.ID)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	waitForSubmissionTest(t, done)
	if exec.calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", exec.calls)
	}
	final, err := store.Get(context.Background(), aigc.GenerationGetRequest{ID: gen.ID})
	if err != nil || final.Status != aigc.StatusSucceeded || final.ProviderTaskID != "upstream-task" {
		t.Fatalf("submission result lost: %+v, err=%v", final, err)
	}
}

func TestSubmissionRestartDoesNotResubmitExpiredSubmission(t *testing.T) {
	store := &recoveryTestStore{inMemoryStore: newInMemoryStore()}
	gen := createSubmissionTestGeneration(t, store)
	gen.Stage = aigc.StageSubmitting
	expired := time.Now().Add(-time.Hour)
	gen.LeaseUntil = &expired
	gen.WorkerID = "stopped-worker"
	store.generations[gen.ID] = gen
	driver := &submissionTestDriver{testDriver: &testDriver{kind: gen.Kind, model: gen.Model}}
	coordinator := NewCoordinator(submissionTestHost(store, driver), nil)
	exec := &testExecutor{}
	coordinator.SetExecutor(exec)
	coordinator.AsyncSubmit(gen.ID)
	count, err := NewRecoveryWorker(coordinator, time.Second).RunOnce(context.Background())
	if err != nil || count != 0 || exec.calls != 0 {
		t.Fatalf("expired submission was retried: recovered=%d, calls=%d, err=%v", count, exec.calls, err)
	}
}

func TestSubmissionPersistsRequestBeforeUpstreamFailure(t *testing.T) {
	for _, failure := range []string{"execute", "parse"} {
		t.Run(failure, func(t *testing.T) {
			store := newInMemoryStore()
			gen := createSubmissionTestGeneration(t, store)
			driver := &submissionTestDriver{testDriver: &testDriver{kind: gen.Kind, model: gen.Model}}
			if failure == "parse" {
				driver.parseErr = errors.New("invalid submit response")
			}
			exec := &testExecutor{executeFn: func(context.Context, string, string, []byte, string) ([]byte, http.Header, *interfaces.ErrorMessage) {
				snapshot, err := store.Get(context.Background(), aigc.GenerationGetRequest{ID: gen.ID})
				if err != nil || snapshot.Stage != aigc.StageSubmitting || string(snapshot.ProviderRequest) != `{"task":"submit"}` || snapshot.Provider != "driver-plugin" {
					t.Errorf("request not persisted before sending: %+v, err=%v", snapshot, err)
				}
				if failure == "execute" {
					return nil, nil, &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: errors.New("connection closed")}
				}
				return []byte(`{"ok":true}`), nil, nil
			}}
			coordinator := NewCoordinator(submissionTestHost(store, driver), nil)
			coordinator.SetExecutor(exec)
			coordinator.AsyncSubmit(gen.ID)
			final, _ := store.Get(context.Background(), aigc.GenerationGetRequest{ID: gen.ID})
			if final.Status != aigc.StatusFailed || string(final.ProviderRequest) != `{"task":"submit"}` {
				t.Fatalf("failed request audit lost: %+v", final)
			}
		})
	}
}

type submissionWriteFailureStore struct{ *inMemoryStore }

func (s *submissionWriteFailureStore) Patch(ctx context.Context, req aigc.GenerationPatchRequest) (aigc.ContentGeneration, error) {
	if _, ok := req.Set["provider_request"]; ok {
		return aigc.ContentGeneration{}, errors.New("request persistence unavailable")
	}
	return s.inMemoryStore.Patch(ctx, req)
}

func TestSubmissionDoesNotSendWithoutPersistingRequest(t *testing.T) {
	store := &submissionWriteFailureStore{inMemoryStore: newInMemoryStore()}
	gen := createSubmissionTestGeneration(t, store)
	driver := &submissionTestDriver{testDriver: &testDriver{kind: gen.Kind, model: gen.Model}}
	coordinator := NewCoordinator(submissionTestHost(store, driver), nil)
	exec := &testExecutor{}
	coordinator.SetExecutor(exec)
	coordinator.AsyncSubmit(gen.ID)
	if exec.calls != 0 {
		t.Fatalf("sent %d requests without persisting payload", exec.calls)
	}
}

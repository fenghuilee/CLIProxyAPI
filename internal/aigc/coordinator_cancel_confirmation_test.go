package aigc

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type confirmedCancelDriver struct {
	testDriver
	prepErr, parseErr error
	parsedStatus      int
}

func (d *confirmedCancelDriver) PrepareSubmit(context.Context, aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Metadata: map[string]any{"provider": "chinadatapay-minimax-h3", "cancel_requires_confirmation": true, "poll_backoff": true}}, nil
}
func (d *confirmedCancelDriver) ParseSubmit(context.Context, aigc.GenerationExecutionResponse) (aigc.GenerationSubmitResult, error) {
	return aigc.GenerationSubmitResult{Status: aigc.StatusSucceeded, ProviderTaskID: "task_test"}, nil
}

func (d *confirmedCancelDriver) PrepareCancel(context.Context, aigc.GenerationCancelInput) (aigc.GenerationExecutionRequest, error) {
	return aigc.GenerationExecutionRequest{Method: http.MethodDelete, URL: "https://example.com/v2/video_generation/task_test"}, d.prepErr
}
func (d *confirmedCancelDriver) ParseCancel(context.Context, aigc.GenerationExecutionResponse) error {
	return d.parseErr
}
func (d *confirmedCancelDriver) ParsePoll(_ context.Context, resp aigc.GenerationExecutionResponse) (aigc.GenerationPollResult, error) {
	d.parsedStatus = resp.StatusCode
	if resp.StatusCode == 404 {
		return aigc.GenerationPollResult{Status: aigc.StatusFailed, Stage: aigc.StageFailed, ErrorCode: "not_found"}, nil
	}
	return aigc.GenerationPollResult{Status: aigc.StatusRunning, Metadata: map[string]any{"minimax_h3": map[string]any{"usage": map[string]any{"output_seconds": 6}}}}, nil
}

func TestCoordinatorRequiresCancellationConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name                string
		prepErr, parseErr   error
		execErr             *interfaces.ErrorMessage
		noExecutor, success bool
	}{
		{name: "success", success: true},
		{name: "prepare failure", prepErr: errors.New("invalid endpoint")},
		{name: "parse failure", parseErr: errors.New("upstream did not confirm cancellation")},
		{name: "upstream rejection", execErr: &interfaces.ErrorMessage{StatusCode: 400, Error: errors.New("not cancellable")}},
		{name: "executor unavailable", noExecutor: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newInMemoryStore()
			driver := &confirmedCancelDriver{testDriver: testDriver{kind: aigc.ContentKindVideo, model: "chinadatapay/MiniMax-H3"}, prepErr: tc.prepErr, parseErr: tc.parseErr}
			host := pluginhost.NewTestHost(
				pluginhost.TestCapabilityRecord{ID: "store", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: store}}},
				pluginhost.TestCapabilityRecord{ID: "driver", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}},
			)
			coordinator := NewCoordinator(host, nil)
			if !tc.noExecutor {
				coordinator.SetExecutor(&testExecutor{executeFn: func(context.Context, string, string, []byte, string) ([]byte, http.Header, *interfaces.ErrorMessage) {
					return []byte(`{"action":"cancelled"}`), nil, tc.execErr
				}})
			}
			gen, err := store.Create(ctx, aigc.GenerationCreateRequest{Draft: aigc.ContentGenerationDraft{ID: "cancel-test", Kind: aigc.ContentKindVideo, Model: driver.model, Metadata: map[string]any{"aigc_cancel_requires_confirmation": true}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Patch(ctx, aigc.GenerationPatchRequest{ID: gen.ID, Set: map[string]any{"status": aigc.StatusRunning, "provider_task_id": "task_test"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = coordinator.CancelGeneration(ctx, gen.ID)
			if (err == nil) != tc.success {
				t.Fatalf("cancel error: %v", err)
			}
			saved, err := store.Get(ctx, aigc.GenerationGetRequest{ID: gen.ID})
			if err != nil {
				t.Fatal(err)
			}
			want := aigc.StatusRunning
			if tc.success {
				want = aigc.StatusCanceled
			}
			if saved.Status != want {
				t.Fatalf("false local cancellation: %+v", saved)
			}
		})
	}
}

func TestCoordinatorH3QueryErrorsAndUsage(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{200, 404, 429, 503} {
		store := newInMemoryStore()
		driver := &confirmedCancelDriver{testDriver: testDriver{pollFn: func(aigc.GenerationPollInput) (aigc.GenerationExecutionRequest, error) {
			return aigc.GenerationExecutionRequest{Metadata: map[string]any{"fail_on_query_client_error": true}}, nil
		}}}
		coordinator := NewCoordinator(nil, nil)
		coordinator.SetExecutor(&testExecutor{executeFn: func(context.Context, string, string, []byte, string) ([]byte, http.Header, *interfaces.ErrorMessage) {
			if status != 200 {
				return nil, nil, &interfaces.ErrorMessage{StatusCode: status, Body: []byte(`{"error_code":"not_found"}`), Error: errors.New("query failed")}
			}
			return []byte(`{}`), nil, nil
		}})
		// Polling emits events through a host even when no mutators are registered.
		coordinator.pluginHost = pluginhost.NewTestHost()
		gen, err := store.Create(ctx, aigc.GenerationCreateRequest{Draft: aigc.ContentGenerationDraft{ID: "query-test", Kind: aigc.ContentKindVideo, Metadata: map[string]any{"aigc_adapter": "chinadatapay-minimax-h3", "canonical": "keep"}}})
		if err != nil {
			t.Fatal(err)
		}
		gen, err = store.Patch(ctx, aigc.GenerationPatchRequest{ID: gen.ID, Set: map[string]any{"status": aigc.StatusRunning, "provider_task_id": "task_test"}})
		if err != nil {
			t.Fatal(err)
		}
		updated := coordinator.pollGeneration(ctx, store, driver, gen)
		if status == 404 && (updated.Status != aigc.StatusFailed || driver.parsedStatus != 404) {
			t.Fatalf("404 retried instead of becoming terminal: %+v", updated)
		}
		if status == 429 || status == 503 {
			if updated.Status != aigc.StatusRunning || driver.parsedStatus != 0 {
				t.Fatalf("transient became terminal: %+v", updated)
			}
		}
		if status == 200 && (updated.Metadata["minimax_h3"] == nil || updated.Metadata["canonical"] != "keep" || updated.Metadata["aigc_adapter"] != "chinadatapay-minimax-h3") {
			t.Fatalf("usage/identity lost: %+v", updated.Metadata)
		}
	}
}

func TestGenerationPollBackoff(t *testing.T) {
	base := 3 * time.Second
	delay := base
	for _, seconds := range []int{6, 12, 24, 30, 30} {
		delay = nextGenerationPollDelay(base, delay, true)
		if delay != time.Duration(seconds)*time.Second {
			t.Fatalf("delay=%v want %ds", delay, seconds)
		}
	}
	if delay := nextGenerationPollDelay(base, delay, false); delay != base {
		t.Fatalf("successful polling did not reset: %v", delay)
	}
	if delay := nextGenerationPollDelay(time.Minute, time.Minute, true); delay != time.Minute {
		t.Fatalf("backoff shortened configured interval: %v", delay)
	}
}

func TestCoordinatorPersistsH3PolicyMetadata(t *testing.T) {
	ctx := context.Background()
	store := newInMemoryStore()
	driver := &confirmedCancelDriver{testDriver: testDriver{kind: aigc.ContentKindVideo, model: "chinadatapay/MiniMax-H3"}}
	host := pluginhost.NewTestHost(
		pluginhost.TestCapabilityRecord{ID: "store", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationStore: store}}},
		pluginhost.TestCapabilityRecord{ID: "driver", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}},
	)
	coordinator := NewCoordinator(host, nil)
	coordinator.SetExecutor(&testExecutor{})
	gen, err := store.Create(ctx, aigc.GenerationCreateRequest{Draft: aigc.ContentGenerationDraft{ID: "policy-test", Kind: aigc.ContentKindVideo, Model: driver.model, Metadata: map[string]any{"canonical": "keep"}}})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.AsyncSubmit(gen.ID)
	saved, err := store.Get(ctx, aigc.GenerationGetRequest{ID: gen.ID})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"aigc_adapter": "chinadatapay-minimax-h3", "aigc_cancel_requires_confirmation": true, "aigc_poll_backoff": true, "canonical": "keep"} {
		if saved.Metadata[key] != want {
			t.Fatalf("%s not persisted: %+v", key, saved.Metadata)
		}
	}
}

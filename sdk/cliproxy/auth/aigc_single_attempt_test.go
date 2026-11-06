package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executionregistry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestAIGCSingleAttemptStopsCredentialAndOuterRetries(t *testing.T) {
	for _, status := range []int{401, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			manager.SetRetryConfig(3, time.Second, 5)
			for _, id := range []string{"first", "second"} {
				id = fmt.Sprintf("aigc-%s-%d", id, status)
				reg := registry.GetGlobalRegistry()
				reg.RegisterClient(id, "claude", []*registry.ModelInfo{{ID: "claude-3"}})
				t.Cleanup(func() { reg.UnregisterClient(id) })
				if _, err := manager.Register(context.Background(), &Auth{ID: id, Provider: "claude", Status: StatusActive}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			manager.RegisterExecutor(&mockCustomErrorExecutor{identifier: "claude", executeFn: func(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
				calls++
				cliproxyexecutor.MarkUpstreamAttempt(ctx)
				return cliproxyexecutor.Response{}, customStatusError{code: status, msg: "ambiguous creation"}
			}})
			_, err := manager.Execute(context.Background(), []string{"claude"}, cliproxyexecutor.Request{Model: "claude-3"}, cliproxyexecutor.Options{Metadata: map[string]any{"aigc_single_attempt": true}})
			if err == nil || calls != 1 {
				t.Fatalf("creation retried: %d calls, %v", calls, err)
			}
		})
	}
}

func TestAIGCSingleAttemptStopsHomeRedispatch(t *testing.T) {
	dispatcher := &retryContractHomeDispatcher{authIDs: []string{"home-retry-a", "home-retry-b"}}
	executor := &retryContractHomeExecutor{}
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
	manager.SetRetryConfig(3, time.Second, 5)
	manager.PublishHomeDispatch(dispatcher, executionregistry.New(), 1)
	manager.RegisterExecutor(executor)
	_, err := manager.Execute(context.Background(), []string{"home-retry-contract"}, cliproxyexecutor.Request{Model: "gpt"}, cliproxyexecutor.Options{Metadata: map[string]any{"aigc_single_attempt": true}})
	if err == nil || len(executor.Calls()) != 1 || len(dispatcher.Excluded()) != 1 {
		t.Fatalf("Home retried creation: calls=%v dispatches=%v error=%v", executor.Calls(), dispatcher.Excluded(), err)
	}
}

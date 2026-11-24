package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestSeedanceRelativePathsUseCredentialBaseURL(t *testing.T) {
	for _, prefix := range []string{"/api/v3", "/custom/api/v3"} {
		t.Run(prefix, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("credential authentication missing")
				}
				switch r.Method {
				case http.MethodPost:
					if r.URL.Path != prefix+"/contents/generations/tasks" {
						t.Errorf("create path=%s", r.URL.Path)
					}
					_, _ = w.Write([]byte(`{"id":"task_existing"}`))
				case http.MethodGet:
					if r.URL.Path != prefix+"/contents/generations/tasks/task_existing" {
						t.Errorf("poll path=%s", r.URL.Path)
					}
					_, _ = w.Write([]byte(`{"id":"task_existing","status":"succeeded","content":{"video_url":"https://example.com/video.mp4"}}`))
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
			}))
			defer server.Close()
			executor := NewOpenAICompatExecutor("chinadatapay", &config.Config{})
			auth := &cliproxyauth.Auth{ID: "seedance-test", Provider: "chinadatapay", Attributes: map[string]string{"base_url": server.URL + prefix + "/", "api_key": "test-token"}}
			for _, request := range []struct {
				method, path string
				body         []byte
			}{
				{http.MethodPost, "/contents/generations/tasks", []byte(`{"model":"doubao-seedance-2-0-mini","content":[{"type":"text","text":"test"}]}`)},
				{http.MethodGet, "/contents/generations/tasks/task_existing", nil},
			} {
				_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "doubao-seedance-2-0-mini", Payload: request.body}, cliproxyexecutor.Options{Metadata: map[string]any{cliproxyexecutor.CustomEndpointMetadataKey: request.path, cliproxyexecutor.HTTPMethodMetadataKey: request.method}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 {
				t.Fatalf("calls=%d, want create and poll", calls)
			}
		})
	}
}

func TestSeedanceAbsoluteEndpointKeepsCredentialV1BaseURL(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("credential authentication missing")
		}
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/api/v3/contents/generations/tasks" {
				t.Errorf("wrong create path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"id":"task_existing"}`))
		case http.MethodGet:
			if r.URL.Path != "/api/v3/contents/generations/tasks/task_existing" {
				t.Errorf("wrong poll path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"id":"task_existing","status":"succeeded","usage":{"completion_tokens":42},"content":{"video_url":"https://example.com/video.mp4"}}`))
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()
	executor := NewOpenAICompatExecutor("chinadatapay", &config.Config{})
	baseURL := server.URL + "/v1"
	auth := &cliproxyauth.Auth{ID: "seedance-absolute-test", Provider: "chinadatapay", Attributes: map[string]string{"base_url": baseURL, "api_key": "test-token"}}
	for _, request := range []struct {
		method, url string
		body        []byte
	}{
		{http.MethodPost, server.URL + "/api/v3/contents/generations/tasks", []byte(`{"model":"doubao-seedance-2-0-mini","content":[{"type":"text","text":"test"}]}`)},
		{http.MethodGet, server.URL + "/api/v3/contents/generations/tasks/task_existing", nil},
	} {
		_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "doubao-seedance-2-0-mini", Payload: request.body}, cliproxyexecutor.Options{Metadata: map[string]any{cliproxyexecutor.CustomEndpointMetadataKey: request.url, cliproxyexecutor.HTTPMethodMetadataKey: request.method}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || auth.Attributes["base_url"] != baseURL {
		t.Fatalf("calls=%d, credential BaseURL=%s", calls, auth.Attributes["base_url"])
	}
}

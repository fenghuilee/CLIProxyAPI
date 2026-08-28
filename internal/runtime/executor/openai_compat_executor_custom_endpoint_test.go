package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestOpenAICompatExecutorCustomEndpointPassthrough(t *testing.T) {
	var gotMethod, gotPath, gotAuthHeader string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuthHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = body

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"task_volc_123","status":"running"}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name: "volcengine",
		}},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":     server.URL + "/api/v3",
			"api_key":      "ark-secret-key-123",
			"compat_name":  "volcengine",
			"provider_key": "volcengine",
		},
	}

	// 1. Test POST /contents/generations/tasks via Metadata
	submitPayload := []byte(`{"model":"doubao-seedance-2-0-mini","content":[{"type":"text","text":"hello"}]}`)
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "doubao-seedance-2-0-mini",
		Payload: submitPayload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Metadata: map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: "/contents/generations/tasks",
			cliproxyexecutor.RequestPathMetadataKey:    "/contents/generations/tasks",
			cliproxyexecutor.HTTPMethodMetadataKey:     http.MethodPost,
		},
		Stream: false,
	})
	if err != nil {
		t.Fatalf("Execute submit error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("got method %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/api/v3/contents/generations/tasks" {
		t.Errorf("got path %q, want /api/v3/contents/generations/tasks", gotPath)
	}
	if gotAuthHeader != "Bearer ark-secret-key-123" {
		t.Errorf("got auth header %q, want Bearer ark-secret-key-123", gotAuthHeader)
	}
	if string(gotBody) != string(submitPayload) {
		t.Errorf("got body %s, want %s", string(gotBody), string(submitPayload))
	}
	if string(resp.Payload) != `{"id":"task_volc_123","status":"running"}` {
		t.Errorf("unexpected response payload: %s", string(resp.Payload))
	}

	// 2. Test GET /contents/generations/tasks/task_volc_123 via Metadata
	_, errPoll := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model: "doubao-seedance-2-0-mini",
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Metadata: map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: "/contents/generations/tasks/task_volc_123",
			cliproxyexecutor.RequestPathMetadataKey:    "/contents/generations/tasks/task_volc_123",
			cliproxyexecutor.HTTPMethodMetadataKey:     http.MethodGet,
		},
		Stream: false,
	})
	if errPoll != nil {
		t.Fatalf("Execute poll error: %v", errPoll)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("got poll method %q, want %q", gotMethod, http.MethodGet)
	}
	// 3. Test that custom endpoint payload model is rewritten to resolved upstream req.Model
	submitAliasPayload := []byte(`{"model":"doubao-seedance-2-0-mini","content":[{"type":"text","text":"video prompt"}]}`)
	_, errRewrite := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "doubao-seedance-2-0-mini-260615", // Resolved upstream model name
		Payload: submitAliasPayload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Metadata: map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: "/contents/generations/tasks",
			cliproxyexecutor.RequestPathMetadataKey:    "/contents/generations/tasks",
			cliproxyexecutor.HTTPMethodMetadataKey:     http.MethodPost,
		},
		Stream: false,
	})
	if errRewrite != nil {
		t.Fatalf("Execute submit with model rewrite error: %v", errRewrite)
	}
	expectedRewrittenBody := `{"model":"doubao-seedance-2-0-mini-260615","content":[{"type":"text","text":"video prompt"}]}`
	if string(gotBody) != expectedRewrittenBody {
		t.Errorf("got rewritten body %s, want %s", string(gotBody), expectedRewrittenBody)
	}
}

func TestOpenAICompatExecutorStandardChatDoesNotTreatRequestPathAsCustomEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-123","choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name: "volcengine",
		}},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":     server.URL + "/api/v3",
			"api_key":      "ark-secret-key-123",
			"compat_name":  "volcengine",
			"provider_key": "volcengine",
		},
	}

	payload := []byte(`{"model":"doubao-seed-2-0-mini","messages":[{"role":"user","content":"hi"}]}`)
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "doubao-seed-2-0-mini-260428",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Metadata: map[string]any{
			// RequestPathMetadataKey is routinely set by Gin handler to the inbound route
			cliproxyexecutor.RequestPathMetadataKey: "/v1/chat/completions",
		},
		Stream: false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("got method %q, want %q", gotMethod, http.MethodPost)
	}
	// Must be /api/v3/chat/completions, NEVER /api/v3/v1/chat/completions
	if gotPath != "/api/v3/chat/completions" {
		t.Errorf("got path %q, want /api/v3/chat/completions", gotPath)
	}
}

func TestOpenAICompatExecutorCustomEndpointAbsoluteURL(t *testing.T) {
	var gotMethod, gotPath, gotAuthHeader string
	var gotBody []byte

	// Independent mock server (simulating DashScope native multimodal generation)
	nativeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuthHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = body

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"task_id":"task_dashscope_123","task_status":"SUCCEEDED"}}`))
	}))
	defer nativeServer.Close()

	executor := NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name: "alibaba (china)",
		}},
	})
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			// Notice: baseURL has /compatible-mode/v1 configured for chat
			"base_url":     "https://dashscope.aliyuncs.com/compatible-mode/v1",
			"api_key":      "sk-dashscope-secret-456",
			"compat_name":  "alibaba (china)",
			"provider_key": "alibaba-cn",
		},
	}

	targetAbsoluteURL := nativeServer.URL + "/api/v1/services/aigc/multimodal-generation/generation"
	submitPayload := []byte(`{"model":"qwen-image-3.0-pro","input":{"prompt":"test"}}`)

	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "qwen-image-3.0-pro",
		Payload: submitPayload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Metadata: map[string]any{
			cliproxyexecutor.CustomEndpointMetadataKey: targetAbsoluteURL,
			cliproxyexecutor.RequestPathMetadataKey:    targetAbsoluteURL,
			cliproxyexecutor.HTTPMethodMetadataKey:     http.MethodPost,
		},
		Stream: false,
	})
	if err != nil {
		t.Fatalf("Execute submit absolute URL error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("got method %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/api/v1/services/aigc/multimodal-generation/generation" {
		t.Errorf("got path %q, want /api/v1/services/aigc/multimodal-generation/generation", gotPath)
	}
	if gotAuthHeader != "Bearer sk-dashscope-secret-456" {
		t.Errorf("got auth header %q, want Bearer sk-dashscope-secret-456", gotAuthHeader)
	}
	if string(gotBody) != string(submitPayload) {
		t.Errorf("got body %s, want %s", string(gotBody), string(submitPayload))
	}
	if string(resp.Payload) != `{"output":{"task_id":"task_dashscope_123","task_status":"SUCCEEDED"}}` {
		t.Errorf("unexpected response payload: %s", string(resp.Payload))
	}
}

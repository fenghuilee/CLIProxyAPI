package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testChinaDataPayConfig() ChinaDataPayConfig {
	return ChinaDataPayConfig{Token: "test-token", GroupID: "test-group"}
}

func localChinaDataPayAdapter(t *testing.T, endpoint string) *ChinaDataPayAdapter {
	t.Helper()
	adapter, err := NewChinaDataPayAdapter(testChinaDataPayConfig())
	if err != nil {
		t.Fatal(err)
	}
	if adapter.endpoint != chinaDataPayAssetEndpoint {
		t.Fatal("gateway endpoint is not fixed")
	}
	// Only tests override the endpoint; public config has no address option.
	adapter.endpoint = endpoint + "/v1/api/assets/"
	return adapter
}

func TestChinaDataPayCreationDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"ServiceUnavailable","Message":"private-upstream-details"}}}`))
	}))
	defer server.Close()
	client := localChinaDataPayAdapter(t, server.URL)
	_, err := client.CreateAsset(context.Background(), "test", "image", "https://example.com/image.png")
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 502 || calls.Load() != 1 {
		t.Fatalf("creation retried or error lost: calls=%d error=%v", calls.Load(), err)
	}
	if !strings.Contains(err.Error(), "ServiceUnavailable") || !strings.Contains(err.Error(), "private-upstream-details") {
		t.Fatalf("expected upstream error details propagated: %v", err)
	}
}

func TestChinaDataPayPropagatesDetailedErrorAndRedactsSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"InvalidParameter.ImageFetchFailed","Message":"Failed fetching image using token test-token"}}}`))
	}))
	defer server.Close()
	client := localChinaDataPayAdapter(t, server.URL)
	_, err := client.CreateAsset(context.Background(), "test", "image", "https://example.com/image.png")
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("expected HTTP 400 AssetAPIError: %v", err)
	}
	if apiErr.Code != "InvalidParameter.ImageFetchFailed" {
		t.Fatalf("expected code InvalidParameter.ImageFetchFailed, got %s", apiErr.Code)
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatalf("token leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in error message: %v", err)
	}
}

func TestChinaDataPayContextCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)
	client := localChinaDataPayAdapter(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.GetAsset(ctx, "asset-test"); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Action request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Action request ignored lifecycle cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Action request did not stop")
	}
}

func TestChinaDataPayDoesNotFollowRedirects(t *testing.T) {
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := localChinaDataPayAdapter(t, server.URL).GetAsset(context.Background(), "asset-test")
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != 307 || forwarded.Load() != 0 {
		t.Fatalf("Bearer request followed redirect: %v", err)
	}
}

func TestChinaDataPayActionClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth := r.Header.Get("Authorization")
		if r.Method != "POST" || auth != "Bearer test-token" || r.Header.Get("X-Date") != "" || r.Header.Get("Content-Type") != "application/json" || r.URL.Query().Get("Version") != "2024-01-01" || r.URL.Path != "/v1/api/assets/" {
			t.Errorf("incorrect Bearer Action request")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, ok := payload["model"]; ok {
			t.Error("unexpected model")
		}
		if _, exists := payload["ProjectName"]; exists {
			t.Error("project must be owned by the server")
		}
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			if payload["AssetType"] != "Video" || payload["URL"] != "https://example.com/video" {
				t.Errorf("wrong create payload: %v", payload)
			}
			if payload["GroupId"] != "test-group" {
				t.Error("missing asset group")
			}
		case "GetAsset":
			if payload["Id"] != "asset-relay" || len(payload) != 1 {
				t.Error("incorrect asset id")
			}
		default:
			t.Error("incorrect action")
		}
		_, _ = w.Write([]byte(`{"Result":{"Id":"asset-relay","Status":"Active","AssetType":"Video"}}`))
	}))
	defer server.Close()
	client := localChinaDataPayAdapter(t, server.URL)
	item, err := client.CreateAsset(context.Background(), "video", "video", "https://example.com/video")
	if err != nil || item.ID != "asset-relay" || item.FileType != "video" {
		t.Fatalf("create: %+v, %v", item, err)
	}
	item, err = client.GetAsset(context.Background(), item.ID)
	if err != nil || item.Status != "Active" || calls != 2 {
		t.Fatalf("get: %+v, %v", item, err)
	}
}

func TestChinaDataPayErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"not found", 404, `{"ResponseMetadata":{"Error":{"Code":"NotFound.asset_id"}}}`, false},
		{"unauthorized", 401, `{"code":1}`, false},
		{"bad request", 400, `{"code":1}`, false},
		{"forbidden", 403, `{"code":1,"message":"upstream not configured"}`, false},
		{"limited", 429, `{"code":1}`, true},
		{"bad gateway", 502, `not json`, true},
		{"business", 200, `{"ResponseMetadata":{"Error":{"Code":"InvalidParameter"}}}`, false},
		{"platform envelope", 200, `{"code":1,"message":"test-token"}`, false},
		{"mismatched id", 200, `{"Result":{"Id":"asset-other","Status":"Active"}}`, false},
		{"invalid JSON", 200, `not json`, false},
		{"missing status", 200, `{"Result":{"Id":"asset-test"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := localChinaDataPayAdapter(t, server.URL)
			_, err := client.GetAsset(context.Background(), "asset-test")
			var apiErr *AssetAPIError
			if !errors.As(err, &apiErr) || apiErr.Retryable() != tc.retry {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), server.URL) {
				t.Fatal("sensitive error")
			}
		})
	}
}

func TestChinaDataPayDefaultGroupAndTokenOnlyConfig(t *testing.T) {
	cfg, err := ParseConfig([]byte("chinadatapay:\n  token: '  test-token  '\n"))
	if err != nil {
		t.Fatal(err)
	}
	router := NewAssetRouter(cfg, nil)
	if len(router.routes) != 1 || router.routes[0].initErr != nil {
		t.Fatal("Token-only config did not initialize")
	}
	client := router.routes[0].mutator.client.(*ChinaDataPayAdapter)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.URL.Path != "/v1/api/assets/" || r.URL.Query().Get("Version") != "2024-01-01" {
			t.Error("wrong token/path/version")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		for _, forbidden := range []string{"GroupId", "ProjectName", "model"} {
			if _, exists := payload[forbidden]; exists {
				t.Errorf("unexpected field: %s", forbidden)
			}
		}
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			if payload["AssetType"] != "Image" || payload["Name"] != "cat" || payload["URL"] != "https://example.com/cat.png" {
				t.Error("wrong create fields")
			}
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":null},"Result":{"Id":"asset-default","Status":"Processing"}}`))
		case "GetAsset":
			if payload["Id"] != "asset-default" || len(payload) != 1 {
				t.Error("wrong query fields")
			}
			_, _ = w.Write([]byte(`{"Result":{"Id":"asset-default","Status":"Active"}}`))
		default:
			t.Error("unexpected action")
		}
	}))
	defer server.Close()
	client.endpoint = server.URL + "/v1/api/assets/"
	created, err := client.CreateAsset(context.Background(), "cat", "image", "https://example.com/cat.png")
	if err != nil || created.Status != "Processing" {
		t.Fatalf("default group creation: %+v %v", created, err)
	}
	queried, err := client.GetAsset(context.Background(), created.ID)
	if err != nil || queried.Status != "Active" {
		t.Fatalf("default group query: %+v %v", queried, err)
	}
}

func TestChinaDataPayResponseLimitAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Result":{"Id":"asset-test","Status":"Active"},"padding":"` + strings.Repeat("x", 1<<20) + `"}`))
	}))
	defer server.Close()
	client := localChinaDataPayAdapter(t, server.URL)
	_, err := client.GetAsset(context.Background(), "asset-test")
	var apiErr *AssetAPIError
	if !errors.As(err, &apiErr) || apiErr.Retryable() {
		t.Fatalf("oversized response accepted: %v", err)
	}
	server.Close()
	_, err = client.GetAsset(context.Background(), "asset-test")
	if !errors.As(err, &apiErr) || !apiErr.Retryable() || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("transport failure was not safely retryable: %v", err)
	}
}

func TestChinaDataPayCreateAssetMissingStatusAndDataEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name           string
		createResponse string
		expectedStatus string
	}{
		{
			name:           "Result without status",
			createResponse: `{"ResponseMetadata":{"Action":"CreateAsset"},"Result":{"Id":"asset-no-status"}}`,
			expectedStatus: "Processing",
		},
		{
			name:           "data envelope without status",
			createResponse: `{"code":0,"message":"ok","data":{"Id":"asset-data-env"},"request_id":"req-123"}`,
			expectedStatus: "Processing",
		},
		{
			name:           "data envelope with status",
			createResponse: `{"code":0,"message":"ok","data":{"Id":"asset-data-active","Status":"Active"},"request_id":"req-456"}`,
			expectedStatus: "Active",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.createResponse))
			}))
			defer server.Close()
			client := localChinaDataPayAdapter(t, server.URL)
			item, err := client.CreateAsset(context.Background(), "test", "image", "https://example.com/test.png")
			if err != nil {
				t.Fatalf("CreateAsset failed: %v", err)
			}
			if item.Status != tc.expectedStatus {
				t.Fatalf("expected status %q, got %q", tc.expectedStatus, item.Status)
			}
		})
	}
}

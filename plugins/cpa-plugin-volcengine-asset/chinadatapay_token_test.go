package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

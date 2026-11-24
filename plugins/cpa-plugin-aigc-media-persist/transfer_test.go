package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockUploader struct {
	uploadedKey  string
	uploadedMIME string
	uploadedData []byte
}

func (m *mockUploader) Upload(ctx context.Context, key string, mimeType string, data []byte) (string, error) {
	m.uploadedKey = key
	m.uploadedMIME = mimeType
	m.uploadedData = data
	return "https://cdn.example.com/" + key, nil
}

func TestHTTPMediaTransferer_ShouldTransfer(t *testing.T) {
	cfg := Config{
		Bucket:        "top1ai-prod",
		PublicBaseURL: "https://top1ai.tos-cn-guangzhou.volces.com",
		SkipDomains:   []string{"mycdn.internal"},
	}
	trans := NewHTTPMediaTransferer(cfg, nil)

	tests := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"data:image/png;base64,xxxx", false},
		{"inline://sha256=xxx", false},
		{"https://top1ai.tos-cn-guangzhou.volces.com/aigc/outputs/test.mp4", false},
		{"https://top1ai-prod.tos-cn-beijing.volces.com/aigc/outputs/test.mp4", false},
		{"https://static.mycdn.internal/videos/sample.mp4", false},
		{"https://dashscope-a717.oss-accelerate.aliyuncs.com/temp/video.mp4?Expires=1795", true},
		{"https://ark-acg-cn-beijing.tos-cn-beijing.volces.com/seedance/video.mp4?X-Tos-Expires=86400", true},
		{"https://files.metaso.cn/api/minimax/video.mp4", true},
	}

	for _, tt := range tests {
		got := trans.ShouldTransfer(tt.url)
		if got != tt.want {
			t.Errorf("ShouldTransfer(%q) = %v; want %v", tt.url, got, tt.want)
		}
	}
}

func TestHTTPMediaTransferer_Transfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("fake-mp4-binary-payload-data"))
	}))
	defer server.Close()

	mockUp := &mockUploader{}
	cfg := DefaultConfig()
	cfg.PublicBaseURL = "https://cdn.example.com"
	trans := NewHTTPMediaTransferer(cfg, mockUp)

	res, err := trans.Transfer(context.Background(), server.URL+"/sample.mp4", "video/mp4")
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	if res.MIMEType != "video/mp4" {
		t.Errorf("expected MIMEType video/mp4, got %s", res.MIMEType)
	}
	if res.SizeBytes != int64(len("fake-mp4-binary-payload-data")) {
		t.Errorf("expected SizeBytes %d, got %d", len("fake-mp4-binary-payload-data"), res.SizeBytes)
	}
	if string(mockUp.uploadedData) != "fake-mp4-binary-payload-data" {
		t.Errorf("uploaded data mismatch")
	}

	// Test default extensions when URL path has no extension
	if ext := mimeToExt("video/mp4"); ext != ".mp4" {
		t.Errorf("expected .mp4 for video, got %s", ext)
	}
	if ext := mimeToExt("image/png"); ext != ".jpg" {
		t.Errorf("expected .jpg for image, got %s", ext)
	}
	if ext := mimeToExt(""); ext != ".mp4" {
		t.Errorf("expected .mp4 fallback, got %s", ext)
	}
}

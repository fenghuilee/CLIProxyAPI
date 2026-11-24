package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// TransferResult captures metadata of an uploaded permanent asset.
type TransferResult struct {
	NewURL    string
	ObjectKey string
	MIMEType  string
	SizeBytes int64
	SHA256    string
}

// MediaTransferer defines interface for downloading remote media and archiving to TOS.
type MediaTransferer interface {
	ShouldTransfer(rawURL string) bool
	Transfer(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error)
}

// HTTPMediaTransferer downloads remote HTTP(S) media and saves it to TOS.
type HTTPMediaTransferer struct {
	cfg        Config
	uploader   ObjectUploader
	httpClient *http.Client
}

// NewHTTPMediaTransferer initializes transferer with configured client and limits.
func NewHTTPMediaTransferer(cfg Config, uploader ObjectUploader) *HTTPMediaTransferer {
	return &HTTPMediaTransferer{
		cfg:      cfg,
		uploader: uploader,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.DownloadTimeoutSeconds) * time.Second,
		},
	}
}

// ShouldTransfer evaluates whether the candidate URL is a remote external media link that requires archiving.
func (t *HTTPMediaTransferer) ShouldTransfer(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Host)

	// Skip if already in self-hosted public domain
	if t.cfg.PublicBaseURL != "" {
		if pubURL, errPub := url.Parse(t.cfg.PublicBaseURL); errPub == nil && pubURL.Host != "" {
			if strings.EqualFold(host, pubURL.Host) {
				return false
			}
		}
	}

	// Skip if already in self-hosted TOS bucket domain
	if t.cfg.Bucket != "" && strings.Contains(host, strings.ToLower(t.cfg.Bucket)) {
		return false
	}

	// Skip user-configured domains
	for _, skip := range t.cfg.SkipDomains {
		if skip != "" && strings.Contains(host, strings.ToLower(strings.TrimSpace(skip))) {
			return false
		}
	}

	return true
}

// Transfer downloads the remote media and archives it to permanent TOS storage.
func (t *HTTPMediaTransferer) Transfer(ctx context.Context, rawURL string, defaultMIME string) (*TransferResult, error) {
	if t.uploader == nil {
		return nil, fmt.Errorf("uploader not configured")
	}

	rawURL = strings.TrimSpace(rawURL)
	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if errReq != nil {
		return nil, fmt.Errorf("create download request for %s: %w", rawURL, errReq)
	}

	resp, errDo := t.httpClient.Do(req)
	if errDo != nil {
		return nil, fmt.Errorf("download remote media from %s: %w", rawURL, errDo)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download remote media from %s: http status %d", rawURL, resp.StatusCode)
	}

	maxBytes := t.cfg.MaxFileBytes
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("media size %d exceeds limit of %d bytes", resp.ContentLength, maxBytes)
	}

	// Read content up to limit + 1 to detect overflow
	data, errRead := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if errRead != nil {
		return nil, fmt.Errorf("read media content: %w", errRead)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("downloaded media exceeds limit of %d bytes", maxBytes)
	}

	mimeType := defaultMIME
	if mimeType == "" {
		if ct := resp.Header.Get("Content-Type"); ct != "" && ct != "application/octet-stream" {
			if idx := strings.Index(ct, ";"); idx != -1 {
				ct = strings.TrimSpace(ct[:idx])
			}
			mimeType = ct
		}
	}
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	ext := ""
	if parsed, errParse := url.Parse(rawURL); errParse == nil {
		ext = path.Ext(parsed.Path)
	}
	if ext == "" {
		ext = mimeToExt(mimeType)
	}

	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])

	dateFolder := time.Now().Format("20060102")
	objectKey := path.Join(t.cfg.OutputPrefix, dateFolder, fmt.Sprintf("%s%s", hashHex, ext))

	tosURL, errUp := t.uploader.Upload(ctx, objectKey, mimeType, data)
	if errUp != nil {
		return nil, fmt.Errorf("upload to tos (%s): %w", objectKey, errUp)
	}

	return &TransferResult{
		NewURL:    tosURL,
		ObjectKey: objectKey,
		MIMEType:  mimeType,
		SizeBytes: int64(len(data)),
		SHA256:    hashHex,
	}, nil
}

func mimeToExt(mimeType string) string {
	lower := strings.ToLower(mimeType)
	if strings.HasPrefix(lower, "image/") {
		return ".jpg"
	}
	if strings.HasPrefix(lower, "audio/") {
		return ".mp3"
	}
	return ".mp4"
}

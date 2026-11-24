package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

// ObjectUploader abstracts object storage operations for uploading media files.
type ObjectUploader interface {
	Upload(ctx context.Context, key string, mimeType string, data []byte) (string, error)
}

// TOSUploader implements ObjectUploader using Volcengine TOS SDK.
type TOSUploader struct {
	client        *tos.ClientV2
	bucket        string
	publicBaseURL string
	endpoint      string
}

// NewTOSUploader initializes a new TOS uploader using the validated configuration.
func NewTOSUploader(cfg Config) (*TOSUploader, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client, err := tos.NewClientV2(
		cfg.Endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(tos.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey)),
	)
	if err != nil {
		return nil, fmt.Errorf("init tos client: %w", err)
	}

	return &TOSUploader{
		client:        client,
		bucket:        cfg.Bucket,
		publicBaseURL: cfg.PublicBaseURL,
		endpoint:      cfg.Endpoint,
	}, nil
}

// Upload uploads byte payload to TOS bucket under key with given content type.
func (u *TOSUploader) Upload(ctx context.Context, key string, mimeType string, data []byte) (string, error) {
	key = strings.TrimPrefix(key, "/")

	_, err := u.client.PutObjectV2(ctx, &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket:      u.bucket,
			Key:         key,
			ContentType: mimeType,
		},
		Content: bytes.NewReader(data),
	})
	if err != nil {
		return "", fmt.Errorf("tos put object (%s): %w", key, err)
	}

	if u.publicBaseURL != "" {
		return fmt.Sprintf("%s/%s", u.publicBaseURL, key), nil
	}

	cleanHost := strings.TrimPrefix(strings.TrimPrefix(u.endpoint, "https://"), "http://")
	return fmt.Sprintf("https://%s.%s/%s", u.bucket, cleanHost, key), nil
}

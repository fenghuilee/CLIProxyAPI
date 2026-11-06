package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

// ObjectUploader abstracts object storage upload operations.
type ObjectUploader interface {
	Upload(ctx context.Context, key string, mimeType string, data []byte) (string, error)
}

// TOSUploader uploads objects to Volcengine TOS.
type TOSUploader struct {
	client        *tos.ClientV2
	bucket        string
	publicBaseURL string
	endpoint      string
}

// NewTOSUploader initializes a new TOS uploader with the given credentials and configuration.
func NewTOSUploader(cfg Config) (*TOSUploader, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("TOS_ENDPOINT")
	}
	region := cfg.Region
	if region == "" {
		region = os.Getenv("TOS_REGION")
		if region == "" {
			region = "cn-beijing"
		}
	}
	bucket := cfg.Bucket
	if bucket == "" {
		bucket = os.Getenv("TOS_BUCKET")
	}
	ak := cfg.AccessKey
	if ak == "" {
		ak = os.Getenv("TOS_ACCESS_KEY")
		if ak == "" {
			ak = os.Getenv("VOLCENGINE_ACCESS_KEY")
		}
		if ak == "" {
			ak = os.Getenv("VOLC_ACCESSKEY")
		}
	}
	sk := cfg.SecretKey
	if sk == "" {
		sk = os.Getenv("TOS_SECRET_KEY")
		if sk == "" {
			sk = os.Getenv("VOLCENGINE_SECRET_KEY")
		}
		if sk == "" {
			sk = os.Getenv("VOLC_SECRETKEY")
		}
	}
	publicBaseURL := cfg.PublicBaseURL
	if publicBaseURL == "" {
		publicBaseURL = os.Getenv("TOS_PUBLIC_BASE_URL")
	}

	if endpoint == "" {
		return nil, fmt.Errorf("tos endpoint is required")
	}
	if bucket == "" {
		return nil, fmt.Errorf("tos bucket is required")
	}
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("tos access_key and secret_key are required")
	}

	client, err := tos.NewClientV2(
		endpoint,
		tos.WithRegion(region),
		tos.WithCredentials(tos.NewStaticCredentials(ak, sk)),
	)
	if err != nil {
		return nil, fmt.Errorf("init tos client: %w", err)
	}

	return &TOSUploader{
		client:        client,
		bucket:        bucket,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		endpoint:      strings.TrimRight(endpoint, "/"),
	}, nil
}

// Upload uploads the byte slice to TOS and returns the accessible URL.
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

	return fmt.Sprintf("https://%s.%s/%s", u.bucket, strings.TrimPrefix(u.endpoint, "https://"), key), nil
}

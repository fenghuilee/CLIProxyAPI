package main

import (
	"context"
	"fmt"
)

// AssetItem is a channel-neutral registered media asset.
type AssetItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	FileType     string `json:"file_type"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// AssetAdapter keeps protocol and authentication out of the shared lifecycle.
type AssetAdapter interface {
	CreateAsset(ctx context.Context, name, fileType, url string) (AssetItem, error)
	GetAsset(ctx context.Context, assetID string) (AssetItem, error)
}

// AssetAPIError contains only safe diagnostics, never SDK response bodies or credentials.
type AssetAPIError struct {
	Status int
	Code   string
}

func (e *AssetAPIError) Error() string {
	return fmt.Sprintf("asset API error (HTTP %d, code %s)", e.Status, e.Code)
}
func (e *AssetAPIError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

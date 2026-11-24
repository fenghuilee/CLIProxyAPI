package main

import (
	"testing"
)

func TestLoadConfig_DefaultsAndOverrides(t *testing.T) {
	// Case 1: Empty YAML uses default values
	cfgDefault := LoadConfig(nil)
	if cfgDefault.Region != "cn-beijing" {
		t.Errorf("expected default region cn-beijing, got %s", cfgDefault.Region)
	}
	if cfgDefault.OutputPrefix != "aigc/outputs" {
		t.Errorf("expected default prefix aigc/outputs, got %s", cfgDefault.OutputPrefix)
	}
	if cfgDefault.Concurrency != 4 {
		t.Errorf("expected default concurrency 4, got %d", cfgDefault.Concurrency)
	}
	if cfgDefault.MaxFileBytes != 200*1024*1024 {
		t.Errorf("expected default max bytes 200MB, got %d", cfgDefault.MaxFileBytes)
	}

	// Case 2: Custom YAML cleanly merges on top of defaults
	customYAML := []byte(`
endpoint: "https://tos-cn-shanghai.volces.com/"
bucket: "my-shanghai-bucket"
access-key: "AK-123"
secret-key: "SK-456"
output-prefix: "/custom/media/"
concurrency: 8
`)
	cfgCustom := LoadConfig(customYAML)

	if cfgCustom.Endpoint != "https://tos-cn-shanghai.volces.com" {
		t.Errorf("expected trimmed endpoint, got %s", cfgCustom.Endpoint)
	}
	if cfgCustom.Bucket != "my-shanghai-bucket" {
		t.Errorf("expected bucket my-shanghai-bucket, got %s", cfgCustom.Bucket)
	}
	if cfgCustom.OutputPrefix != "custom/media" {
		t.Errorf("expected trimmed prefix custom/media, got %s", cfgCustom.OutputPrefix)
	}
	if cfgCustom.Concurrency != 8 {
		t.Errorf("expected overridden concurrency 8, got %d", cfgCustom.Concurrency)
	}
	// Verify un-overridden defaults were preserved
	if cfgCustom.Region != "cn-beijing" {
		t.Errorf("expected preserved default region cn-beijing, got %s", cfgCustom.Region)
	}
	if cfgCustom.DownloadTimeoutSeconds != 60 {
		t.Errorf("expected preserved default timeout 60s, got %d", cfgCustom.DownloadTimeoutSeconds)
	}

	// Validate method
	if err := cfgCustom.Validate(); err != nil {
		t.Errorf("valid config failed validation: %v", err)
	}

	// Validate incomplete
	incompleteCfg := LoadConfig([]byte(`bucket: "only-bucket"`))
	if err := incompleteCfg.Validate(); err == nil {
		t.Errorf("expected validation error for incomplete credentials")
	}
}

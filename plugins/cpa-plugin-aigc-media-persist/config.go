package main

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for the aigc-media-persist plugin.
type Config struct {
	Endpoint               string   `yaml:"endpoint"`
	Region                 string   `yaml:"region"`
	Bucket                 string   `yaml:"bucket"`
	AccessKey              string   `yaml:"access-key"`
	SecretKey              string   `yaml:"secret-key"`
	PublicBaseURL          string   `yaml:"public-base-url"`
	OutputPrefix           string   `yaml:"output-prefix"`
	MaxFileBytes           int64    `yaml:"max-file-bytes"`
	DownloadTimeoutSeconds int      `yaml:"download-timeout-seconds"`
	Concurrency            int      `yaml:"concurrency"`
	SystemOnly             bool     `yaml:"system-only"` // If true, only archives generations created by system (user_id == 0)
	SkipDomains            []string `yaml:"skip-domains"`
}

// DefaultConfig returns base configuration with robust standard defaults.
func DefaultConfig() Config {
	return Config{
		Region:                 "cn-beijing",
		OutputPrefix:           "aigc/outputs",
		MaxFileBytes:           200 * 1024 * 1024, // 200MB default limit for video files
		DownloadTimeoutSeconds: 60,                // 60s per file download timeout
		Concurrency:            4,
	}
}

// MergeWithDefaults overlays non-zero user-defined configuration values on top of DefaultConfig.
func MergeWithDefaults(custom Config) Config {
	merged := DefaultConfig()

	if custom.Endpoint != "" {
		merged.Endpoint = strings.TrimRight(custom.Endpoint, "/")
	}
	if custom.Region != "" {
		merged.Region = strings.TrimSpace(custom.Region)
	}
	if custom.Bucket != "" {
		merged.Bucket = strings.TrimSpace(custom.Bucket)
	}
	if custom.AccessKey != "" {
		merged.AccessKey = strings.TrimSpace(custom.AccessKey)
	}
	if custom.SecretKey != "" {
		merged.SecretKey = strings.TrimSpace(custom.SecretKey)
	}
	if custom.PublicBaseURL != "" {
		merged.PublicBaseURL = strings.TrimRight(custom.PublicBaseURL, "/")
	}
	if custom.OutputPrefix != "" {
		merged.OutputPrefix = strings.Trim(custom.OutputPrefix, "/")
	}
	if custom.MaxFileBytes > 0 {
		merged.MaxFileBytes = custom.MaxFileBytes
	}
	if custom.DownloadTimeoutSeconds > 0 {
		merged.DownloadTimeoutSeconds = custom.DownloadTimeoutSeconds
	}
	if custom.Concurrency > 0 {
		merged.Concurrency = custom.Concurrency
	}
	if custom.SystemOnly {
		merged.SystemOnly = true
	}
	if len(custom.SkipDomains) > 0 {
		merged.SkipDomains = custom.SkipDomains
	}

	return merged
}

// LoadConfig parses raw YAML bytes and merges user settings on top of DefaultConfig.
func LoadConfig(rawYAML []byte) Config {
	if len(rawYAML) == 0 {
		return DefaultConfig()
	}

	var custom Config
	if err := yaml.Unmarshal(rawYAML, &custom); err != nil {
		return DefaultConfig()
	}

	return MergeWithDefaults(custom)
}

// Validate verifies required credentials for initializing the storage client.
func (c Config) Validate() error {
	if c.Endpoint == "" {
		return errors.New("tos endpoint is required")
	}
	if c.Bucket == "" {
		return errors.New("tos bucket is required")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return errors.New("tos access_key and secret_key are required")
	}
	return nil
}

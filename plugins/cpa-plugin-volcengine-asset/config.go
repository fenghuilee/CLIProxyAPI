package main

import (
	"path/filepath"
	"strings"
	"time"
)

// Config defines the configuration for the Volcengine Asset plugin.
type Config struct {
	AccessKey          string   `yaml:"access_key" json:"access_key"`
	SecretKey          string   `yaml:"secret_key" json:"secret_key"`
	GroupID            string   `yaml:"group_id" json:"group_id"`
	Region             string   `yaml:"region" json:"region"`
	Project            string   `yaml:"project" json:"project"`
	Models             []string `yaml:"models" json:"models"`
	PollIntervalMs     int      `yaml:"poll_interval_ms" json:"poll_interval_ms"`
	PollTimeoutSeconds int      `yaml:"poll_timeout_seconds" json:"poll_timeout_seconds"`
	CacheTTLSeconds    int      `yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
}

// Normalize populates default values for missing configuration fields.
func (c *Config) Normalize() {
	if c.Region == "" {
		c.Region = "cn-beijing"
	}
	if c.Project == "" {
		c.Project = "default"
	}
	if len(c.Models) == 0 {
		c.Models = []string{"volcengine/doubao-seedance-*"}
	}
	if c.PollIntervalMs <= 0 {
		c.PollIntervalMs = 800
	}
	if c.PollTimeoutSeconds <= 0 {
		c.PollTimeoutSeconds = 60
	}
	if c.CacheTTLSeconds <= 0 {
		c.CacheTTLSeconds = 86400
	}
}

// DefaultConfig provides sensible defaults.
func DefaultConfig() Config {
	return Config{
		Region:  "cn-beijing",
		Project: "default",
		Models: []string{
			"volcengine/doubao-seedance-*",
		},
		PollIntervalMs:     800,
		PollTimeoutSeconds: 60,
		CacheTTLSeconds:    86400,
	}
}

// MatchesModel returns true if the given model matches any configured model pattern.
func (c *Config) MatchesModel(model string) bool {
	modelLower := strings.ToLower(strings.TrimSpace(model))
	if modelLower == "" {
		return false
	}

	models := c.Models
	if len(models) == 0 {
		models = DefaultConfig().Models
	}

	for _, pattern := range models {
		patternLower := strings.ToLower(strings.TrimSpace(pattern))
		if patternLower == "" {
			continue
		}
		if matched, _ := filepath.Match(patternLower, modelLower); matched {
			return true
		}
	}
	return false
}

// PollInterval returns the duration between asset status checks.
func (c *Config) PollInterval() time.Duration {
	if c.PollIntervalMs <= 0 {
		return 800 * time.Millisecond
	}
	return time.Duration(c.PollIntervalMs) * time.Millisecond
}

// PollTimeout returns the maximum duration to wait for an asset to become active.
func (c *Config) PollTimeout() time.Duration {
	if c.PollTimeoutSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.PollTimeoutSeconds) * time.Second
}

// CacheTTL returns the memory cache TTL duration.
func (c *Config) CacheTTL() time.Duration {
	if c.CacheTTLSeconds <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.CacheTTLSeconds) * time.Second
}

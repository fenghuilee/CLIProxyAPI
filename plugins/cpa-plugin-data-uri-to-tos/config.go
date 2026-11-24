package main

// Config holds configuration for the Data URI to TOS plugin.
type Config struct {
	Enabled       bool   `json:"enabled" yaml:"enabled"`
	Priority      int    `json:"priority" yaml:"priority"`
	Endpoint      string `json:"endpoint" yaml:"endpoint"`
	Region        string `json:"region" yaml:"region"`
	Bucket        string `json:"bucket" yaml:"bucket"`
	AccessKey     string `json:"access-key" yaml:"access-key"`
	SecretKey     string `json:"secret-key" yaml:"secret-key"`
	PublicBaseURL string `json:"public-base-url" yaml:"public-base-url"`
	InputPrefix   string `json:"input-prefix" yaml:"input-prefix"`
	OutputPrefix  string `json:"output-prefix" yaml:"output-prefix"`
	ObjectPrefix  string `json:"object-prefix" yaml:"object-prefix"`
	MaxImageBytes int64  `json:"max-image-bytes" yaml:"max-image-bytes"`
	MaxTotalBytes int64  `json:"max-total-bytes" yaml:"max-total-bytes"`
}

// DefaultConfig returns default values for plugin configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		Region:        "cn-beijing",
		Endpoint:      "https://tos-cn-beijing.volces.com",
		InputPrefix:   "aigc/inputs",
		OutputPrefix:  "aigc/outputs",
		ObjectPrefix:  "aigc/inputs",
		MaxImageBytes: 20 * 1024 * 1024, // 20 MB
		MaxTotalBytes: 50 * 1024 * 1024, // 50 MB
	}
}

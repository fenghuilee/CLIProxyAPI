package main

import (
	"path/filepath"
	"strings"
)

// Config defines the unified configuration for all AIGC adapters.
type Config struct {
	QwenImage          QwenImageConfig          `yaml:"qwen-image"`
	VolcengineSeedance VolcengineSeedanceConfig `yaml:"volcengine-seedance"`
	VolcengineSeedream VolcengineSeedreamConfig `yaml:"volcengine-seedream"`
	OpenAICompat       OpenAICompatConfig       `yaml:"openai-compat"`
}

// QwenImageConfig defines configuration for the Alibaba DashScope Qwen-Image adapter.
type QwenImageConfig struct {
	Enabled        *bool    `yaml:"enabled,omitempty"`
	Models         []string `yaml:"models,omitempty"`
	Endpoint       string   `yaml:"endpoint,omitempty"`
	PromptExtend   *bool    `yaml:"prompt-extend,omitempty"`
	EnableThinking *bool    `yaml:"enable-thinking,omitempty"`
}

// VolcengineSeedanceConfig defines configuration for the Volcengine Doubao Seedance video adapter.
type VolcengineSeedanceConfig struct {
	Enabled  *bool    `yaml:"enabled,omitempty"`
	Models   []string `yaml:"models,omitempty"`
	Endpoint string   `yaml:"endpoint,omitempty"`
}

// VolcengineSeedreamConfig defines configuration for the Volcengine Doubao Seedream image/layer adapter.
type VolcengineSeedreamConfig struct {
	Enabled  *bool    `yaml:"enabled,omitempty"`
	Models   []string `yaml:"models,omitempty"`
	Endpoint string   `yaml:"endpoint,omitempty"`
}

// OpenAICompatConfig defines configuration for the universal OpenAI-compatible adapter.
type OpenAICompatConfig struct {
	Enabled               *bool    `yaml:"enabled,omitempty"`
	ImageModels           []string `yaml:"image-models,omitempty"`
	VideoModels           []string `yaml:"video-models,omitempty"`
	Fallback              *bool    `yaml:"fallback,omitempty"`
	ImageSubmitPath       string   `yaml:"image-submit-path,omitempty"`
	ImageEditPath         string   `yaml:"image-edit-path,omitempty"`
	VideoSubmitPath       string   `yaml:"video-submit-path,omitempty"`
	VideoPollPathTemplate string   `yaml:"video-poll-path-template,omitempty"`
}

const (
	defaultQwenEndpoint             = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
	defaultQwenTasksBaseURL         = "https://dashscope.aliyuncs.com/api/v1/tasks"
	defaultVolcengineArkTasksURL    = "/contents/generations/tasks"
	defaultVolcengineArkImageGenURL = "/images/generations"

	defaultOpenAIImageSubmitPath = "/images/generations"
	defaultOpenAIImageEditPath   = "/images/edits"
	defaultOpenAIVideoSubmitPath = "/videos/generations"
	defaultOpenAIVideoPollPath   = "/videos/generations/%s"
)

// DefaultConfig returns the default configuration with all built-in adapters enabled.
func DefaultConfig() Config {
	t := true

	return Config{
		QwenImage: QwenImageConfig{
			Enabled: &t,
			Models: []string{
				"alibaba-cn/qwen-image-*",
				"alibaba/qwen-image-*",
				"qwen/qwen-image-*",
				"qwen-image-*",
				"wanx2.1-t2i-*",
				"wanx2.0-t2i-*",
			},
			Endpoint:       defaultQwenEndpoint,
			PromptExtend:   &t,
			EnableThinking: &t,
		},
		VolcengineSeedance: VolcengineSeedanceConfig{
			Enabled: &t,
			Models: []string{
				"volcengine/doubao-seedance-*",
				"doubao-seedance-*",
			},
			Endpoint: defaultVolcengineArkTasksURL,
		},
		VolcengineSeedream: VolcengineSeedreamConfig{
			Enabled: &t,
			Models: []string{
				"volcengine/doubao-seedream-*",
				"doubao-seedream-*",
			},
			Endpoint: defaultVolcengineArkImageGenURL,
		},
		OpenAICompat: OpenAICompatConfig{
			Enabled:               &t,
			Fallback:              &t,
			ImageModels:           []string{"*"},
			VideoModels:           []string{"*"},
			ImageSubmitPath:       defaultOpenAIImageSubmitPath,
			ImageEditPath:         defaultOpenAIImageEditPath,
			VideoSubmitPath:       defaultOpenAIVideoSubmitPath,
			VideoPollPathTemplate: defaultOpenAIVideoPollPath,
		},
	}
}

// MatchesModelPattern checks whether a model matches any pattern in the list (case-insensitive).
func MatchesModelPattern(patterns []string, model string) bool {
	modelLower := strings.ToLower(strings.TrimSpace(model))
	if modelLower == "" {
		return false
	}
	for _, p := range patterns {
		pLower := strings.ToLower(strings.TrimSpace(p))
		if pLower == "" {
			continue
		}
		if pLower == "*" || pLower == modelLower {
			return true
		}
		if matched, _ := filepath.Match(pLower, modelLower); matched {
			return true
		}
	}
	return false
}

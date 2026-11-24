package main

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config separates channel authentication from shared lifecycle settings.
type Config struct {
	Volcengine   *VolcengineConfig   `yaml:"volcengine,omitempty" json:"volcengine,omitempty"`
	ChinaDataPay *ChinaDataPayConfig `yaml:"chinadatapay,omitempty" json:"chinadatapay,omitempty"`
	Runtime      RuntimeConfig       `yaml:"runtime" json:"runtime"`
}

// ChannelConfig contains account settings, never protocol addresses.
type ChannelConfig struct {
	Enabled   *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Models    []string `yaml:"models,omitempty" json:"models,omitempty"`
	AccessKey string   `yaml:"access-key" json:"access-key"`
	SecretKey string   `yaml:"secret-key" json:"secret-key"`
	Project   string   `yaml:"project,omitempty" json:"project,omitempty"`
	GroupID   string   `yaml:"group-id" json:"group-id"`
}

type VolcengineConfig struct {
	ChannelConfig `yaml:",inline"`
	Region        string `yaml:"region,omitempty" json:"region,omitempty"`
}

// ChinaDataPayConfig uses platform authentication; the server owns project routing.
type ChinaDataPayConfig struct {
	Enabled *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Models  []string `yaml:"models,omitempty" json:"models,omitempty"`
	Token   string   `yaml:"token" json:"token"`
	GroupID string   `yaml:"group-id,omitempty" json:"group-id,omitempty"`
}

func (c ChinaDataPayConfig) enabled() bool { return c.Enabled == nil || *c.Enabled }

func (c *ChinaDataPayConfig) normalize() {
	if len(c.Models) == 0 {
		c.Models = []string{"cdp/doubao-seedance-*"}
	}
	c.Models = withCDPModelAliases(c.Models)
	c.GroupID = strings.TrimSpace(c.GroupID)
}

// withCDPModelAliases keeps existing channel patterns valid without widening suffixes.
func withCDPModelAliases(patterns []string) []string {
	models := append([]string(nil), patterns...)
	for _, pattern := range patterns {
		prefix, suffix, ok := strings.Cut(strings.TrimSpace(pattern), "/")
		if !ok {
			continue
		}
		var alias string
		switch strings.ToLower(prefix) {
		case "cdp":
			alias = "chinadatapay/" + suffix
		case "chinadatapay":
			alias = "cdp/" + suffix
		default:
			continue
		}
		if !slices.ContainsFunc(models, func(model string) bool { return strings.EqualFold(strings.TrimSpace(model), alias) }) {
			models = append(models, alias)
		}
	}
	return models
}

func (c ChinaDataPayConfig) validate() error {
	if !c.enabled() {
		return nil
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("chinadatapay requires token")
	}
	if strings.ContainsAny(c.Token, "\r\n") {
		return fmt.Errorf("chinadatapay has invalid token")
	}
	return validateModelPatterns("chinadatapay", c.Models)
}

type RuntimeConfig struct {
	PollIntervalMs     int `yaml:"poll-interval-ms" json:"poll-interval-ms"`
	PollTimeoutSeconds int `yaml:"poll-timeout-seconds" json:"poll-timeout-seconds"`
	CacheTTLSeconds    int `yaml:"cache-ttl-seconds" json:"cache-ttl-seconds"`
}

// WorkflowConfig is internal: the lifecycle never receives credentials.
type WorkflowConfig struct {
	Models []string
	RuntimeConfig
}

func (c ChannelConfig) enabled() bool { return c.Enabled == nil || *c.Enabled }

func (c *ChannelConfig) normalize(prefix string) {
	if c.Project == "" {
		c.Project = "default"
	}
	if len(c.Models) == 0 {
		c.Models = []string{prefix + "/doubao-seedance-*"}
	}
}

func (c *Config) Normalize() {
	if c.Runtime.PollIntervalMs <= 0 {
		c.Runtime.PollIntervalMs = 1200
	}
	if c.Runtime.PollTimeoutSeconds <= 0 {
		c.Runtime.PollTimeoutSeconds = 60
	}
	if c.Runtime.CacheTTLSeconds <= 0 {
		c.Runtime.CacheTTLSeconds = 86400
	}
	if c.Volcengine != nil {
		c.Volcengine.normalize("volcengine")
		if c.Volcengine.Region == "" {
			c.Volcengine.Region = "cn-beijing"
		}
	}
	if c.ChinaDataPay != nil {
		c.ChinaDataPay.normalize()
	}
}

func DefaultConfig() Config {
	c := Config{}
	c.Normalize()
	return c
}

func DefaultWorkflowConfig() WorkflowConfig {
	return WorkflowConfig{Models: []string{"volcengine/doubao-seedance-*"}, RuntimeConfig: DefaultConfig().Runtime}
}

func (c ChannelConfig) validate(name string) error {
	if !c.enabled() {
		return nil
	}
	if strings.TrimSpace(c.AccessKey) == "" || strings.TrimSpace(c.SecretKey) == "" {
		return fmt.Errorf("%s requires access_key and secret_key", name)
	}
	if strings.ContainsAny(c.AccessKey+c.SecretKey, "\r\n") {
		return fmt.Errorf("%s has invalid credentials", name)
	}
	if strings.TrimSpace(c.GroupID) == "" {
		return fmt.Errorf("%s requires group_id; provision the asset group first", name)
	}
	return validateModelPatterns(name, c.Models)
}

func validateModelPatterns(name string, models []string) error {
	for _, pattern := range models {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("%s has an empty model pattern", name)
		}
		if _, err := filepath.Match(strings.ToLower(pattern), ""); err != nil {
			return fmt.Errorf("%s has an invalid model pattern", name)
		}
	}
	return nil
}

func (c Config) Validate() error {
	if c.Volcengine != nil {
		if err := c.Volcengine.validate("volcengine"); err != nil {
			return err
		}
	}
	if c.ChinaDataPay != nil {
		if err := c.ChinaDataPay.validate(); err != nil {
			return err
		}
	}
	return nil
}

// ParseConfig rejects removed keys instead of silently disabling registration.
func ParseConfig(raw []byte) (Config, error) {
	c := DefaultConfig()
	if len(bytes.TrimSpace(raw)) != 0 {
		// The host forwards its instance settings alongside channel configuration.
		// Accept those settings without mixing them into channel routing or caches.
		instance := struct {
			Config   `yaml:",inline"`
			Enabled  *bool          `yaml:"enabled,omitempty"`
			Priority int            `yaml:"priority,omitempty"`
			Store    map[string]any `yaml:"store,omitempty"`
		}{Config: c}
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		if err := decoder.Decode(&instance); err != nil {
			return Config{}, fmt.Errorf("invalid asset configuration: use volcengine AK/SK, chinadatapay token and runtime blocks; legacy top-level settings, endpoint and chinadatapay AK/SK/project/region are unsupported")
		}
		c = instance.Config
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return Config{}, fmt.Errorf("asset configuration must contain one YAML document")
		}
	}
	c.Normalize()
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c WorkflowConfig) MatchesModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	for _, pattern := range c.Models {
		if matched, _ := filepath.Match(strings.ToLower(strings.TrimSpace(pattern)), model); matched {
			return true
		}
	}
	return false
}

func (c RuntimeConfig) PollInterval() time.Duration {
	if c.PollIntervalMs <= 0 {
		return 1200 * time.Millisecond
	}
	return time.Duration(c.PollIntervalMs) * time.Millisecond
}
func (c RuntimeConfig) PollTimeout() time.Duration {
	if c.PollTimeoutSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.PollTimeoutSeconds) * time.Second
}
func (c RuntimeConfig) CacheTTL() time.Duration {
	if c.CacheTTLSeconds <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.CacheTTLSeconds) * time.Second
}

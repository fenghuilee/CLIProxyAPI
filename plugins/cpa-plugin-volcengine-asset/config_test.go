package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"gopkg.in/yaml.v3"
)

const validAssetConfig = `volcengine:
  access-key: direct-ak
  secret-key: direct-sk
  group-id: direct-group
chinadatapay:
  token: relay-token
  group-id: relay-group
runtime:
  poll-interval-ms: 1500
`

func TestConfigChannelsAndDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(validAssetConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Volcengine.Region != "cn-beijing" || cfg.Volcengine.Project != "default" || cfg.ChinaDataPay.Token != "relay-token" {
		t.Fatal("incorrect channel defaults")
	}
	if cfg.Runtime.PollIntervalMs != 1500 || cfg.Runtime.PollTimeoutSeconds != 60 || cfg.Runtime.CacheTTLSeconds != 86400 {
		t.Fatal("incorrect shared defaults")
	}
	router := NewAssetRouter(cfg, nil)
	for _, route := range router.routes {
		if route.initErr != nil {
			t.Fatal(route.initErr)
		}
		if route.cfg.MatchesModel("chinadatapay/MiniMax-H3") || route.cfg.MatchesModel("chinadatapay/wan3.0-video") {
			t.Fatal("default patterns too broad")
		}
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]map[string]any
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 {
		t.Fatal("expected only two channels and runtime")
	}
	for _, name := range []string{"volcengine", "chinadatapay"} {
		for _, removed := range []string{"endpoint", "base_url"} {
			if _, exists := fields[name][removed]; exists {
				t.Fatalf("removed option remains: %s.%s", name, removed)
			}
		}
	}
	for _, removed := range []string{"access_key", "secret_key", "project", "region"} {
		if _, exists := fields["chinadatapay"][removed]; exists {
			t.Fatalf("gateway exposes unsupported setting: %s", removed)
		}
	}
}

func TestHostInstanceConfigRegistration(t *testing.T) {
	for _, channel := range []string{
		"volcengine:\n  enabled: true\n  access-key: test-ak\n  secret-key: test-sk\n  project: sdapi\n  group-id: test-group\n",
		"chinadatapay:\n  token: test-token\n",
	} {
		for _, host := range []string{
			"enabled: true\npriority: 2\n",
			"enabled: true\npriority: 2\nstore:\n  version: 2026.10.13\n  release-tag: v2026.10.13\n",
		} {
			cfg, err := ParseConfig([]byte(host + channel))
			if err != nil {
				t.Fatal(err)
			}
			if len(NewAssetRouter(cfg, nil).routes) != 1 {
				t.Fatal("host settings changed channel routing")
			}
			p := &Plugin{}
			raw, err := json.Marshal(rpcLifecycleRequest{ConfigYAML: []byte(host + channel)})
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				OK     bool            `json:"ok"`
				Result rpcRegistration `json:"result"`
			}
			if err := json.Unmarshal(p.handleRegister(raw), &response); err != nil {
				t.Fatal(err)
			}
			if !response.OK || response.Result.Metadata.Name != "volcengine-asset" || len(response.Result.Metadata.ConfigFields) != 3 || response.Result.Capabilities["content_generation_mutator"] != true {
				t.Fatal("host configuration failed to register plugin metadata and capabilities")
			}
			encoded, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := yaml.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"enabled", "priority", "store"} {
				if _, exists := fields[key]; exists {
					t.Fatal("host settings leaked into channel config")
				}
			}
		}
	}
	for _, invalid := range []string{
		"enabled: true\npriority: 2\naccess_key: sensitive-legacy-value\n",
		"enabled: true\npriority: 2\nchinadatapay:\n  token: test-token\n  endpoint: sensitive-legacy-value\n",
		"enabled: true\npriority: 2\nunknown: sensitive-legacy-value\n",
	} {
		if _, err := ParseConfig([]byte(invalid)); err == nil || strings.Contains(err.Error(), "sensitive-legacy-value") {
			t.Fatal("host compatibility weakened strict configuration validation or exposed a secret")
		}
	}
}

func TestConfigRejectsRemovedAndInvalidSettings(t *testing.T) {
	for _, raw := range []string{
		"access_key: sensitive-legacy-value\n",
		"poll_interval_ms: 1200\n",
		"chinadatapay:\n  secret_key: sensitive-legacy-value\n",
		"chinadatapay:\n  endpoint: https://example.invalid\n",
		"chinadatapay:\n  base_url: https://example.invalid\n",
		"chinadatapay:\n  region: cn-shanghai\n",
		"chinadatapay:\n  project: default\n",
		"chinadatapay: {}\n",
		"chinadatapay:\n  access_key: ak\n  secret_key: sk\n",
		"chinadatapay:\n  token: test-token\n  models: ['[']\n",
		"chinadatapay:\n  token: ''\n",
		"chinadatapay:\n  token: \"test\\ninvalid\"\n",
		"runtime: [sensitive-legacy-value]\n",
		"{}\n---\n{}\n",
	} {
		_, err := ParseConfig([]byte(raw))
		if err == nil {
			t.Fatalf("invalid configuration accepted: %s", raw)
		}
		if strings.Contains(err.Error(), "sensitive-legacy-value") {
			t.Fatal("configuration error exposed a secret")
		}
	}
}

func TestDisabledChannelAndCustomModels(t *testing.T) {
	cfg, err := ParseConfig([]byte("chinadatapay:\n  enabled: false\n"))
	if err != nil || len(NewAssetRouter(cfg, nil).routes) != 0 {
		t.Fatalf("disabled channel requires credentials: %v", err)
	}
	cfg, err = ParseConfig(nil)
	if err != nil || len(NewAssetRouter(cfg, nil).routes) != 0 {
		t.Fatal("empty config enabled a channel")
	}
	relay := testChinaDataPayConfig()
	relay.Models = []string{"custom/seedance-*"}
	cfg.ChinaDataPay = &relay
	router := NewAssetRouter(cfg, nil)
	if !router.routes[0].cfg.MatchesModel("CUSTOM/SEEDANCE-2.0") || router.routes[0].cfg.MatchesModel("chinadatapay/doubao-seedance-2.0") {
		t.Fatal("custom model patterns ignored")
	}
}

func TestAdapterScopesAndAmbiguousRouting(t *testing.T) {
	cfg, err := ParseConfig([]byte(validAssetConfig))
	if err != nil {
		t.Fatal(err)
	}
	initial := NewAssetRouter(cfg, nil)
	base := initial.routes[1].scope
	for _, field := range []string{"account", "group"} {
		copyCfg, err := ParseConfig([]byte(validAssetConfig))
		if err != nil {
			t.Fatal(err)
		}
		switch field {
		case "account":
			copyCfg.ChinaDataPay.Token = "another-token"
		case "group":
			copyCfg.ChinaDataPay.GroupID = "another-group"
		}
		if NewAssetRouter(copyCfg, nil).routes[1].scope == base {
			t.Fatalf("%s not isolated", field)
		}
	}
	cfg.ChinaDataPay.Token = "  relay-token  "
	if NewAssetRouter(cfg, nil).routes[1].scope != base {
		t.Fatal("Token normalization changed task scope")
	}
	if initial.routes[0].scope == base {
		t.Fatal("channels share a cache scope")
	}
	cfg.Volcengine.Models = []string{"*/*"}
	cfg.ChinaDataPay.Models = []string{"*/*"}
	req := aigc.GenerationMutationRequest{Phase: aigc.PhaseBeforeCreate, Draft: &aigc.ContentGenerationDraft{Model: "custom/seedance", Input: []byte(`{}`)}}
	if _, err := NewAssetRouter(cfg, nil).MutateContentGeneration(context.Background(), req); err == nil {
		t.Fatal("ambiguous routing accepted")
	}
}

func TestReconfigureValidationPreservesActiveRouter(t *testing.T) {
	p := &Plugin{}
	register := func(config string) []byte {
		raw, err := json.Marshal(rpcLifecycleRequest{ConfigYAML: []byte(config)})
		if err != nil {
			t.Fatal(err)
		}
		return p.handleRegister(raw)
	}
	register(validAssetConfig)
	if p.mutator == nil || len(p.mutator.routes) != 2 {
		t.Fatal("valid configuration not installed")
	}
	original := p.mutator
	for _, invalid := range []string{"chinadatapay: {}", "chinadatapay:\n  secret_key: private-old-token"} {
		response := register(invalid)
		if !strings.Contains(string(response), "invalid_config") || strings.Contains(string(response), "private-old-token") {
			t.Fatal("invalid config response or leaked credential")
		}
		if p.mutator != original {
			t.Fatal("failed reconfiguration replaced active router")
		}
	}
	response := register("chinadatapay:\n  enabled: false\n")
	if strings.Contains(string(response), "invalid_config") || len(p.mutator.routes) != 0 {
		t.Fatal("channel could not be disabled")
	}
}

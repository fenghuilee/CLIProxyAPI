package main

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func TestPlugin_Register(t *testing.T) {
	reqBytes, _ := json.Marshal(rpcLifecycleRequest{
		ConfigYAML: []byte(`
region: cn-beijing
bucket: test-bucket
output-prefix: aigc/outputs
concurrency: 4
`),
	})

	respBytes, _ := handleMethod(pluginabi.MethodPluginRegister, reqBytes)
	if len(respBytes) == 0 {
		t.Fatal("empty response from register")
	}

	var env pluginabi.Envelope
	if err := json.Unmarshal(respBytes, &env); err != nil {
		t.Fatalf("unmarshal register envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("register envelope not ok: %+v", env.Error)
	}

	var reg rpcRegistration
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}

	if reg.Metadata.Name != "aigc-media-persist" {
		t.Errorf("expected plugin name 'aigc-media-persist', got '%s'", reg.Metadata.Name)
	}
	if reg.Capabilities["content_generation_mutator"] != true {
		t.Errorf("expected content_generation_mutator capability")
	}
}

func TestPlugin_MutatePassThroughWhenUnconfigured(t *testing.T) {
	p := getPlugin()
	p.mu.Lock()
	p.mutator = nil // Simulate unconfigured state
	p.mu.Unlock()

	reqBytes, _ := json.Marshal(aigc.GenerationMutationRequest{
		Phase: aigc.PhaseBeforeComplete,
		Generation: aigc.ContentGeneration{
			ID: "cg_unconfigured",
			Artifacts: []aigc.ContentGenerationArtifact{
				{
					GenerationID:    "cg_unconfigured",
					ArtifactType:    "output_video",
					StorageProvider: "volcengine",
					URI:             "https://example.com/temp.mp4",
				},
			},
		},
	})

	respBytes, _ := handleMethod(pluginabi.MethodContentGenerationMutate, reqBytes)
	var env pluginabi.Envelope
	if err := json.Unmarshal(respBytes, &env); err != nil {
		t.Fatalf("unmarshal mutate envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("mutate envelope not ok: %+v", env.Error)
	}

	var resp aigc.GenerationMutationResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("unmarshal mutate response: %v", err)
	}

	if len(resp.Artifacts) != 1 || resp.Artifacts[0].StorageProvider != "volcengine" {
		t.Errorf("expected pass-through when unconfigured, got: %+v", resp)
	}
}

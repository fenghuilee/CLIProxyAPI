package driver_test

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestValidateOpenAIInput(t *testing.T) {
	// 1. Empty body is valid
	if err := driver.ValidateOpenAIInput(aigc.ContentKindImage, nil); err != nil {
		t.Fatalf("expected nil for empty body, got %v", err)
	}

	// 2. Valid standard video keys
	validVideo := []byte(`{"prompt":"test","seconds":5,"size":"1280x720","model":"m","extra_body":{"key":"val"}}`)
	if err := driver.ValidateOpenAIInput(aigc.ContentKindVideo, validVideo); err != nil {
		t.Fatalf("expected nil for valid video keys, got %v", err)
	}

	// 3. Unrecognized key in video rejected
	invalidVideo := []byte(`{"prompt":"test","generate_audio":true}`)
	err := driver.ValidateOpenAIInput(aigc.ContentKindVideo, invalidVideo)
	if err == nil || !strings.Contains(err.Error(), "unrecognized parameter") || !strings.Contains(err.Error(), "generate_audio") {
		t.Fatalf("expected unrecognized parameter error, got %v", err)
	}

	// 4. Invalid JSON
	if err := driver.ValidateOpenAIInput(aigc.ContentKindImage, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

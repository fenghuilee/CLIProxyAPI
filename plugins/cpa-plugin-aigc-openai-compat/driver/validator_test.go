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

func TestValidateOpenAIInputImage26Controls(t *testing.T) {
	input := []byte(`{"model":"ze/gpt-image-2.6","prompt":"a star","output_format":"gif","frame_count":4,"frame_duration_ms":150,"complexity":"medium","background":"transparent"}`)
	if err := driver.ValidateOpenAIInput(aigc.ContentKindImage, input); err != nil {
		t.Fatalf("expected image output controls to be accepted, got %v", err)
	}
	if err := driver.ValidateOpenAIInput(aigc.ContentKindVideo, input); err == nil {
		t.Fatal("image output controls must not expand the video whitelist")
	}
	if err := driver.ValidateOpenAIInput(aigc.ContentKindImage, []byte(`{"output_format":"gif","frame_duration":150}`)); err == nil || !strings.Contains(err.Error(), "frame_duration") {
		t.Fatalf("expected incorrect frame parameter name to be rejected, got %v", err)
	}
	if err := driver.ValidateOpenAIInput(aigc.ContentKindImage, []byte(`[]`)); err == nil {
		t.Fatal("expected non-object input to be rejected")
	}
}

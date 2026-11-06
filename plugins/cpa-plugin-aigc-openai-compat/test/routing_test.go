package test

import (
	"context"
	"testing"

	aigcdriver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestDriver_Supports(t *testing.T) {
	ctx := context.Background()
	driver := aigcdriver.NewDriver()

	tests := []struct {
		kind         aigc.ContentKind
		model        string
		wantSupport  bool
		wantProvider string
	}{
		// 1. Qwen Image
		{aigc.ContentKindImage, "qwen/qwen-image-3.0-pro", true, "qwen-image"},
		{aigc.ContentKindImage, "alibaba/qwen-image-max", true, "qwen-image"},
		{aigc.ContentKindImage, "qwen/qwen-image-plus", true, "qwen-image"},
		{aigc.ContentKindImage, "qwen/wanx2.1-t2i-turbo", true, "qwen-image"},
		{aigc.ContentKindVideo, "qwen/qwen-image-3.0-pro", false, ""},

		// 2. Qwen Wan Video
		{aigc.ContentKindVideo, "qwen/wan3.0-video", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba/wan3.0-video-prime", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba-cn/wan2.1-i2v-plus", true, "qwen-wan"},
		{aigc.ContentKindVideo, "alibaba/wanx2.1-t2v-turbo", true, "qwen-wan"},
		{aigc.ContentKindImage, "qwen/wan3.0-video", false, ""},

		// 3. Volcengine Seedance Video
		{aigc.ContentKindVideo, "volcengine/doubao-seedance-2-5", true, "volcengine-seedance"},
		{aigc.ContentKindVideo, "volcengine/doubao-seedance-2-0-mini", true, "volcengine-seedance"},
		{aigc.ContentKindImage, "volcengine/doubao-seedance-2-5", false, ""},

		// 4. Volcengine Seedream Image
		{aigc.ContentKindImage, "volcengine/doubao-seedream-5-0-pro", true, "volcengine-seedream"},
		{aigc.ContentKindImage, "volcengine/doubao-seedream-4-5", true, "volcengine-seedream"},
		{aigc.ContentKindImage, "volcengine/doubao-seedream-5-0", true, "volcengine-seedream"},
		{aigc.ContentKindVideo, "volcengine/doubao-seedream-5-0-pro", false, ""},

		// 5. Bare models explicit fallback to openai-compat
		{aigc.ContentKindImage, "qwen-image-plus", true, "openai-compat"},
		{aigc.ContentKindImage, "wanx2.1-t2i-turbo", true, "openai-compat"},
		{aigc.ContentKindVideo, "wan3.0-video", true, "openai-compat"},
		{aigc.ContentKindVideo, "wan3.0-video-prime", true, "openai-compat"},
		{aigc.ContentKindVideo, "wan2.1-i2v-plus", true, "openai-compat"},
		{aigc.ContentKindVideo, "doubao-seedance-2-0-mini", true, "openai-compat"},
		{aigc.ContentKindImage, "doubao-seedream-5-0", true, "openai-compat"},

		// 6. OpenAI Compat (Fallback / Explicit)
		{aigc.ContentKindImage, "openai/dall-e-3", true, "openai-compat"},
		{aigc.ContentKindImage, "dall-e-2", true, "openai-compat"},
		{aigc.ContentKindImage, "zeroapi/gpt-image-2", true, "openai-compat"},
		{aigc.ContentKindImage, "siliconflow/flux-1-dev", true, "openai-compat"},
		{aigc.ContentKindVideo, "openai/sora-1.0", true, "openai-compat"},
		{aigc.ContentKindVideo, "xai/grok-video-1.0", true, "openai-compat"},
		{aigc.ContentKindVideo, "kling-v1.5", true, "openai-compat"},

		// 7. Unsupported kinds
		{aigc.ContentKindAudio, "openai/tts-1", false, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind)+"_"+tt.model, func(t *testing.T) {
			res, err := driver.Supports(ctx, aigc.GenerationSupportRequest{
				Kind:  tt.kind,
				Model: tt.model,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Supported != tt.wantSupport {
				t.Fatalf("Supported = %v, want %v", res.Supported, tt.wantSupport)
			}
			if tt.wantSupport && res.Provider != tt.wantProvider {
				t.Fatalf("Provider = %q, want %q", res.Provider, tt.wantProvider)
			}
		})
	}
}

package openaicompat_test

import (
	"context"
	"testing"

	"github.com/MoeForge/moego/ojson"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestImageOperationUsesExplicitEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name, operation, body, path string
		wantErr                     bool
	}{
		{"generation", "generations", `{"prompt":"a star"}`, "/images/generations", false},
		{"edit", "edits", `{"prompt":"edit","images":["https://example.com/a.png"],"output_format":"gif"}`, "/images/edits", false},
		{"generation does not auto-route", "generations", `{"prompt":"edit","images":["https://example.com/a.png"]}`, "", true},
		{"edit needs image", "edits", `{"prompt":"a star"}`, "", true},
		{"edit needs prompt", "edits", `{"images":["https://example.com/a.png"]}`, "", true},
		{"unknown operation", "unknown", `{"prompt":"a star"}`, "", true},
		{"legacy driver caller", "", `{"prompt":"edit","images":["https://example.com/a.png"]}`, "/images/edits", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := driver.NewDriver().PrepareSubmit(context.Background(), aigc.GenerationSubmitInput{Generation: aigc.ContentGeneration{
				Kind: aigc.ContentKindImage, Model: "ze/gpt-image-2.6", Input: []byte(tc.body),
				Metadata: map[string]any{"image_operation": tc.operation},
			}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil {
				if req.URL != tc.path {
					t.Errorf("endpoint = %q, want %q", req.URL, tc.path)
				}
				if ojson.GetBytes(req.Body, "image_operation").Exists() {
					t.Error("internal operation leaked into upstream payload")
				}
			}
		})
	}
}

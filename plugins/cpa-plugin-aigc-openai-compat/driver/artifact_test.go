package driver_test

import (
	"testing"

	"github.com/MoeForge/moego/base64"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
)

func TestProcessImageArtifactURLFormats(t *testing.T) {
	for _, tc := range []struct{ url, mime string }{
		{"https://example.com/star.GIF?token=abc", "image/gif"},
		{"https://example.com/star.svg?download=star.png", "image/svg+xml"},
		{"https://example.com/star.png?download=star.gif", "image/png"},
		{"https://example.com/star.jpeg", "image/jpeg"},
		{"https://example.com/star.webp", "image/webp"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			artifact := driver.ProcessImageArtifact("output_image", "openai-compat", tc.url, "", nil)
			if artifact.MIMEType != tc.mime || artifact.URI != tc.url {
				t.Fatalf("artifact = %+v, want MIME %s and unchanged URL", artifact, tc.mime)
			}
		})
	}
}

func TestProcessImageArtifactBase64Formats(t *testing.T) {
	for _, tc := range []struct{ data, mime string }{
		{"GIF89aimage-data", "image/gif"},
		{`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h10v10z"/></svg>`, "image/svg+xml"},
		{"\x89PNG\r\n\x1a\nimage-data", "image/png"},
	} {
		t.Run(tc.mime, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString([]byte(tc.data))
			artifact := driver.ProcessImageArtifact("output_image", "openai-compat", "", encoded, nil)
			if artifact.MIMEType != tc.mime || artifact.Metadata["b64_json"] != encoded || artifact.SizeBytes != int64(len(tc.data)) {
				t.Fatalf("artifact = %+v, want MIME %s and unchanged payload", artifact, tc.mime)
			}
		})
	}
}

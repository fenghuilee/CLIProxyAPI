package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestOpenAICompatImage26EditMultipartAndFinalPayloadRules(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nreference-image")
	for _, filterImage := range []bool{false, true} {
		t.Run(fmt.Sprint(filterImage), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/images/edits" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
					t.Errorf("unexpected transport: %s %s", r.URL.Path, r.Header.Get("Content-Type"))
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer func() { _ = r.MultipartForm.RemoveAll() }()
				if r.FormValue("model") != "configured-model" || r.FormValue("quality") != "high" || r.FormValue("output_format") != "gif" || r.FormValue("prompt") != "" {
					t.Errorf("final payload rules were undone: %v", r.MultipartForm.Value)
				}
				files := r.MultipartForm.File["image[]"]
				if filterImage {
					if len(files) != 0 {
						t.Error("filtered image was re-injected")
					}
				} else if len(files) != 1 {
					t.Errorf("image[] count = %d", len(files))
				} else {
					f, err := files[0].Open()
					if err != nil {
						t.Error(err)
						return
					}
					data, err := io.ReadAll(f)
					_ = f.Close()
					if err != nil || !bytes.Equal(data, png) || files[0].Header.Get("Content-Type") != "image/png" {
						t.Error("reference image changed")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"url":"https://example.com/result.gif"}]}`)
			}))
			defer server.Close()
			filters := []string{"prompt"}
			if filterImage {
				filters = append(filters, "image[]")
			}
			cfg := &config.Config{Payload: config.PayloadConfig{
				Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "gpt-image-2.6"}}, Params: map[string]any{"model": "configured-model", "quality": "high"}}},
				Filter:   []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "gpt-image-2.6"}}, Params: filters}},
			}}
			cfg.RequestLog = true
			doc := ojson.NewDocString(`{"model":"gpt-image-2.6","prompt":"edit","quality":"low","output_format":"gif"}`)
			if err := doc.Set("images", []map[string]string{{"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}}); err != nil {
				t.Fatal(err)
			}
			auth := &cliproxyauth.Auth{Provider: "openai-compatibility", Attributes: map[string]string{"base_url": server.URL, "api_key": "test-key"}}
			logContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx := context.WithValue(context.Background(), "gin", logContext)
			_, err := NewOpenAICompatExecutor("openai-compatibility", cfg).Execute(ctx, auth,
				cliproxyexecutor.Request{Model: "gpt-image-2.6", Payload: doc.Bytes()},
				cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-image"), Headers: http.Header{"Content-Type": {"application/json"}}, Metadata: map[string]any{cliproxyexecutor.RequestPathMetadataKey: "/images/edits"}},
			)
			if err != nil || calls != 1 {
				t.Fatalf("execute calls=%d error=%v", calls, err)
			}
			logged, _ := logContext.Get("API_REQUEST")
			text := string(logged.([]byte))
			if !strings.Contains(text, "multipart body omitted") || strings.Contains(text, "reference-image") || strings.Contains(text, "base64,") {
				t.Fatal("reference image content retained in request log")
			}
		})
	}
}

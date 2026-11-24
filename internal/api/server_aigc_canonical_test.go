package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	internalaigc "github.com/router-for-me/CLIProxyAPI/v8/internal/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

func TestReadAIGCCanonicalMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		valid  bool
	}{
		{name: "missing", valid: true},
		{name: "whitespace", header: "  ", valid: true},
		{name: "gateway parameters", header: `{"resolution":"480p","duration":5,"generate_audio":true}`, valid: true},
		{name: "malformed", header: `{`},
		{name: "empty object", header: `{}`},
		{name: "null", header: `null`},
		{name: "array", header: `[]`},
		{name: "scalar", header: `"480p"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/aigc/v1/videos", nil)
			c.Request.Header.Set("X-Canonical-Body", tc.header)
			metadata, ok := readAIGCCanonicalMetadata(c)
			if ok != tc.valid {
				t.Fatalf("valid = %v, want %v", ok, tc.valid)
			}
			if !tc.valid {
				if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"invalid_canonical_body"`) {
					t.Fatalf("unexpected rejection: %d %s", recorder.Code, recorder.Body.String())
				}
				return
			}
			if strings.TrimSpace(tc.header) == "" {
				if metadata != nil {
					t.Fatalf("missing header must not supply canonical metadata: %#v", metadata)
				}
				return
			}
			var expected map[string]any
			if err := json.Unmarshal([]byte(tc.header), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(metadata["canonical"], expected) {
				t.Fatalf("gateway parameters changed: %#v", metadata)
			}
		})
	}
}

func TestAIGCCreateCanonicalHeaderOptional(t *testing.T) {
	for _, mode := range []string{"async", "sync"} {
		for _, header := range []string{"", `{"resolution":"480p"}`, `{}`} {
			t.Run(mode+"/"+header, func(t *testing.T) {
				s := &Server{
					aigcCoordinator:  internalaigc.NewCoordinator(&pluginhost.Host{}, nil),
					aigcSyncPipeline: internalaigc.NewSyncPipeline(nil, nil),
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				// Deliberately omit model/store so no generation can be submitted.
				c.Request = httptest.NewRequest(http.MethodPost, "/aigc/v1/videos", strings.NewReader(`{"prompt":"test"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("X-Canonical-Body", header)
				if mode == "async" {
					s.handleAIGCAsyncCreate(c, aigc.ContentKind("video"))
				} else {
					s.handleAIGCSync(c)
				}
				wantStatus, wantMessage := http.StatusBadRequest, "invalid_canonical_body"
				if header == "" || header == `{"resolution":"480p"}` {
					if mode == "async" {
						wantStatus, wantMessage = http.StatusServiceUnavailable, "aigc store plugin not configured"
					} else {
						wantMessage = "model is required"
					}
				}
				if recorder.Code != wantStatus || !strings.Contains(recorder.Body.String(), wantMessage) {
					t.Fatalf("response = %d %s, want %d containing %q", recorder.Code, recorder.Body.String(), wantStatus, wantMessage)
				}
			})
		}
	}
}

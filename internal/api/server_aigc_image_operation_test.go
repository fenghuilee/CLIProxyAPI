package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	internalaigc "github.com/router-for-me/CLIProxyAPI/v8/internal/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type captureImageOperationDriver struct {
	aigc.ContentGenerationDriver
	operation string
}

func (*captureImageOperationDriver) Supports(context.Context, aigc.GenerationSupportRequest) (aigc.GenerationSupportResponse, error) {
	return aigc.GenerationSupportResponse{Supported: true}, nil
}

func (d *captureImageOperationDriver) PrepareSubmit(_ context.Context, input aigc.GenerationSubmitInput) (aigc.GenerationExecutionRequest, error) {
	d.operation, _ = input.Generation.Metadata["image_operation"].(string)
	return aigc.GenerationExecutionRequest{}, errors.New("test stopped before upstream execution")
}

func TestAIGCImageOperationComesFromHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"generations", "edits"} {
		t.Run(operation, func(t *testing.T) {
			driver := &captureImageOperationDriver{}
			host := pluginhost.NewTestHost(pluginhost.TestCapabilityRecord{ID: "capture", Plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{ContentGenerationDriver: driver}}})
			s := &Server{aigcSyncPipeline: internalaigc.NewSyncPipeline(host, nil)}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			// Client payload cannot override the operation selected by the handler.
			c.Request = httptest.NewRequest(http.MethodPost, "/aigc/v1/images/"+operation, strings.NewReader(`{"model":"ze/gpt-image-2.6","prompt":"edit","images":["https://example.com/a.png"],"image_operation":"client-controlled"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			if operation == "edits" {
				s.aigcEditImageSyncHandler(c)
			} else {
				s.aigcGenerateImageSyncHandler(c)
			}
			if driver.operation != operation || w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "test stopped") {
				t.Fatalf("operation=%q response=%d %s", driver.operation, w.Code, w.Body.String())
			}
		})
	}
}

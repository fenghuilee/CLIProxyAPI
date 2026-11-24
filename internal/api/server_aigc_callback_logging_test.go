package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

func callbackLogTestHook(t *testing.T) *logtest.Hook {
	t.Helper()
	logger := log.StandardLogger()
	oldHooks, oldOutput, oldLevel := logger.Hooks, logger.Out, logger.Level
	logger.ReplaceHooks(make(log.LevelHooks))
	logger.SetOutput(io.Discard)
	logger.SetLevel(log.InfoLevel)
	hook := logtest.NewGlobal()
	t.Cleanup(func() {
		logger.ReplaceHooks(oldHooks)
		logger.SetOutput(oldOutput)
		logger.SetLevel(oldLevel)
	})
	return hook
}

func TestAIGCCallbackLogExchange(t *testing.T) {
	hook := callbackLogTestHook(t)
	engine := gin.New()
	engine.Use(aigcCallbackLoggingMiddleware())
	const path = "/aigc/v1/callbacks/chinadatapay-minimax-h3"
	const body = `{"challenge":"echo-me","token":"body-secret","task":{"url":"https://example.com/video.mp4?X-Amz-Credential=url-secret"}}`
	engine.POST(path, func(c *gin.Context) {
		got, err := io.ReadAll(c.Request.Body)
		if err != nil || string(got) != body {
			t.Errorf("handler received changed body: %q, %v", got, err)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task", "token": "response-secret"})
	})
	req := httptest.NewRequest(http.MethodPost, path+"?gid=cg_test&token=query-secret&challenge=query-challenge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer header-secret")
	req.Header.Set("Cookie", "session=cookie-secret")
	req.Header.Set("X-Signature", "signature-secret")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "response-secret") {
		t.Fatal("logging changed the actual response")
	}
	entry := hook.LastEntry()
	if entry == nil || !strings.HasPrefix(entry.Message, "aigc: callback exchange ") {
		t.Fatal("missing callback log")
	}
	if entry.Data["status"] != http.StatusBadRequest || entry.Data["request_body_complete"] != true {
		t.Fatalf("unexpected metadata: %v", entry.Data)
	}
	text, err := entry.String()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"body-secret", "url-secret", "response-secret", "query-secret", "header-secret", "cookie-secret", "signature-secret"} {
		if strings.Contains(text, secret) {
			t.Errorf("log leaked %s", secret)
		}
	}
	for _, visible := range []string{"echo-me", "query-challenge", "cg_test", "video.mp4"} {
		if !strings.Contains(text, visible) {
			t.Errorf("missing diagnostic value %s", visible)
		}
	}
	formatted, err := (&logging.LogFormatter{}).Format(entry)
	if err != nil || !strings.Contains(string(formatted), "echo-me") || strings.Count(string(formatted), "\n") != 1 {
		t.Fatalf("application log formatter lost or split the callback details: %s, %v", formatted, err)
	}
}

func TestAIGCCallbackLogUnsupportedMethodAndProviderScope(t *testing.T) {
	hook := callbackLogTestHook(t)
	engine := gin.New()
	engine.Use(aigcCallbackLoggingMiddleware())
	engine.POST("/aigc/v1/callbacks/chinadatapay-minimax-h3", func(c *gin.Context) { c.Status(200) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/aigc/v1/callbacks/chinadatapay-minimax-h3?challenge=hello", nil))
	if rec.Code != http.StatusNotFound || hook.LastEntry() == nil || hook.LastEntry().Data["status"] != http.StatusNotFound {
		t.Fatal("unsupported GET was not logged with its original status")
	}
	if !strings.Contains(hook.LastEntry().Data["query"].(string), "hello") {
		t.Fatal("GET challenge was lost")
	}
	hook.Reset()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/aigc/v1/callbacks/volcengine-seedance", nil))
	if hook.LastEntry() != nil {
		t.Fatal("unrelated provider was logged")
	}
}

func TestAIGCCallbackLogBodySanitization(t *testing.T) {
	for _, tt := range []struct {
		name, body, contentType, want string
		truncated                     bool
	}{
		{"form", "challenge=hello&signature=secret", "application/x-www-form-urlencoded", "challenge=hello", false},
		{"malformed", `{"token":"secret"`, "application/json", "[omitted: non-JSON/non-form body]", false},
		{"text", "Bearer secret", "text/plain", "[omitted: non-JSON/non-form body]", false},
		{"truncated", `{"token":"secret"`, "application/json", "[omitted: body exceeds 16 KiB]", true},
		{"nested", `{"items":[{"apiKey":"secret","challenge":"hello"}]}`, "application/json", `"challenge":"hello"`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := callbackLogBody([]byte(tt.body), tt.contentType, tt.truncated)
			if strings.Contains(got, "secret") || !strings.Contains(got, tt.want) {
				t.Fatalf("unexpected sanitized body: %s", got)
			}
		})
	}
}

type callbackLogErrorBody struct{}

func (callbackLogErrorBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (callbackLogErrorBody) Close() error             { return nil }

func TestAIGCCallbackLogBoundedCaptureAndReadError(t *testing.T) {
	large := strings.Repeat("x", callbackLogBodyLimit+100)
	reader := &callbackLogReader{ReadCloser: io.NopCloser(strings.NewReader(large))}
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != large || reader.body.Len() != callbackLogBodyLimit || !reader.truncated {
		t.Fatal("capture changed or retained too much of the request")
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writer := &callbackLogWriter{ResponseWriter: c.Writer}
	_, _ = writer.WriteString(large)
	_, _ = writer.Write([]byte("tail"))
	if rec.Body.String() != large+"tail" || writer.body.Len() != callbackLogBodyLimit || !writer.truncated {
		t.Fatal("capture changed or retained too much of the response")
	}
	failing := &callbackLogReader{ReadCloser: callbackLogErrorBody{}}
	if _, errRead := io.ReadAll(failing); errRead == nil || !failing.failed {
		t.Fatal("read failure was hidden")
	}
}

func TestAIGCCallbackLogDoesNotReadRejectedBody(t *testing.T) {
	hook := callbackLogTestHook(t)
	engine := gin.New()
	engine.Use(aigcCallbackLoggingMiddleware())
	engine.POST("/aigc/v1/callbacks/chinadatapay-minimax-h3", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusServiceUnavailable)
	})
	req := httptest.NewRequest(http.MethodPost, "/aigc/v1/callbacks/chinadatapay-minimax-h3", nil)
	req.Body = callbackLogErrorBody{}
	req.ContentLength = -1
	engine.ServeHTTP(httptest.NewRecorder(), req)
	entry := hook.LastEntry()
	if entry == nil || entry.Data["request_body_bytes_read"] != int64(0) || entry.Data["request_body_read_failed"] != false || entry.Data["request_body_complete"] != false {
		t.Fatal("logging consumed a rejected request body")
	}
}

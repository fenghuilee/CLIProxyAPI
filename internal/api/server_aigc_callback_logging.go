package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

const callbackLogBodyLimit = 16 << 10

var callbackLogURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// Register globally so unsupported methods (including challenge GETs) are visible.
// This observes callbacks only; it does not implement a provider handshake.
func aigcCallbackLoggingMiddleware() gin.HandlerFunc {
	instance, _ := os.Hostname()
	return func(c *gin.Context) {
		if c.Request.URL.Path != "/aigc/v1/callbacks/chinadatapay-minimax-h3" {
			c.Next()
			return
		}
		start := time.Now()
		fields := log.Fields{
			"provider": "chinadatapay-minimax-h3", "instance": instance,
			"request_id": logging.GetGinRequestID(c),
			"method":     c.Request.Method, "path": c.Request.URL.Path,
			"query":          callbackLogQuery(c.Request.URL.RawQuery),
			"content_length": c.Request.ContentLength,
		}
		headers := make(map[string]string)
		for _, key := range []string{"Content-Type", "Content-Encoding", "User-Agent"} {
			if value := c.GetHeader(key); value != "" {
				headers[key] = callbackLogText(value)
			}
		}
		fields["headers"] = headers
		response := &callbackLogWriter{ResponseWriter: c.Writer}
		c.Writer = response
		var requestBody *callbackLogReader
		if c.Request.Body != nil {
			requestBody = &callbackLogReader{ReadCloser: c.Request.Body}
			c.Request.Body = requestBody
		}
		defer func() {
			fields["status"] = response.Status()
			fields["duration_ms"] = time.Since(start).Milliseconds()
			fields["response_body"] = callbackLogBody(response.body.Bytes(), response.Header().Get("Content-Type"), response.truncated)
			fields["response_body_truncated"] = response.truncated
			fields["response_body_captured_bytes"] = response.body.Len()
			if requestBody != nil {
				fields["request_body"] = callbackLogBody(requestBody.body.Bytes(), c.GetHeader("Content-Type"), requestBody.truncated)
				fields["request_body_bytes_read"] = requestBody.total
				fields["request_body_truncated"] = requestBody.truncated
				fields["request_body_read_failed"] = requestBody.failed
				fields["request_body_complete"] = requestBody.eof || (c.Request.ContentLength >= 0 && requestBody.total == c.Request.ContentLength)
			}
			// The application formatter only renders whitelisted fields. Include
			// bounded, sanitized JSON in the message as well as structured fields.
			doc := ojson.NewDoc([]byte(`{}`))
			if err := doc.Set("exchange", fields); err != nil {
				log.Warn("aigc: callback exchange serialization failed")
				return
			}
			log.WithFields(fields).Info("aigc: callback exchange " + ojson.GetBytes(doc.Bytes(), "exchange").Raw)
		}()
		c.Next()
	}
}

// Capture only bytes the handler reads: logging must not drain an unread body or
// delay a rejection/handshake while waiting for an unauthenticated sender.
type callbackLogReader struct {
	io.ReadCloser
	body      bytes.Buffer
	total     int64
	truncated bool
	failed    bool
	eof       bool
}

func (r *callbackLogReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.total += int64(n)
	callbackLogAppend(&r.body, p[:n], &r.truncated)
	r.eof = r.eof || err == io.EOF
	r.failed = r.failed || (err != nil && err != io.EOF)
	return n, err
}

type callbackLogWriter struct {
	gin.ResponseWriter
	body      bytes.Buffer
	truncated bool
}

func (w *callbackLogWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	callbackLogAppend(&w.body, p[:n], &w.truncated)
	return n, err
}

func (w *callbackLogWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	remaining := callbackLogBodyLimit - w.body.Len()
	if n > remaining {
		w.truncated = true
	}
	_, _ = w.body.WriteString(s[:min(n, remaining)])
	return n, err
}

func callbackLogAppend(dst *bytes.Buffer, p []byte, truncated *bool) {
	remaining := callbackLogBodyLimit - dst.Len()
	if len(p) > remaining {
		*truncated = true
		p = p[:remaining]
	}
	_, _ = dst.Write(p)
}

func callbackLogSensitive(key string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"token", "secret", "password", "authorization", "cookie", "signature", "credential", "api_key", "apikey", "api-key"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return key == "key" || key == "sig"
}

func callbackLogQuery(raw string) string {
	if len(raw) > callbackLogBodyLimit {
		return "[omitted: query exceeds 16 KiB]"
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[omitted: invalid query]"
	}
	for key := range values {
		if callbackLogSensitive(key) {
			values.Set(key, "[REDACTED]")
		} else {
			for i, value := range values[key] {
				values[key][i] = callbackLogText(value)
			}
		}
	}
	return values.Encode()
}

func callbackLogText(value string) string {
	if len(value) > callbackLogBodyLimit {
		return "[omitted: value exceeds 16 KiB]"
	}
	return callbackLogURLPattern.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[omitted: invalid URL]"
		}
		parsed.User = nil
		// Signed media URLs may use vendor-specific credential parameter names.
		if parsed.RawQuery != "" {
			parsed.RawQuery = "REDACTED"
		}
		parsed.Fragment = ""
		return parsed.String()
	})
}

func callbackLogValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if callbackLogSensitive(key) {
				v[key] = "[REDACTED]"
			} else {
				v[key] = callbackLogValue(item)
			}
		}
	case []any:
		for i := range v {
			v[i] = callbackLogValue(v[i])
		}
	case string:
		return callbackLogText(v)
	}
	return value
}

func callbackLogBody(body []byte, contentType string, truncated bool) string {
	if len(body) == 0 {
		return ""
	}
	// A partial or malformed document cannot be reliably credential-redacted.
	if truncated {
		return "[omitted: body exceeds 16 KiB]"
	}
	var value any
	if ojson.ValidBytes(body) {
		if err := json.Unmarshal(body, &value); err != nil {
			return "[omitted: invalid JSON]"
		}
		doc := ojson.NewDoc([]byte(`{}`))
		if err := doc.Set("payload", callbackLogValue(value)); err != nil {
			return "[omitted: serialization failed]"
		}
		return ojson.GetBytes(doc.Bytes(), "payload").Raw
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/x-www-form-urlencoded" {
		return callbackLogQuery(string(body))
	}
	return "[omitted: non-JSON/non-form body]"
}

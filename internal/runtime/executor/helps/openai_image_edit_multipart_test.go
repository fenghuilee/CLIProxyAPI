package helps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
)

func TestImageEditMultipartPreservesFilesAndFields(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nreference")
	calls := 0
	payload := []byte(`{"model":"gpt-image-2.6","prompt":"edit","images":[{"image_url":"first"},{"image_url":"second"}],"mask":{"image_url":"mask"},"output_format":"gif","frame_count":4,"frame_duration_ms":150,"complexity":"medium","response_format":"url","n":1}`)
	body, contentType, err := prepareImageEditMultipart(context.Background(), payload, func(_ context.Context, ref string, limit int64) ([]byte, error) {
		want := []string{"first", "second", "mask"}[calls]
		if ref != want || limit != int64(imageEditMaxBytes-calls*len(png)) {
			t.Errorf("fetch %d: ref=%s limit=%d", calls, ref, limit)
		}
		calls++
		return png, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(contentType)
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = form.RemoveAll() }()
	for key, want := range map[string]string{"model": "gpt-image-2.6", "prompt": "edit", "output_format": "gif", "frame_count": "4", "frame_duration_ms": "150", "complexity": "medium", "response_format": "url", "n": "1"} {
		if len(form.Value[key]) != 1 || form.Value[key][0] != want {
			t.Errorf("field %s = %v, want %s", key, form.Value[key], want)
		}
	}
	if len(form.File["image[]"]) != 2 || len(form.File["mask"]) != 1 || len(form.Value["images"]) != 0 {
		t.Fatalf("incorrect image fields: %+v", form)
	}
	for _, files := range form.File {
		for _, file := range files {
			r, errOpen := file.Open()
			if errOpen != nil {
				t.Fatal(errOpen)
			}
			data, errRead := io.ReadAll(r)
			_ = r.Close()
			if errRead != nil || !bytes.Equal(data, png) || file.Header.Get("Content-Type") != "image/png" {
				t.Fatal("file bytes or MIME type changed")
			}
		}
	}
}

func TestImageEditMultipartRejectsBadReferences(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		data       []byte
		fetchErr   error
	}{
		{"missing", `{"prompt":"edit"}`, nil, nil},
		{"empty URL", `{"images":[""]}`, nil, nil},
		{"invalid object", `{"images":[{"other":"x"}]}`, nil, nil},
		{"download error", `{"images":["url"]}`, nil, errors.New("download failed")},
		{"HTML", `{"images":["url"]}`, []byte("<html>not an image</html>"), nil},
		{"empty data", `{"images":["url"]}`, nil, nil},
		{"too large", `{"images":["url"]}`, make([]byte, imageEditMaxBytes+1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := prepareImageEditMultipart(context.Background(), []byte(tc.body), func(context.Context, string, int64) ([]byte, error) { return tc.data, tc.fetchErr })
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestGPTImage26EditMultipartScopeAndDataURI(t *testing.T) {
	payload := []byte(`{"prompt":"edit"}`)
	got, ct, err := PrepareGPTImage26EditMultipart(context.Background(), payload, "gpt-image-2", "application/json")
	if err != nil || !bytes.Equal(got, payload) || ct != "application/json" {
		t.Fatal("legacy model changed")
	}
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nreference"))
	for _, model := range []string{"ze/gpt-image-2.6", "gpt-image-2.6-sunburst", "gpt-image-2.6-flare"} {
		doc := ojson.NewDocString(`{"prompt":"edit"}`)
		if errSet := doc.Set("images", []map[string]string{{"image_url": "data:image/png;base64," + png}}); errSet != nil {
			t.Fatal(errSet)
		}
		_, ct, err = PrepareGPTImage26EditMultipart(context.Background(), doc.Bytes(), model, "application/json")
		if err != nil || !strings.HasPrefix(ct, "multipart/form-data;") {
			t.Fatalf("%s: %s %v", model, ct, err)
		}
	}
}

func TestImageEditReferenceSecurity(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/image", "http://user:pass@example.com/a", "http://127.0.0.1/a", "http://[::1]/a", "http://169.254.169.254/a", "http://10.0.0.1/a"} {
		u, _ := url.Parse(raw)
		if validateImageEditURL(u) == nil {
			t.Errorf("accepted unsafe URL: %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "::ffff:127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "ff02::1", "fd00::1"} {
		if publicImageEditIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted unsafe IP: %s", raw)
		}
	}
	if !publicImageEditIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public IP rejected")
	}
	_, err := dialImageEditReference(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("dial accepted private target")
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	doc := ojson.NewDocString(`{}`)
	if errSet := doc.Set("images", []string{server.URL + "/image.png"}); errSet != nil {
		t.Fatal(errSet)
	}
	_, _, err = PrepareGPTImage26EditMultipart(context.Background(), doc.Bytes(), "gpt-image-2.6", "application/json")
	if err == nil || calls != 0 {
		t.Fatal("private reference was fetched")
	}
}

type imageEditTestTransport func(*http.Request) (*http.Response, error)

func (f imageEditTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type imageEditTestBody struct {
	io.Reader
	closed bool
}

func (b *imageEditTestBody) Close() error { b.closed = true; return nil }

func TestImageEditHTTPDownloadAndRedirects(t *testing.T) {
	for _, tc := range []struct {
		name, redirect string
		wantErr        bool
	}{
		{"public redirect", "https://cdn.example.com/image.png", false},
		{"private redirect", "http://127.0.0.1/image.png", true},
		{"metadata redirect", "http://169.254.169.254/image.png", true},
		{"credential redirect", "https://user:pass@cdn.example.com/image.png", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			png := []byte("\x89PNG\r\n\x1a\nreference")
			client := newImageEditReferenceClient(imageEditTestTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("Referer") != "" {
					t.Error("credentials or source URL leaked to image download")
				}
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {tc.redirect}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(png)), ContentLength: int64(len(png)), Request: req}, nil
			}))
			data, err := fetchImageEditReference(context.Background(), client, "https://images.example.com/source?signature=private", imageEditMaxBytes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("download error=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && calls != 1 {
				t.Fatal("unsafe redirect was followed")
			}
			if !tc.wantErr && (calls != 2 || !bytes.Equal(data, png)) {
				t.Fatal("public reference download changed")
			}
		})
	}
}

func TestImageEditHTTPDownloadRejectsErrorsAndClosesBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		length int64
		data   string
	}{
		{"HTTP failure", 404, 3, "bad"},
		{"declared oversize", 200, 100, "bad"},
		{"streamed oversize", 200, -1, "too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &imageEditTestBody{Reader: strings.NewReader(tc.data)}
			client := newImageEditReferenceClient(imageEditTestTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: body, Header: http.Header{}, ContentLength: tc.length, Request: req}, nil
			}))
			_, err := fetchImageEditReference(context.Background(), client, "https://images.example.com/a?signature=secret", 3)
			if err == nil || !body.closed || strings.Contains(err.Error(), "signature") {
				t.Fatalf("error=%v bodyClosed=%v", err, body.closed)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchImageEditReference(ctx, nil, "https://example.com/a", 32); err == nil {
		t.Fatal("canceled download accepted")
	}
}

func TestImageEditDNSChecksAndPinsResolvedAddress(t *testing.T) {
	for _, ips := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
	} {
		_, err := resolveImageEditAddress(context.Background(), "images.example.com:443", func(context.Context, string, string) ([]netip.Addr, error) { return ips, nil })
		if err == nil {
			t.Fatal("unsafe DNS answer accepted")
		}
	}
	calls := 0
	address, err := resolveImageEditAddress(context.Background(), "images.example.com:443", func(context.Context, string, string) ([]netip.Addr, error) {
		calls++
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	})
	if err != nil || address != "8.8.8.8:443" || calls != 1 {
		t.Fatalf("address not pinned: %s error=%v lookups=%d", address, err, calls)
	}
}

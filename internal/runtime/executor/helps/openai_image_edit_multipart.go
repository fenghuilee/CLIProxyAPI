package helps

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
)

const imageEditMaxBytes = 32 << 20
const imageEditMaxReferences = 16

// PrepareGPTImage26EditMultipart converts only the confirmed GPT Image 2.6
// protocol. Other models and existing multipart requests remain unchanged.
func PrepareGPTImage26EditMultipart(ctx context.Context, payload []byte, model, contentType string) ([]byte, string, error) {
	switch path.Base(model) {
	case "gpt-image-2.6", "gpt-image-2.6-sunburst", "gpt-image-2.6-flare":
	default:
		return payload, contentType, nil
	}
	if !ojson.ValidBytes(payload) {
		return payload, contentType, nil
	}
	transport := &http.Transport{DialContext: dialImageEditReference}
	defer transport.CloseIdleConnections()
	client := newImageEditReferenceClient(transport)
	return prepareImageEditMultipart(ctx, payload, func(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
		return fetchImageEditReference(ctx, client, rawURL, limit)
	})
}

func fetchImageEditReference(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("reference image download canceled")
	}
	if strings.HasPrefix(rawURL, "data:") {
		header, encoded, ok := strings.Cut(rawURL, ",")
		if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") || base64.StdEncoding.DecodedLen(len(encoded)) > int(limit)+2 {
			return nil, fmt.Errorf("invalid or oversized reference image data URI")
		}
		data, errDecode := base64.StdEncoding.DecodeString(encoded)
		if errDecode != nil {
			return nil, fmt.Errorf("invalid reference image base64")
		}
		return data, nil
	}
	parsed, errParse := url.Parse(rawURL)
	if errParse != nil || validateImageEditURL(parsed) != nil {
		return nil, fmt.Errorf("invalid reference image URL")
	}
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if errRequest != nil {
		return nil, fmt.Errorf("invalid reference image request")
	}
	// A dedicated client never forwards upstream credentials, cookies or proxy auth.
	resp, errDownload := client.Do(req)
	if errDownload != nil {
		return nil, fmt.Errorf("reference image download failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit {
		return nil, fmt.Errorf("reference image download rejected (status %d or size limit)", resp.StatusCode)
	}
	data, errRead := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if errRead != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("reference image unreadable or exceeds size limit")
	}
	return data, nil
}

func newImageEditReferenceClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Referer")
			if len(via) >= 3 {
				return fmt.Errorf("too many reference image redirects")
			}
			return validateImageEditURL(req.URL)
		},
	}
}

func validateImageEditURL(u *url.URL) error {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("unsupported reference image URL")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicImageEditIP(ip) {
		return fmt.Errorf("reference image address is not public")
	}
	return nil
}

func publicImageEditIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

func dialImageEditReference(ctx context.Context, network, address string) (net.Conn, error) {
	checkedAddress, errAddress := resolveImageEditAddress(ctx, address, net.DefaultResolver.LookupNetIP)
	if errAddress != nil {
		return nil, errAddress
	}
	// Pin the connection to an already checked IP, preventing DNS rebinding.
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, checkedAddress)
}

func resolveImageEditAddress(ctx context.Context, address string, lookup func(context.Context, string, string) ([]netip.Addr, error)) (string, error) {
	host, port, errSplit := net.SplitHostPort(address)
	if errSplit != nil {
		return "", fmt.Errorf("invalid reference image address")
	}
	ips, errResolve := lookup(ctx, "ip", host)
	if errResolve != nil || len(ips) == 0 {
		return "", fmt.Errorf("reference image host resolution failed")
	}
	for _, ip := range ips {
		if !publicImageEditIP(ip) {
			return "", fmt.Errorf("reference image host resolves to a non-public address")
		}
	}
	return net.JoinHostPort(ips[0].String(), port), nil
}

func prepareImageEditMultipart(ctx context.Context, payload []byte, fetch func(context.Context, string, int64) ([]byte, error)) ([]byte, string, error) {
	fields := ojson.ParseBytes(payload).Map()
	var images []string
	for _, key := range []string{"images", "image"} {
		value := fields[key]
		values := []ojson.Result{value}
		if value.IsArray() {
			values = value.Array()
		}
		for _, item := range values {
			if !item.Exists() {
				continue
			}
			if item.IsObject() {
				item = item.Get("image_url")
			}
			if item.Type != ojson.String || strings.TrimSpace(item.String()) == "" {
				return nil, "", fmt.Errorf("invalid reference image")
			}
			images = append(images, item.String())
		}
	}
	if len(images) == 0 || len(images) > imageEditMaxReferences {
		return nil, "", fmt.Errorf("image edits require 1 to %d reference images", imageEditMaxReferences)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "images" || key == "image" || key == "mask" {
			continue
		}
		value := fields[key]
		text := value.Raw
		if value.Type == ojson.String {
			text = value.String()
		}
		if errWrite := writer.WriteField(key, text); errWrite != nil {
			return nil, "", errWrite
		}
	}
	remaining := int64(imageEditMaxBytes)
	writeImage := func(field, rawURL string, index int) error {
		data, errFetch := fetch(ctx, rawURL, remaining)
		if errFetch != nil {
			return errFetch
		}
		if len(data) == 0 || int64(len(data)) > remaining {
			return fmt.Errorf("reference images empty or exceed total size limit")
		}
		remaining -= int64(len(data))
		mimeType := http.DetectContentType(data)
		ext := ""
		switch mimeType {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpeg"
		case "image/webp":
			ext = ".webp"
		default:
			return fmt.Errorf("reference image must be PNG, JPEG or WebP")
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", multipart.FileContentDisposition(field, fmt.Sprintf("reference_%d%s", index, ext)))
		header.Set("Content-Type", mimeType)
		part, errPart := writer.CreatePart(header)
		if errPart != nil {
			return errPart
		}
		_, errWrite := part.Write(data)
		return errWrite
	}
	for i, imageURL := range images {
		if errWrite := writeImage("image[]", imageURL, i); errWrite != nil {
			return nil, "", fmt.Errorf("reference image %d: %w", i+1, errWrite)
		}
	}
	if mask := fields["mask"]; mask.Exists() {
		if mask.IsObject() {
			mask = mask.Get("image_url")
		}
		if mask.Type != ojson.String {
			return nil, "", fmt.Errorf("invalid mask image")
		}
		if errWrite := writeImage("mask", mask.String(), len(images)); errWrite != nil {
			return nil, "", errWrite
		}
	}
	if errClose := writer.Close(); errClose != nil {
		return nil, "", errClose
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

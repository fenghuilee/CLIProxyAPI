package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/MoeForge/moego/base64"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// dataURIRegex matches RFC 2397 Base64 Data URIs.
var dataURIRegex = regexp.MustCompile(`^data:([a-zA-Z0-9]+/[a-zA-Z0-9\-\+\.]+);base64,(.+)$`)

// ProcessImageArtifact converts upstream image data (URL or Base64/Data URI) to ContentGenerationArtifact.
func ProcessImageArtifact(artifactType, defaultProvider, rawURL, rawB64 string, meta map[string]any) aigc.ContentGenerationArtifact {
	if artifactType == "" {
		artifactType = "output_image"
	}

	trimmedURL := strings.TrimSpace(rawURL)
	if strings.HasPrefix(trimmedURL, "http://") || strings.HasPrefix(trimmedURL, "https://") {
		mime := "image/png"
		if parsed, errParse := url.Parse(trimmedURL); errParse == nil {
			switch strings.ToLower(path.Ext(parsed.Path)) {
			case ".jpg", ".jpeg":
				mime = "image/jpeg"
			case ".webp":
				mime = "image/webp"
			case ".gif":
				mime = "image/gif"
			case ".svg":
				mime = "image/svg+xml"
			}
		}
		return CreateArtifact(artifactType, defaultProvider, trimmedURL, mime, 0, meta)
	}

	b64Input := strings.TrimSpace(rawB64)
	if b64Input == "" && strings.HasPrefix(trimmedURL, "data:") {
		b64Input = trimmedURL
	}

	if b64Input != "" {
		rawB64Only := b64Input
		mimeType := "image/png"
		if matches := dataURIRegex.FindStringSubmatch(b64Input); len(matches) == 3 {
			mimeType = matches[1]
			rawB64Only = matches[2]
		}

		data, errDecode := base64.StdEncoding.DecodeString(rawB64Only)
		if errDecode != nil {
			data, _ = base64.RawStdEncoding.DecodeString(rawB64Only)
		}

		if len(data) > 0 {
			if detected := http.DetectContentType(data); detected != "application/octet-stream" && strings.HasPrefix(detected, "image/") {
				mimeType = detected
			} else {
				// XML sniffing identifies SVG, but does not sanitize its contents.
				// Storage/rendering boundaries must still enforce their SVG policy.
				var root struct{ XMLName xml.Name }
				if errXML := xml.Unmarshal(data, &root); errXML == nil && root.XMLName.Local == "svg" && (root.XMLName.Space == "" || root.XMLName.Space == "http://www.w3.org/2000/svg") {
					mimeType = "image/svg+xml"
				}
			}
		}

		hash := sha256.Sum256(data)
		sha256Hex := hex.EncodeToString(hash[:])

		safeURI := fmt.Sprintf("inline://sha256=%s", sha256Hex)
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["b64_json"] = b64Input
		meta["sha256"] = sha256Hex

		art := CreateArtifact(artifactType, "inline", safeURI, mimeType, int64(len(data)), meta)
		art.SHA256 = sha256Hex
		return art
	}

	return CreateArtifact(artifactType, defaultProvider, trimmedURL, "image/png", 0, meta)
}

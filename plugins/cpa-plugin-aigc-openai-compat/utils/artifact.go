package utils

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

var dataURIRegex = regexp.MustCompile(`^data:([a-zA-Z0-9]+/[a-zA-Z0-9\-\+\.]+);base64,(.+)$`)

// ProcessImageArtifact processes an image item from upstream:
// - If rawURL is an HTTP/HTTPS URL, directly passes through without alteration.
// - If rawB64 is provided (or rawURL is a Data URI), creates an inline artifact with sha256 and size info.
func ProcessImageArtifact(artifactType, defaultProvider, rawURL, rawB64 string, meta map[string]any) aigc.ContentGenerationArtifact {
	if artifactType == "" {
		artifactType = "output_image"
	}

	trimmedURL := strings.TrimSpace(rawURL)
	if strings.HasPrefix(trimmedURL, "http://") || strings.HasPrefix(trimmedURL, "https://") {
		mime := "image/png"
		if strings.Contains(trimmedURL, ".jpg") || strings.Contains(trimmedURL, ".jpeg") {
			mime = "image/jpeg"
		} else if strings.Contains(trimmedURL, ".webp") {
			mime = "image/webp"
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

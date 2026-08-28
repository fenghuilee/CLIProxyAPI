package utils

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// ParseDimensions extracts (width, height) from an OpenAI standard size string (e.g. "854x480", "1920x1080").
func ParseDimensions(size string) (width, height int, ok bool) {
	size = strings.TrimSpace(size)
	if size == "" {
		return 0, 0, false
	}

	parts := strings.FieldsFunc(size, func(r rune) bool {
		return r == 'x' || r == 'X' || r == '*'
	})
	if len(parts) == 2 {
		w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errW == nil && errH == nil && w > 0 && h > 0 {
			return w, h, true
		}
	}
	return 0, 0, false
}

// NormalizeSizeToStar formats a size as "WIDTH*HEIGHT" (e.g. "1024*1024").
func NormalizeSizeToStar(body []byte) string {
	rawSize := strings.TrimSpace(gjson.GetBytes(body, "size").String())
	if rawSize == "" {
		width := gjson.GetBytes(body, "width").Int()
		height := gjson.GetBytes(body, "height").Int()
		if width > 0 && height > 0 {
			return fmt.Sprintf("%d*%d", width, height)
		}
		return ""
	}

	if w, h, ok := ParseDimensions(rawSize); ok {
		return fmt.Sprintf("%d*%d", w, h)
	}
	return rawSize
}

// NormalizeSizeToX formats a size as "WIDTHxHEIGHT" (e.g. "1024x1024").
func NormalizeSizeToX(body []byte) string {
	rawSize := strings.TrimSpace(gjson.GetBytes(body, "size").String())
	if rawSize == "" {
		width := gjson.GetBytes(body, "width").Int()
		height := gjson.GetBytes(body, "height").Int()
		if width > 0 && height > 0 {
			return fmt.Sprintf("%dx%d", width, height)
		}
		return ""
	}

	if w, h, ok := ParseDimensions(rawSize); ok {
		return fmt.Sprintf("%dx%d", w, h)
	}
	return rawSize
}

// RatioFromSize extracts ratio ("16:9", "9:16", "4:3", "3:4", "1:1", "21:9") from width/height or size string.
func RatioFromSize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	if strings.Contains(size, ":") {
		return size
	}

	w, h, ok := ParseDimensions(size)
	if !ok || w <= 0 || h <= 0 {
		return ""
	}

	ratio := float64(w) / float64(h)
	switch {
	case ratio >= 2.0:
		return "21:9"
	case ratio >= 1.6:
		return "16:9"
	case ratio <= 0.6:
		return "9:16"
	case ratio >= 1.2:
		return "4:3"
	case ratio <= 0.8:
		return "3:4"
	default:
		return "1:1"
	}
}

package driver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

var allowedImageTopLevelKeys = map[string]bool{
	"prompt":          true,
	"model":           true,
	"n":               true,
	"quality":         true,
	"response_format": true,
	"size":            true,
	"style":           true,
	"user":            true,
	"image":           true,
	"images":          true,
	"image_url":       true,
	"input_reference": true,
	"mask":            true,
	"extra_body":      true,
}

var allowedVideoTopLevelKeys = map[string]bool{
	"prompt":          true,
	"model":           true,
	"seconds":         true,
	"size":            true,
	"input_reference": true,
	"user":            true,
	"extra_body":      true,
}

// ValidateOpenAIInput validates that the request contains only allowed top-level keys.
func ValidateOpenAIInput(kind aigc.ContentKind, inputBytes []byte) error {
	if len(inputBytes) == 0 {
		return nil
	}

	var root map[string]any
	if err := json.Unmarshal(inputBytes, &root); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}

	var allowed map[string]bool
	switch kind {
	case aigc.ContentKindImage:
		allowed = allowedImageTopLevelKeys
	case aigc.ContentKindVideo:
		allowed = allowedVideoTopLevelKeys
	default:
		return nil
	}

	var unrecognized []string
	for k := range root {
		if !allowed[k] {
			unrecognized = append(unrecognized, k)
		}
	}

	if len(unrecognized) > 0 {
		sort.Strings(unrecognized)
		return fmt.Errorf("unrecognized parameter %s", strings.Join(unrecognized, ", "))
	}

	return nil
}

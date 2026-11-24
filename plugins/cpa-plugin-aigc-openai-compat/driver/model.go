package driver

import "strings"

// UpstreamModel strips the first vendor prefix segment from a model name.
func UpstreamModel(model string) string {
	model = strings.TrimSpace(model)
	_, rest, ok := strings.Cut(model, "/")
	if !ok {
		return model
	}
	return strings.TrimSpace(rest)
}

package utils

import (
	"path/filepath"
	"strings"
)

// MatchModel checks if a model name matches a pattern (case-insensitive glob/prefix).
func MatchModel(pattern, model string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	m := strings.ToLower(strings.TrimSpace(model))
	if p == "" || m == "" {
		return false
	}
	if p == "*" || p == m {
		return true
	}
	if strings.Contains(p, "*") || strings.Contains(p, "?") {
		if matched, _ := filepath.Match(p, m); matched {
			return true
		}
	}
	return strings.EqualFold(p, m)
}

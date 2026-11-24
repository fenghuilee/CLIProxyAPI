package aigc

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// DynamicErrorRule represents a user-configurable error mapping rule loaded from database.
type DynamicErrorRule struct {
	ID            uint64 `json:"id"`
	Provider      string `json:"provider"`
	MatchType     string `json:"match_type"`
	MatchPattern  string `json:"match_pattern"`
	StandardCode  string `json:"standard_code"`
	StandardType  string `json:"standard_type"`
	UserMessage   string `json:"user_message"`
	UserMessageEN string `json:"user_message_en,omitempty"`
	HTTPStatus    int    `json:"http_status"`
	Priority      int    `json:"priority"`
	Status        int8   `json:"status"`
	Description   string `json:"description,omitempty"`
}

type compiledRule struct {
	rule             DynamicErrorRule
	compiledRegex    *regexp.Regexp
	patternLower     string
	providerWildcard bool
	providerLower    string
}

// RuleRegistry manages the in-memory compiled dynamic error mapping rules.
type RuleRegistry struct {
	rules    atomic.Pointer[[]compiledRule]
	mu       sync.RWMutex
	loadFunc func(ctx context.Context) ([]DynamicErrorRule, error)
}

var globalRuleRegistry = &RuleRegistry{}

func init() {
	empty := make([]compiledRule, 0)
	globalRuleRegistry.rules.Store(&empty)
}

// GetGlobalRuleRegistry returns the singleton rule registry.
func GetGlobalRuleRegistry() *RuleRegistry {
	return globalRuleRegistry
}

// RegisterDynamicRulesLoader registers a callback to fetch rules from the database.
func RegisterDynamicRulesLoader(loader func(ctx context.Context) ([]DynamicErrorRule, error)) {
	globalRuleRegistry.mu.Lock()
	defer globalRuleRegistry.mu.Unlock()
	globalRuleRegistry.loadFunc = loader
}

// ReloadDynamicRules queries the registered database loader and updates the in-memory rules.
func ReloadDynamicRules(ctx context.Context) error {
	globalRuleRegistry.mu.RLock()
	loader := globalRuleRegistry.loadFunc
	globalRuleRegistry.mu.RUnlock()

	if loader == nil {
		return nil
	}
	rules, err := loader(ctx)
	if err != nil {
		return err
	}
	SetDynamicRules(rules)
	return nil
}

// SetDynamicRules compiles, sorts, and atomically replaces the in-memory rule set.
func SetDynamicRules(rules []DynamicErrorRule) {
	globalRuleRegistry.SetRules(rules)
}

// SetRules compiles and atomically stores the rule list.
func (r *RuleRegistry) SetRules(rules []DynamicErrorRule) {
	// Sort by Priority ASC, then ID ASC
	sorted := make([]DynamicErrorRule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})

	compiled := make([]compiledRule, 0, len(sorted))
	for _, rule := range sorted {
		if rule.Status == 0 {
			continue
		}
		c := compiledRule{
			rule:         rule,
			patternLower: strings.ToLower(strings.TrimSpace(rule.MatchPattern)),
		}

		prov := strings.ToLower(strings.TrimSpace(rule.Provider))
		if prov == "" || prov == "*" {
			c.providerWildcard = true
		} else {
			c.providerLower = prov
		}

		if rule.MatchType == "msg_regex" || rule.MatchType == "code_regex" {
			if re, err := regexp.Compile(rule.MatchPattern); err == nil {
				c.compiledRegex = re
			}
		}
		compiled = append(compiled, c)
	}

	r.rules.Store(&compiled)
}

// Match evaluates all active compiled dynamic rules against the given error context.
func (r *RuleRegistry) Match(provider string, statusCode int, upstreamCode, upstreamMsg string) (NormalizedError, bool) {
	ptr := r.rules.Load()
	if ptr == nil {
		return NormalizedError{}, false
	}
	rules := *ptr
	if len(rules) == 0 {
		return NormalizedError{}, false
	}

	provLower := strings.ToLower(strings.TrimSpace(provider))
	codeLower := strings.ToLower(strings.TrimSpace(upstreamCode))
	msgLower := strings.ToLower(strings.TrimSpace(upstreamMsg))

	for _, cr := range rules {
		// 1. Provider matching
		if !cr.providerWildcard {
			if strings.Contains(cr.providerLower, "*") {
				if matched, _ := filepath.Match(cr.providerLower, provLower); !matched {
					continue
				}
			} else if cr.providerLower != provLower && !strings.Contains(provLower, cr.providerLower) {
				continue
			}
		}

		// 2. Condition matching
		matched := false
		switch cr.rule.MatchType {
		case "code_exact":
			matched = codeLower != "" && codeLower == cr.patternLower
		case "code_prefix":
			matched = codeLower != "" && strings.HasPrefix(codeLower, cr.patternLower)
		case "code_contains":
			matched = codeLower != "" && strings.Contains(codeLower, cr.patternLower)
		case "msg_contains":
			matched = msgLower != "" && strings.Contains(msgLower, cr.patternLower)
		case "msg_regex":
			if cr.compiledRegex != nil {
				matched = cr.compiledRegex.MatchString(upstreamMsg)
			}
		case "code_regex":
			if cr.compiledRegex != nil {
				matched = cr.compiledRegex.MatchString(upstreamCode)
			}
		}

		if matched {
			httpStatus := cr.rule.HTTPStatus
			if httpStatus <= 0 {
				httpStatus = statusCode
			}
			if httpStatus <= 0 {
				httpStatus = 400
			}

			stdType := cr.rule.StandardType
			if stdType == "" {
				stdType = "invalid_request_error"
			}

			userMsg := cr.rule.UserMessage
			if userMsg == "" {
				userMsg = upstreamMsg
			}

			return NormalizedError{
				Code:         cr.rule.StandardCode,
				Message:      userMsg,
				Type:         stdType,
				HTTPStatus:   httpStatus,
				UpstreamCode: upstreamCode,
			}, true
		}
	}

	return NormalizedError{}, false
}

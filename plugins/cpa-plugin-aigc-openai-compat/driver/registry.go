package driver

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// Factory describes how to construct a registered adapter.
type Factory struct {
	Name     string
	New      func() Adapter
	Fallback bool
}

var (
	catalogMu        sync.RWMutex
	catalogFactories []Factory
	catalogFallback  string
)

// RegisterFactory adds an adapter factory to the default catalog.
func RegisterFactory(factory Factory) error {
	name := strings.TrimSpace(factory.Name)
	if name == "" {
		return fmt.Errorf("adapter factory name cannot be empty")
	}
	if factory.New == nil {
		return fmt.Errorf("adapter factory %q constructor cannot be nil", name)
	}
	factory.Name = name

	catalogMu.Lock()
	defer catalogMu.Unlock()

	for _, existing := range catalogFactories {
		if existing.Name == name {
			return fmt.Errorf("adapter factory %q already registered", name)
		}
	}
	if factory.Fallback && catalogFallback != "" {
		return fmt.Errorf("fallback adapter factory already registered (%s)", catalogFallback)
	}
	catalogFactories = append(catalogFactories, factory)
	if factory.Fallback {
		catalogFallback = name
	}
	return nil
}

// MustRegisterFactory registers a factory and panics if registration fails.
func MustRegisterFactory(factory Factory) {
	if err := RegisterFactory(factory); err != nil {
		panic(err)
	}
}

// CatalogSnapshot returns a sorted copy of all registered factories.
func CatalogSnapshot() []Factory {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	result := append([]Factory(nil), catalogFactories...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// ResetCatalog clears the registered factories (primarily for testing).
func ResetCatalog() {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	catalogFactories = nil
	catalogFallback = ""
}

type trieNode struct {
	children map[rune]*trieNode
	adapter  Adapter
}

func newTrieNode() *trieNode {
	return &trieNode{
		children: make(map[rune]*trieNode),
	}
}

type prefixTrie struct {
	root *trieNode
}

func newPrefixTrie() *prefixTrie {
	return &prefixTrie{
		root: newTrieNode(),
	}
}

// ValidatePrefix validates that prefix contains non-empty vendor/model segments without wildcards.
func ValidatePrefix(prefix string) (string, error) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return "", fmt.Errorf("model prefix cannot be empty")
	}
	if strings.ContainsAny(trimmed, "*?") {
		return "", fmt.Errorf("wildcards not allowed in model prefix %q", prefix)
	}
	slashIdx := strings.Index(trimmed, "/")
	if slashIdx <= 0 || slashIdx == len(trimmed)-1 {
		return "", fmt.Errorf("prefix %q must be a full vendor/model prefix with vendor/ separator", prefix)
	}
	return strings.ToLower(trimmed), nil
}

func (t *prefixTrie) checkCollision(normalized string) error {
	curr := t.root
	for _, ch := range normalized {
		child, exists := curr.children[ch]
		if !exists {
			return nil
		}
		curr = child
	}
	if curr.adapter != nil {
		return fmt.Errorf("prefix collision: %q already registered to %s", normalized, curr.adapter.Name())
	}
	return nil
}

func (t *prefixTrie) insert(normalized string, adapter Adapter) {
	curr := t.root
	for _, ch := range normalized {
		child, exists := curr.children[ch]
		if !exists {
			child = newTrieNode()
			curr.children[ch] = child
		}
		curr = child
	}
	curr.adapter = adapter
}

// Lookup returns the adapter matching the longest registered prefix.
func (t *prefixTrie) Lookup(model string) (Adapter, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return nil, false
	}
	curr := t.root
	var longest Adapter
	found := false
	if curr.adapter != nil {
		longest = curr.adapter
		found = true
	}
	for _, ch := range normalized {
		child, exists := curr.children[ch]
		if !exists {
			break
		}
		curr = child
		if curr.adapter != nil {
			longest = curr.adapter
			found = true
		}
	}
	return longest, found
}

// Registry manages per-kind tries, named adapters, and fallback adapter.
type Registry struct {
	tries          map[aigc.ContentKind]*prefixTrie
	adaptersByName map[string]Adapter
	fallback       FallbackAdapter
}

// NewRegistry creates a new model routing registry.
func NewRegistry() *Registry {
	return &Registry{
		tries: map[aigc.ContentKind]*prefixTrie{
			aigc.ContentKindImage: newPrefixTrie(),
			aigc.ContentKindVideo: newPrefixTrie(),
		},
		adaptersByName: make(map[string]Adapter),
	}
}

// Register validates adapter name and declared prefixes before storing them.
func (r *Registry) Register(adapter Adapter) error {
	if adapter == nil {
		return fmt.Errorf("adapter cannot be nil")
	}
	name := strings.TrimSpace(adapter.Name())
	if name == "" {
		return fmt.Errorf("adapter name cannot be empty")
	}
	if _, exists := r.adaptersByName[name]; exists {
		return fmt.Errorf("adapter %q already registered", name)
	}

	provider, ok := adapter.(PrefixProvider)
	if !ok {
		r.adaptersByName[name] = adapter
		return nil
	}

	kind := provider.Kind()
	trie, exists := r.tries[kind]
	if !exists {
		trie = newPrefixTrie()
		r.tries[kind] = trie
	}

	rawPrefixes := provider.Prefixes()
	validated := make([]string, 0, len(rawPrefixes))
	seenInAdapter := make(map[string]bool, len(rawPrefixes))

	for _, raw := range rawPrefixes {
		normalized, err := ValidatePrefix(raw)
		if err != nil {
			return err
		}
		if seenInAdapter[normalized] {
			return fmt.Errorf("duplicate prefix %q declared by adapter %s", raw, name)
		}
		seenInAdapter[normalized] = true

		if err := trie.checkCollision(normalized); err != nil {
			return err
		}
		validated = append(validated, normalized)
	}

	r.adaptersByName[name] = adapter
	for _, prefix := range validated {
		trie.insert(prefix, adapter)
	}
	return nil
}

// SetFallback sets the unique fallback adapter.
func (r *Registry) SetFallback(fallback FallbackAdapter) error {
	if fallback == nil {
		return fmt.Errorf("fallback adapter cannot be nil")
	}
	name := strings.TrimSpace(fallback.Name())
	if name == "" {
		return fmt.Errorf("fallback adapter name cannot be empty")
	}
	if r.fallback != nil {
		return fmt.Errorf("fallback adapter already configured (%s)", r.fallback.Name())
	}
	if _, exists := r.adaptersByName[name]; exists {
		return fmt.Errorf("adapter %q already registered", name)
	}
	r.fallback = fallback
	r.adaptersByName[name] = fallback
	return nil
}

// Lookup resolves the adapter by model prefix within the given kind.
func (r *Registry) Lookup(kind aigc.ContentKind, model string) (Adapter, bool) {
	trie, exists := r.tries[kind]
	if !exists {
		return nil, false
	}
	return trie.Lookup(model)
}

// GetAdapterByName returns an adapter by registered name.
func (r *Registry) GetAdapterByName(name string) Adapter {
	return r.adaptersByName[name]
}

// Fallback returns the configured fallback adapter.
func (r *Registry) Fallback() FallbackAdapter {
	return r.fallback
}

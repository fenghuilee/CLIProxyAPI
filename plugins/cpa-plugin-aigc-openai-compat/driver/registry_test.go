package driver_test

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

type mockPrefixAdapter struct {
	mockAdapter
	kind     aigc.ContentKind
	prefixes []string
}

func (m *mockPrefixAdapter) Kind() aigc.ContentKind { return m.kind }
func (m *mockPrefixAdapter) Prefixes() []string     { return m.prefixes }

func TestRegistry_LongestPrefixMatching(t *testing.T) {
	reg := driver.NewRegistry()
	adBase := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "base-adapter"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/model-v1"},
	}
	adPro := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "pro-adapter"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/model-v1-pro"},
	}

	if err := reg.Register(adBase); err != nil {
		t.Fatalf("failed to register base adapter: %v", err)
	}
	if err := reg.Register(adPro); err != nil {
		t.Fatalf("failed to register pro adapter: %v", err)
	}

	matched, found := reg.Lookup(aigc.ContentKindVideo, "vendor/model-v1-pro-max")
	if !found || matched.Name() != "pro-adapter" {
		t.Fatalf("expected pro-adapter for longest match, got %v (found: %v)", matched, found)
	}

	matchedBase, foundBase := reg.Lookup(aigc.ContentKindVideo, "vendor/model-v1-standard")
	if !foundBase || matchedBase.Name() != "base-adapter" {
		t.Fatalf("expected base-adapter, got %v (found: %v)", matchedBase, foundBase)
	}

	_, foundNone := reg.Lookup(aigc.ContentKindVideo, "vendor/model-v2")
	if foundNone {
		t.Fatalf("expected no match for vendor/model-v2")
	}
}

func TestRegistry_EqualFullStringMatching(t *testing.T) {
	reg := driver.NewRegistry()
	ad := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "wan-adapter"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"cdp/wan3.0-video"},
	}

	if err := reg.Register(ad); err != nil {
		t.Fatalf("failed to register adapter: %v", err)
	}

	matched, found := reg.Lookup(aigc.ContentKindVideo, "cdp/wan3.0-video")
	if !found || matched.Name() != "wan-adapter" {
		t.Fatalf("expected wan-adapter, got %v (found: %v)", matched, found)
	}
}

func TestRegistry_CollisionErrors(t *testing.T) {
	reg := driver.NewRegistry()
	ad1 := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "adapter-1"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/test-model"},
	}
	ad2 := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "adapter-2"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/test-model"},
	}

	if err := reg.Register(ad1); err != nil {
		t.Fatalf("initial register failed: %v", err)
	}

	errDup := reg.Register(ad2)
	if errDup == nil || !strings.Contains(errDup.Error(), "collision") {
		t.Fatalf("expected collision error, got %v", errDup)
	}

	adCase := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "adapter-case"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"VENDOR/TEST-MODEL"},
	}
	errCaseDup := reg.Register(adCase)
	if errCaseDup == nil || !strings.Contains(errCaseDup.Error(), "collision") {
		t.Fatalf("expected case-insensitive collision error, got %v", errCaseDup)
	}

	adSelfDup := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "adapter-self-dup"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/unique-prefix", "vendor/unique-prefix"},
	}
	errSelfDup := reg.Register(adSelfDup)
	if errSelfDup == nil || !strings.Contains(errSelfDup.Error(), "duplicate prefix") {
		t.Fatalf("expected duplicate prefix error within same adapter, got %v", errSelfDup)
	}

	invalidPrefixes := []struct {
		prefix string
		desc   string
	}{
		{"", "empty prefix"},
		{"   ", "whitespace only"},
		{"bare-model", "bare model without vendor slash"},
		{"vendor/*", "wildcard asterisk"},
		{"vendor/model-?", "wildcard question mark"},
		{"/model", "missing vendor"},
		{"vendor/", "missing model"},
	}

	for _, tc := range invalidPrefixes {
		badAdapter := &mockPrefixAdapter{
			mockAdapter: mockAdapter{name: "bad-" + tc.desc},
			kind:        aigc.ContentKindVideo,
			prefixes:    []string{tc.prefix},
		}
		err := reg.Register(badAdapter)
		if err == nil {
			t.Fatalf("expected validation error for %s (%q), got nil", tc.desc, tc.prefix)
		}
	}
}

func TestRegistry_DuplicateAdapterNamesError(t *testing.T) {
	reg := driver.NewRegistry()
	ad1 := &mockAdapter{name: "shared-adapter-name"}
	ad2 := &mockAdapter{name: "shared-adapter-name"}

	if err := reg.Register(ad1); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	errDup := reg.Register(ad2)
	if errDup == nil || !strings.Contains(errDup.Error(), "already registered") {
		t.Fatalf("expected duplicate adapter name error, got %v", errDup)
	}
}

func TestRegistry_FailedRegistrationNoPartialInsertion(t *testing.T) {
	reg := driver.NewRegistry()
	failingAdapter := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "failing-adapter"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/valid-first", "invalid-bare-prefix", "vendor/valid-second"},
	}

	err := reg.Register(failingAdapter)
	if err == nil {
		t.Fatalf("expected registration error due to invalid prefix, got nil")
	}

	if reg.GetAdapterByName("failing-adapter") != nil {
		t.Fatalf("adapter name should not be recorded on failed registration")
	}

	if _, found := reg.Lookup(aigc.ContentKindVideo, "vendor/valid-first"); found {
		t.Fatalf("vendor/valid-first should not be present in trie after failed registration")
	}
	if _, found := reg.Lookup(aigc.ContentKindVideo, "vendor/valid-second"); found {
		t.Fatalf("vendor/valid-second should not be present in trie after failed registration")
	}
}

func TestRegistry_SetFallbackUniqueness(t *testing.T) {
	reg := driver.NewRegistry()
	fb1 := &mockFallbackAdapter{mockAdapter: mockAdapter{name: "fallback-primary"}}
	fb2 := &mockFallbackAdapter{mockAdapter: mockAdapter{name: "fallback-secondary"}}

	if err := reg.SetFallback(fb1); err != nil {
		t.Fatalf("initial SetFallback failed: %v", err)
	}
	if reg.Fallback() != fb1 {
		t.Fatalf("expected fb1 as fallback")
	}

	if err := reg.SetFallback(fb2); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("expected already configured error on second SetFallback, got %v", err)
	}

	regFresh := driver.NewRegistry()
	if err := regFresh.SetFallback(nil); err == nil {
		t.Fatalf("expected error on nil fallback")
	}

	regCollision := driver.NewRegistry()
	ad := &mockAdapter{name: "existing-name"}
	if err := regCollision.Register(ad); err != nil {
		t.Fatalf("register ad failed: %v", err)
	}
	fbCollide := &mockFallbackAdapter{mockAdapter: mockAdapter{name: "existing-name"}}
	if err := regCollision.SetFallback(fbCollide); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected already registered error on name collision, got %v", err)
	}
}

func TestRegistry_Normalization(t *testing.T) {
	reg := driver.NewRegistry()
	ad := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "minimax-adapter"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"cdp/MiniMax-H3"},
	}

	if err := reg.Register(ad); err != nil {
		t.Fatalf("failed to register adapter: %v", err)
	}

	for _, lookup := range []string{
		"cdp/minimax-h3",
		"CDP/MINIMAX-H3",
		"  cdp/MiniMax-H3  ",
		"cdp/minimax-h3-extra-suffix",
	} {
		matched, found := reg.Lookup(aigc.ContentKindVideo, lookup)
		if !found || matched.Name() != "minimax-adapter" {
			t.Fatalf("lookup %q failed: matched=%v, found=%v", lookup, matched, found)
		}
	}
}

func TestRegistry_KindIsolation(t *testing.T) {
	reg := driver.NewRegistry()
	adImg := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "img-adapter"},
		kind:        aigc.ContentKindImage,
		prefixes:    []string{"vendor/shared-name"},
	}
	adVid := &mockPrefixAdapter{
		mockAdapter: mockAdapter{name: "vid-adapter"},
		kind:        aigc.ContentKindVideo,
		prefixes:    []string{"vendor/shared-name"},
	}

	if err := reg.Register(adImg); err != nil {
		t.Fatalf("failed to register img: %v", err)
	}
	if err := reg.Register(adVid); err != nil {
		t.Fatalf("failed to register vid: %v", err)
	}

	matchedImg, foundImg := reg.Lookup(aigc.ContentKindImage, "vendor/shared-name")
	if !foundImg || matchedImg.Name() != "img-adapter" {
		t.Fatalf("expected img-adapter, got %v", matchedImg)
	}

	matchedVid, foundVid := reg.Lookup(aigc.ContentKindVideo, "vendor/shared-name")
	if !foundVid || matchedVid.Name() != "vid-adapter" {
		t.Fatalf("expected vid-adapter, got %v", matchedVid)
	}
}

func TestRegistry_EmptyOrMissingModel(t *testing.T) {
	reg := driver.NewRegistry()
	if _, found := reg.Lookup(aigc.ContentKindVideo, ""); found {
		t.Fatal("expected false for empty model lookup")
	}
	if _, found := reg.Lookup(aigc.ContentKindVideo, "   "); found {
		t.Fatal("expected false for whitespace model lookup")
	}
	if _, found := reg.Lookup(aigc.ContentKind("unknown"), "vendor/model"); found {
		t.Fatal("expected false for unknown content kind lookup")
	}
}

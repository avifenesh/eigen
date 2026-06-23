package main

import (
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// TestDefaultChatModelIsConsistent pins the APP-016 invariant: the default
// interactive chat (no config, no flags) must resolve to the SAME model the
// --model flag help advertises (mantle / openai.gpt-5.5), on BOTH the daemon
// and direct paths — never the daemon's old converse/Opus fallback.
//
// effectiveModel is what main now hands the daemon's NewSession (instead of an
// empty string), so the daemon can't fall through to its own divergent default.
func TestDefaultChatModelIsConsistent(t *testing.T) {
	// Flag defaults when nothing is configured: --provider mantle, --model "".
	const provider, model = "mantle", ""

	got := effectiveModel(provider, model)
	if got != "openai.gpt-5.5" {
		t.Fatalf("default chat model = %q, want openai.gpt-5.5 (the flag-help/direct default)", got)
	}
	// And it must be a mantle model — not the daemon's old converse/Opus default.
	if got == llm.DefaultModel("converse") {
		t.Fatalf("default chat model collapsed to the converse/Opus default %q", got)
	}
	if info, ok := llm.Lookup(got); !ok || llm.CanonicalProvider(info.Provider) != "mantle" {
		t.Fatalf("default chat model %q is not a mantle model (provider=%q ok=%v)", got, info.Provider, ok)
	}
}

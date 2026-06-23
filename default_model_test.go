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

// TestNewSessionRefCarriesProvider pins the APP-060 invariant: the ref main
// hands the daemon's NewSession must carry the chosen --provider, since the
// protocol has no provider field and the daemon resolves it from
// cfg.Provider/converse. Folding --provider into the ref (llm.Ref over the
// effective model) makes the daemon honor `eigen --provider grok` even when the
// provider isn't encoded in the model id.
func TestNewSessionRefCarriesProvider(t *testing.T) {
	// The exact expression main uses at the dc.NewSession call site.
	ref := func(provider, model string) string {
		return llm.Ref(provider, effectiveModel(provider, model))
	}

	// An unknown model id under a non-default provider must keep the provider
	// tag, or the daemon would build it on its own default backend.
	if got := ref("grok", "some-unlisted-model"); got != "grok:some-unlisted-model" {
		t.Fatalf("ref(grok, unlisted) = %q, want grok:some-unlisted-model (provider must survive)", got)
	}

	// --provider grok with no --model resolves to grok's default model, which the
	// catalog knows and self-tags — the daemon reconciles the provider from the id.
	got := ref("grok", "")
	want := llm.DefaultModel("grok")
	if got != want {
		t.Fatalf("ref(grok, \"\") = %q, want %q (grok's self-tagging default)", got, want)
	}
	if llm.ResolveProvider("converse", got) != "grok" {
		t.Fatalf("daemon would not resolve %q back to grok", got)
	}

	// The default path (mantle / no model) is unchanged: a catalog id self-tags,
	// so no provider prefix appears and the APP-016 default still holds.
	if got := ref("mantle", ""); got != "openai.gpt-5.5" {
		t.Fatalf("ref(mantle, \"\") = %q, want openai.gpt-5.5 (unchanged default)", got)
	}
}

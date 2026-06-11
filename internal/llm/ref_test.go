package llm

import (
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		in, prov, model string
	}{
		// explicit tags (canonical + aliases)
		{"mantle:us.openai.gpt-5.5", "mantle", "us.openai.gpt-5.5"},
		{"ant:claude-opus-4-1-20250805", "ant", "claude-opus-4-1-20250805"},
		{"converse:global.anthropic.claude-fable-5", "converse", "global.anthropic.claude-fable-5"},
		{"xai:grok-build", "xai", "grok-build"},
		// untagged: self-tagging ids pass through whole
		{"us.anthropic.claude-opus-4-8", "", "us.anthropic.claude-opus-4-8"},
		{"claude-fable-5", "", "claude-fable-5"},
		{"glm-5.1", "", "glm-5.1"},
		// a colon INSIDE a model id must not split (the prefix isn't a provider)
		{"us.anthropic.claude-haiku-4-5-20251001-v1:0", "", "us.anthropic.claude-haiku-4-5-20251001-v1:0"},
		// degenerate forms
		{"", "", ""},
		{":model", "", ":model"},
		{"mantle:", "", "mantle:"},
	}
	for _, c := range cases {
		p, m := ParseRef(c.in)
		if p != c.prov || m != c.model {
			t.Errorf("ParseRef(%q) = (%q, %q), want (%q, %q)", c.in, p, m, c.prov, c.model)
		}
	}
}

func TestResolveProviderHonorsRefTag(t *testing.T) {
	// An explicit tag wins even when the catalog disagrees.
	if got := ResolveProvider("converse", "mantle:us.anthropic.claude-opus-4-8"); got != "mantle" {
		t.Fatalf("tag should force the provider, got %q", got)
	}
	// Untagged catalog ids still self-tag.
	if got := ResolveProvider("mantle", "us.anthropic.claude-opus-4-8"); got != "converse" {
		t.Fatalf("catalog should reconcile, got %q", got)
	}
}

func TestLookupIsTagBlind(t *testing.T) {
	tagged, ok1 := Lookup("converse:us.anthropic.claude-opus-4-8")
	bare, ok2 := Lookup("us.anthropic.claude-opus-4-8")
	if !ok1 || !ok2 || tagged.ID != bare.ID {
		t.Fatalf("tagged lookup should match bare: %v %v %v %v", tagged, ok1, bare, ok2)
	}
}

func TestKnownProvider(t *testing.T) {
	for _, yes := range []string{"mantle", "ant", "anthropic", "converse", "claude", "xai", "glm", "local"} {
		if !knownProvider(yes) {
			t.Errorf("knownProvider(%q) should be true", yes)
		}
	}
	for _, no := range []string{"", "us.anthropic.claude-haiku-4-5-20251001-v1", "gpt", "openai"} {
		if knownProvider(no) {
			t.Errorf("knownProvider(%q) should be false", no)
		}
	}
}

func TestRefRendersOneField(t *testing.T) {
	// Catalog ids self-tag: bare, even when the provider field disagrees
	// (the catalog wins at use time — a tag would force a stale backend).
	if got := Ref("mantle", "us.anthropic.claude-opus-4-8"); got != "us.anthropic.claude-opus-4-8" {
		t.Fatalf("catalog id should render bare, got %q", got)
	}
	if got := Ref("converse", "global.anthropic.claude-fable-5"); got != "global.anthropic.claude-fable-5" {
		t.Fatalf("got %q", got)
	}
	// Unknown ids: the provider field is the only signal → tagged.
	if got := Ref("glm", "glm-99-experimental"); got != "glm:glm-99-experimental" {
		t.Fatalf("unknown id should carry the tag, got %q", got)
	}
	if got := Ref("", "anything"); got != "anything" {
		t.Fatalf("no provider → bare, got %q", got)
	}
}

func TestNewAcceptsAliasRef(t *testing.T) {
	// 'ant:' must reach the anthropic backend — the tag splits, then New
	// canonicalizes the alias before the switch (regression: it used to hit
	// the unknown-provider error). Construction may fail later on missing
	// creds, but it must NOT be 'unknown provider'.
	_, err := New("", "ant:claude-fable-5")
	if err != nil && strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("alias ref must resolve to a real backend, got: %v", err)
	}
}

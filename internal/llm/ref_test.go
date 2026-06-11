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

func TestModelEffortLevelsPerCatalog(t *testing.T) {
	cases := []struct {
		model string
		want  []string
	}{
		// mantle GPT (verified live): none|low..xhigh — minimal and max rejected
		{"openai.gpt-5.5", []string{"none", "low", "medium", "high", "xhigh"}},
		// Anthropic adaptive fable/opus (verified live on converse AND native):
		// low..xhigh|max — auto and minimal rejected
		{"global.anthropic.claude-fable-5", []string{"low", "medium", "high", "xhigh", "max"}},
		{"us.anthropic.claude-opus-4-8", []string{"low", "medium", "high", "xhigh", "max"}},
		{"claude-fable-5", []string{"low", "medium", "high", "xhigh", "max"}},
		// budget-style sonnet: off (thinking disabled) through xhigh budgets
		{"us.anthropic.claude-sonnet-4-6", []string{"off", "low", "medium", "high", "xhigh"}},
		{"claude-sonnet-4-5-20250929", []string{"off", "low", "medium", "high", "xhigh"}},
	}
	for _, c := range cases {
		got := ModelEffortLevels(c.model)
		if len(got) != len(c.want) {
			t.Errorf("%s: levels = %v, want %v", c.model, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: levels = %v, want %v", c.model, got, c.want)
				break
			}
		}
	}
	// Non-reasoning models: no effort control at all.
	if got := ModelEffortLevels("us.anthropic.claude-haiku-4-5-20251001-v1:0"); got != nil {
		t.Errorf("haiku should have no effort levels, got %v", got)
	}
	if got := ModelEffortLevels("glm-5.1"); got != nil {
		t.Errorf("glm should have no effort levels, got %v", got)
	}
}

func TestSetEffortRespectsModelCatalog(t *testing.T) {
	// Mantle GPT (verified live): minimal/max/auto rejected, none accepted.
	m := &Mantle{Model: "openai.gpt-5.5"}
	if m.SetEffort("minimal") {
		t.Error("gpt-5.5 must reject minimal")
	}
	if m.SetEffort("max") {
		t.Error("gpt-5.5 must reject max")
	}
	if m.SetEffort("auto") {
		t.Error("gpt-5.5 must reject auto")
	}
	if !m.SetEffort("none") {
		t.Error("gpt-5.5 must accept none")
	}
	if !m.SetEffort("xhigh") {
		t.Error("gpt-5.5 must accept xhigh")
	}
	// Adaptive fable (verified live): max accepted, auto/minimal rejected.
	c := &Converse{Model: "global.anthropic.claude-fable-5"}
	if !c.SetEffort("max") {
		t.Error("fable-5 must accept max")
	}
	if c.Effort() != "max" {
		t.Errorf("fable-5 effort = %q, want max", c.Effort())
	}
	if c.SetEffort("auto") {
		t.Error("fable-5 must reject auto (API rejects it)")
	}
	if c.SetEffort("minimal") {
		t.Error("fable-5 must reject minimal")
	}
	// Budget sonnet: off zeroes the thinking budget; max rejected.
	s := &Converse{Model: "us.anthropic.claude-sonnet-4-6"}
	if !s.SetEffort("off") {
		t.Error("sonnet-4-6 must accept off")
	}
	if s.thinkingBudget != 0 {
		t.Errorf("off should zero the budget, got %d", s.thinkingBudget)
	}
	if s.SetEffort("max") {
		t.Error("sonnet-4-6 must reject max")
	}
}

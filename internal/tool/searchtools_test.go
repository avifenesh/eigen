package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

func nicheDef(name, desc string) Definition {
	return Definition{Name: name, Description: desc, Niche: true, Parameters: json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) { return "ran " + name, nil }}
}

func nicheGroupDef(name, group, desc string) Definition {
	d := nicheDef(name, desc)
	d.Group = group
	d.GroupDesc = group + " server gist"
	return d
}

func nicheCapDef(name, group, cap, desc string) Definition {
	d := nicheGroupDef(name, group, desc)
	d.Capability = cap
	d.CapabilityDesc = cap + " capability"
	return d
}

func names(s []llm.ToolSpec) []string {
	var o []string
	for _, x := range s {
		o = append(o, x.Name)
	}
	return o
}

func TestProgressiveDisclosure(t *testing.T) {
	reg, _ := NewRegistry(
		Definition{Name: "read", Description: "read a file", Run: func(context.Context, json.RawMessage) (string, error) { return "", nil }},
		nicheCapDef("chrome_click", "chrome", "page-actions", "click an element in the browser"),
		nicheCapDef("chrome_navigate", "chrome", "page-actions", "go to a URL"),
		nicheDef("generate_image", "make an image from a prompt"), // ungrouped niche
	)
	// Core specs (nothing unlocked): only the non-niche tool.
	core := reg.CoreSpecs(nil)
	if len(core) != 1 || core[0].Name != "read" {
		t.Fatalf("core specs should exclude niche tools, got %v", names(core))
	}
	// Level-0 catalog: one GROUP (chrome) + the loose generate_image.
	groups, loose := reg.GroupCatalog(nil)
	if len(groups) != 1 || groups[0].Name != "chrome" || groups[0].Count != 2 {
		t.Fatalf("catalog should have the chrome group with 2 tools, got %v", groups)
	}
	if groups[0].Gist != "chrome server gist" {
		t.Fatalf("group gist should come from GroupDesc, got %q", groups[0].Gist)
	}
	if len(loose) != 1 || !strings.Contains(loose[0], "generate_image") {
		t.Fatalf("catalog should list the ungrouped niche tool, got %v", loose)
	}

	st := SearchTools(func() *Registry { return reg }, nil)
	// Level 1: search a group name → capability categories, no tool-name menu,
	// no unlock, no schemas dumped.
	out, _ := st.Run(context.Background(), []byte(`{"query":"chrome"}`))
	if !strings.Contains(out, "page-actions") {
		t.Fatalf("group browse should list capabilities, got %q", out)
	}
	if strings.Contains(out, "chrome_click") || strings.Contains(out, "args:") {
		t.Fatalf("group browse should NOT dump tool names or schemas, got %q", out)
	}

	// Level 2: search group + capability → unlock the whole (small) capability
	// batch with schemas in ONE call, so the model doesn't round-trip per tool.
	var capUnlocked []string
	stCap := SearchTools(func() *Registry { return reg }, func(n []string) { capUnlocked = append(capUnlocked, n...) })
	outCap, _ := stCap.Run(context.Background(), []byte(`{"query":"chrome page-actions"}`))
	if !strings.Contains(outCap, "chrome_click") || !strings.Contains(outCap, "chrome_navigate") || !strings.Contains(outCap, "args:") {
		t.Fatalf("capability browse should unlock the batch with schemas, got %q", outCap)
	}
	if len(capUnlocked) != 2 {
		t.Fatalf("capability browse should unlock both tools, got %v", capUnlocked)
	}

	// Level 3: search a keyword → full schema + unlock.
	var unlocked []string
	st2 := SearchTools(func() *Registry { return reg }, func(n []string) { unlocked = append(unlocked, n...) })
	out2, _ := st2.Run(context.Background(), []byte(`{"query":"navigate"}`))
	if !strings.Contains(out2, "chrome_navigate") || !strings.Contains(out2, "args:") {
		t.Fatalf("keyword search should return the full schema, got %q", out2)
	}
	if len(unlocked) != 1 || unlocked[0] != "chrome_navigate" {
		t.Fatalf("keyword search should unlock the match, got %v", unlocked)
	}
	// After unlocking, CoreSpecs includes it.
	if core2 := reg.CoreSpecs(map[string]bool{"chrome_navigate": true}); len(core2) != 2 {
		t.Fatalf("unlocked niche tool should appear in core specs, got %v", names(core2))
	}
}

func TestGroupCapabilityBrowseAcceptsSpacedGroupAlias(t *testing.T) {
	reg, _ := NewRegistry(
		nicheCapDef("computer_use_setup_accessibility", "computer_use", "accessibility", "set up desktop accessibility"),
	)
	st := SearchTools(func() *Registry { return reg }, nil)
	out, _ := st.Run(context.Background(), []byte(`{"query":"computer use accessibility"}`))
	// Spaced group alias still resolves the group+capability; the capability is a
	// small batch so it unlocks with schemas in one call.
	if !strings.Contains(out, "computer_use_setup_accessibility") || !strings.Contains(out, "args:") {
		t.Fatalf("spaced group alias should resolve + unlock the capability batch, got %q", out)
	}
}

// Paraphrased, multi-word, hyphen-variant queries must still resolve — this is
// the looping the strict matcher caused (the agent couldn't guess the literal
// substring).
func TestFuzzyKeywordMatching(t *testing.T) {
	reg, _ := NewRegistry(
		nicheCapDef("chrome_new_tab", "chrome", "tabs", "open a new browser tab"),
		nicheCapDef("chrome_read_article", "chrome", "page-read", "extract the readable article text from the page"),
	)
	cases := []struct {
		query, want string
	}{
		{"new tab", "chrome_new_tab"},       // multi-word, none-verbatim ("new"+"tab")
		{"open the page article", "chrome_read_article"}, // stopword "the" dropped
		{"read page", "chrome_read_article"}, // matches the page-read capability desc
		{"tabs", "chrome_new_tab"},           // plural reaches singular field
	}
	for _, c := range cases {
		var unlocked []string
		st := SearchTools(func() *Registry { return reg }, func(n []string) { unlocked = append(unlocked, n...) })
		out, _ := st.Run(context.Background(), []byte(`{"query":`+jsonStr(c.query)+`}`))
		if !strings.Contains(out, c.want) {
			t.Errorf("query %q should reach %s, got %q", c.query, c.want, out)
		}
	}
}

// A query that names the right server but misses on keywords must guide into
// that server's capabilities, not dead-end with "no tools match".
func TestScopedMissGuidesIntoGroup(t *testing.T) {
	reg, _ := NewRegistry(
		nicheCapDef("chrome_click", "chrome", "page-actions", "click an element"),
		nicheCapDef("chrome_screenshot", "chrome", "screenshots", "capture a screenshot"),
	)
	st := SearchTools(func() *Registry { return reg }, nil)
	out, _ := st.Run(context.Background(), []byte(`{"query":"chrome frobnicate"}`))
	if strings.Contains(strings.ToLower(out), "no tools match") {
		t.Fatalf("scoped miss should guide into the group, not dead-end, got %q", out)
	}
	if !strings.Contains(out, "page-actions") && !strings.Contains(out, "screenshots") {
		t.Fatalf("scoped miss should list the group's capabilities, got %q", out)
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestEmptySearchListsGroups(t *testing.T) {
	reg, _ := NewRegistry(
		Definition{Name: "read", Description: "r", Run: func(context.Context, json.RawMessage) (string, error) { return "", nil }},
		nicheCapDef("workspace_click", "workspace", "input", "click"),
	)
	st := SearchTools(func() *Registry { return reg }, nil)
	out, _ := st.Run(context.Background(), []byte(`{"query":""}`))
	if !strings.Contains(out, "workspace") || !strings.Contains(out, "1 tools") {
		t.Fatalf("empty query should list groups with counts, got %q", out)
	}
}

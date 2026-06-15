package dream

import (
	"strings"
	"testing"
	"time"
)

func TestParseRollout(t *testing.T) {
	out := `TITLE: Fix the mantle gpt-5.5 failover
OUTCOME: success
PREFERENCES:
- "i want xhigh available at the end" -> keep high reasoning effort as the target, low is triage only
KEY:
- gpt-5.5 mantle engine 500s server-side; gpt-5.4 works on the identical path
FAILURES:
- retrying the same 500 does not help; switch model instead
- (none)
REUSABLE:
- failover gpt-5.5 -> gpt-5.4 first (closest healthy sibling)`
	r := parseRollout(out)
	if r.Title == "" || r.Outcome != "success" {
		t.Fatalf("title/outcome: %+v", r)
	}
	if len(r.Preferences) != 1 || !strings.Contains(r.Preferences[0], "xhigh") {
		t.Fatalf("preferences: %v", r.Preferences)
	}
	if len(r.Key) != 1 || len(r.Reusable) != 1 {
		t.Fatalf("key/reusable: %+v", r)
	}
	// "(none)" must be dropped.
	if len(r.Failures) != 1 || strings.Contains(strings.Join(r.Failures, ""), "(none)") {
		t.Fatalf("failures should drop (none): %v", r.Failures)
	}
}

func TestRolloutSlugAndEmpty(t *testing.T) {
	r := RolloutSummary{Title: "Fix the Mantle GPT-5.5 Failover!!", Outcome: "success", Key: []string{"x"}}
	if r.Slug() != "fix-the-mantle-gpt-5-5-failover" {
		t.Fatalf("slug: %q", r.Slug())
	}
	if r.Empty() {
		t.Fatal("a summary with Key is not empty")
	}
	if !(RolloutSummary{Outcome: "skip"}).Empty() {
		t.Fatal("skip outcome is empty")
	}
	if !(RolloutSummary{Outcome: "success"}).Empty() {
		t.Fatal("no-section summary is empty")
	}
}

func TestRolloutMarkdown(t *testing.T) {
	r := RolloutSummary{Title: "T", Outcome: "partial", Preferences: []string{"p"}, Reusable: []string{"r"}}
	md := r.Markdown("s42", time.Date(2026, 6, 16, 1, 2, 0, 0, time.UTC))
	for _, want := range []string{"# T", "session: s42", "outcome: partial", "## Preferences", "- p", "## Reusable", "- r"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
	// Empty sections omitted.
	if strings.Contains(md, "## Key") || strings.Contains(md, "## Failures") {
		t.Fatal("empty sections should be omitted")
	}
}

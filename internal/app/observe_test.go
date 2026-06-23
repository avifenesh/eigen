package app

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/observe"
)

func TestObservePageRendersTelemetry(t *testing.T) {
	d := testData()
	d.ObservePath = "/tmp/events.jsonl"
	d.Observe = observe.Summary{
		Records: 12,
		ByKind:  map[string]int{"tool_result": 4, "done": 2},
		Errors:  map[string]int{"denied": 1},
		Notes:   map[string]int{"route": 2, "background": 1},
		Routes:  observe.RouteSummary{Routed: 1, Skipped: 1, Assessed: 1, ByModel: map[string]int{"grok-code-fast-1": 1}, ByKind: map[string]int{"general": 1}, ByDifficulty: map[string]int{"trivial": 1}, SkipReasons: map[string]int{"assessor_unavailable": 1}},
		Models:  map[string]observe.ModelSummary{"gpt-5.5": {Turns: 2, InTokens: 100, OutTokens: 25, CacheReadTokens: 70, CacheWriteTokens: 10, DurationMS: 2000}},
		Tools: map[string]observe.ToolSummary{
			"bash":       {Calls: 2, Errors: 1, DurationMS: 500},
			"task_group": {Calls: 1, DurationMS: 1000},
		},
		Hooks:  map[string]observe.HookSummary{"session_start": {Starts: 1, Done: 1, DurationMS: 20}},
		Skills: map[string]observe.SkillSummary{"frontend-skill": {Calls: 2, DurationMS: 30}},
		Subagents: observe.SubagentSummary{
			GroupCalls:      1,
			BackgroundNotes: 1,
			RouteNotes:      2,
		},
		Runtime: observe.RuntimeSummary{MaxMemAllocBytes: 1024 * 1024, MaxHeapInuseBytes: 2 * 1024 * 1024, MaxHeapSysBytes: 3 * 1024 * 1024, MaxGoroutines: 42},
	}
	m := NewAt(d, PageObserve)
	out := m.observe.view(m, 100, 30)
	for _, want := range []string{
		"observe",
		"events 12",
		"routing decisions",
		"1 routed",
		"1 skipped",
		"grok-code-fast-1",
		"subagents / spawning",
		"task_group",
		"errors",
		"route / system notes",
		"model / token usage",
		"gpt-5.5",
		"skill invocations",
		"frontend-skill",
		"tool calls",
		"bash",
		"hooks",
		"session_start",
		"runtime stress",
		"eigen observe summary",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("observe page missing %q:\n%s", want, out)
		}
	}
}

func TestObserveJKScrollContent(t *testing.T) {
	d := testData()
	d.Observe = observe.Summary{
		Records: 99,
		ByKind:  map[string]int{"tool_result": 9, "done": 8, "assistant": 7, "user": 6},
		Errors:  map[string]int{"denied": 3, "timeout": 2},
		Notes:   map[string]int{"route": 4, "background": 3},
		Models: map[string]observe.ModelSummary{
			"gpt-5.5": {Turns: 2, InTokens: 100}, "fast": {Turns: 1}, "claude": {Turns: 3}, "grok": {Turns: 1},
		},
		Tools:  map[string]observe.ToolSummary{"bash": {Calls: 2}, "read": {Calls: 5}, "edit": {Calls: 3}, "grep": {Calls: 4}},
		Hooks:  map[string]observe.HookSummary{"session_start": {Done: 1}, "pre_tool": {Done: 2}},
		Skills: map[string]observe.SkillSummary{"frontend": {Calls: 2}, "backend": {Calls: 1}},
	}
	m := NewAt(d, PageObserve)
	// A short viewport guarantees the rendered page overflows, so scroll is live.
	m.width, m.height = 80, 14
	if m.contentScroll != 0 {
		t.Fatalf("contentScroll should start at 0, got %d", m.contentScroll)
	}
	m.Update(key("j"))
	if m.contentScroll != 1 {
		t.Fatalf("j should scroll content down one line, got %d", m.contentScroll)
	}
	m.Update(key("j"))
	if m.contentScroll != 2 {
		t.Fatalf("second j should scroll to 2, got %d", m.contentScroll)
	}
	m.Update(key("k"))
	if m.contentScroll != 1 {
		t.Fatalf("k should scroll content up one line, got %d", m.contentScroll)
	}
}

func TestHomeSurfacesObserveAttention(t *testing.T) {
	d := testData()
	d.Observe = observe.Summary{
		Records:   10,
		Errors:    map[string]int{"denied": 1},
		Tools:     map[string]observe.ToolSummary{"bash": {Calls: 1, Errors: 1}},
		Subagents: observe.SubagentSummary{RouteNotes: 2},
	}
	m := NewAt(d, PageHome)
	out := m.home.view(m, 90, 30)
	if !strings.Contains(out, "observe:") || !strings.Contains(out, "press o for telemetry dashboard") {
		t.Fatalf("home should surface observability attention:\n%s", out)
	}
}

func TestObservePageAlias(t *testing.T) {
	p, ok := PageByName("observability")
	if !ok || p != PageObserve {
		t.Fatalf("observability alias = %v/%v", p, ok)
	}
}

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

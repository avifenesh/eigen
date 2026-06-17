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
		Models:  map[string]observe.ModelSummary{"gpt-5.5": {Turns: 2, InTokens: 100, OutTokens: 25, CacheReadTokens: 70, CacheWriteTokens: 10, DurationMS: 2000}},
		Tools: map[string]observe.ToolSummary{
			"bash":       {Calls: 2, Errors: 1, DurationMS: 500},
			"task_group": {Calls: 1, DurationMS: 1000},
		},
		Hooks: map[string]observe.HookSummary{"session_start": {Starts: 1, Done: 1, DurationMS: 20}},
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
		"subagents / spawning",
		"task_group",
		"errors",
		"route / system notes",
		"model / token usage",
		"gpt-5.5",
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

func TestObservePageAlias(t *testing.T) {
	p, ok := PageByName("observability")
	if !ok || p != PageObserve {
		t.Fatalf("observability alias = %v/%v", p, ok)
	}
}

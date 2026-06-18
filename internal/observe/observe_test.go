package observe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/hook"
)

func TestLoggerWritesRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	lg, err := Open(path, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	var forwarded int
	sink := lg.Wrap(func(agent.Event) { forwarded++ })
	sink(agent.Event{Kind: agent.EventToolStart, Step: 0, ToolName: "bash", ToolID: "tc1"})
	sink(agent.Event{Kind: agent.EventToolResult, Step: 0, ToolName: "bash", ToolID: "tc1", IsError: true, Result: "Denied: nope"})
	sink(agent.Event{Kind: agent.EventNote, Text: "task → routed → grok-code-fast-1 (general/trivial; assessed by glm-5.2)"})
	sink(agent.Event{Kind: agent.EventNote, Text: "route skipped: assessor unavailable (classifier offline)"})
	sink(agent.Event{Kind: agent.EventDone, Provider: "codex", Model: "gpt-5.5", InTokens: 10, OutTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 2})
	lg.Close()

	if forwarded != 5 {
		t.Fatalf("sink should forward to next, got %d", forwarded)
	}
	f, _ := os.Open(path)
	defer f.Close()
	var recs []Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 5 {
		t.Fatalf("want 5 records, got %d", len(recs))
	}
	if recs[0].Kind != "tool_start" || recs[0].Tool != "bash" || recs[0].ToolID != "tc1" || recs[0].Session != "sess-1" {
		t.Fatalf("rec0 wrong: %+v", recs[0])
	}
	if recs[1].Kind != "tool_result" || !recs[1].IsError || recs[1].ResultLen == 0 || recs[1].ErrorKind != "denied" || recs[1].ErrorHash == "" {
		t.Fatalf("rec1 wrong: %+v", recs[1])
	}
	rawRec, _ := json.Marshal(recs[1])
	if strings.Contains(string(rawRec), "Denied: nope") {
		t.Fatalf("observability record must not store raw error text: %s", rawRec)
	}
	if recs[1].Goroutines == 0 || recs[1].MemAllocBytes == 0 {
		t.Fatalf("rec1 should include runtime sample: %+v", recs[1])
	}
	if recs[2].Kind != "note" || recs[2].NoteKind != "route" || recs[2].RouteStatus != "routed" || recs[2].RouteModel != "grok-code-fast-1" || recs[2].RouteKind != "general" || recs[2].RouteDifficulty != "trivial" || recs[2].RouteAssessor != "glm-5.2" {
		t.Fatalf("rec2 wrong: %+v", recs[2])
	}
	if recs[3].Kind != "note" || recs[3].RouteStatus != "skipped" || recs[3].RouteSkipReason != "assessor_unavailable" {
		t.Fatalf("rec3 wrong: %+v", recs[3])
	}
	if recs[4].Kind != "done" || recs[4].Provider != "codex" || recs[4].Model != "gpt-5.5" || recs[4].InTokens != 10 || recs[4].OutTokens != 3 || recs[4].CacheReadTokens != 4 || recs[4].CacheWriteTokens != 2 || recs[4].Goroutines == 0 {
		t.Fatalf("rec4 wrong: %+v", recs[4])
	}
}

func TestHookObserverAndSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	lg, err := Open(path, "sess-2")
	if err != nil {
		t.Fatal(err)
	}
	obs := lg.HookObserver()
	obs(hook.Observation{Event: "session_start", Phase: "start", Session: "sess-2", CommandHash: "sha256:abc", Argc: 2})
	obs(hook.Observation{Event: "session_start", Phase: "done", Session: "sess-2", CommandHash: "sha256:abc", Argc: 2})
	sink := lg.Wrap(nil)
	sink(agent.Event{Kind: agent.EventToolStart, ToolName: "skill", ToolID: "skill-1", ToolArgs: json.RawMessage(`{"name":"frontend-skill"}`)})
	sink(agent.Event{Kind: agent.EventToolResult, ToolName: "skill", ToolID: "skill-1", Result: "loaded"})
	sink(agent.Event{Kind: agent.EventToolResult, ToolName: "task", Result: "ok"})
	sink(agent.Event{Kind: agent.EventToolResult, ToolName: "task_group", IsError: true, Result: "failed"})
	sink(agent.Event{Kind: agent.EventNote, Text: "task → routed → grok-code-fast-1 (general/trivial; assessed by glm-5.2)"})
	sink(agent.Event{Kind: agent.EventNote, Text: "route skipped: assessor unavailable (classifier offline)"})
	sink(agent.Event{Kind: agent.EventBgDone, Text: "background finished"})
	lg.Close()
	s, err := ReadSummary(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hooks["session_start"].Starts != 1 || s.Hooks["session_start"].Done != 1 {
		t.Fatalf("hook summary wrong: %+v", s.Hooks)
	}
	if s.Subagents.TaskCalls != 1 || s.Subagents.GroupCalls != 1 || s.Subagents.GroupErrors != 1 || s.Subagents.BackgroundDone != 1 {
		t.Fatalf("subagent summary wrong: %+v", s.Subagents)
	}
	if s.Skills["frontend-skill"].Calls != 1 || s.Skills["frontend-skill"].Errors != 0 {
		t.Fatalf("skill summary wrong: %+v", s.Skills)
	}
	if s.Routes.Routed != 1 || s.Routes.Skipped != 1 || s.Routes.Assessed != 1 || s.Routes.SkipReasons["assessor_unavailable"] != 1 {
		t.Fatalf("route summary wrong: %+v", s.Routes)
	}
	out := FormatSummary(s)
	if !strings.Contains(out, "hooks:") || !strings.Contains(out, "session_start") || !strings.Contains(out, "subagents/spawns") || !strings.Contains(out, "skills:") || !strings.Contains(out, "frontend-skill") || !strings.Contains(out, "routing decisions:") || !strings.Contains(out, "assessor_unavailable") {
		t.Fatalf("summary should include hooks/subagents, got:\n%s", out)
	}
}

func TestNilLoggerIsNoop(t *testing.T) {
	var lg *Logger // nil
	var got int
	next := func(agent.Event) { got++ }
	sink := lg.Wrap(next)
	sink(agent.Event{Kind: agent.EventDone})
	if got != 1 {
		t.Fatal("nil logger should forward to next unchanged")
	}
	if err := lg.Close(); err != nil {
		t.Fatalf("nil close should be nil, got %v", err)
	}
}

func TestOpenEmptyPathNil(t *testing.T) {
	lg, err := Open("", "")
	if err != nil || lg != nil {
		t.Fatalf("empty path should be (nil,nil), got (%v,%v)", lg, err)
	}
	// Wrapping with the nil logger is the identity.
	next := agent.EventSink(func(agent.Event) {})
	if lg.Wrap(next) == nil {
		t.Fatal("nil logger Wrap should return next")
	}
}

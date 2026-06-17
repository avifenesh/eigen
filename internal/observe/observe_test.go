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
	sink(agent.Event{Kind: agent.EventNote, Text: "routed → grok-code-fast-1 (general/trivial)"})
	sink(agent.Event{Kind: agent.EventDone, Provider: "codex", Model: "gpt-5.5", InTokens: 10, OutTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 2})
	lg.Close()

	if forwarded != 4 {
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
	if len(recs) != 4 {
		t.Fatalf("want 4 records, got %d", len(recs))
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
	if recs[2].Kind != "note" || recs[2].NoteKind != "route" {
		t.Fatalf("rec2 wrong: %+v", recs[2])
	}
	if recs[3].Kind != "done" || recs[3].Provider != "codex" || recs[3].Model != "gpt-5.5" || recs[3].InTokens != 10 || recs[3].OutTokens != 3 || recs[3].CacheReadTokens != 4 || recs[3].CacheWriteTokens != 2 || recs[3].Goroutines == 0 {
		t.Fatalf("rec3 wrong: %+v", recs[3])
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
	lg.Close()
	s, err := ReadSummary(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hooks["session_start"].Starts != 1 || s.Hooks["session_start"].Done != 1 {
		t.Fatalf("hook summary wrong: %+v", s.Hooks)
	}
	out := FormatSummary(s)
	if !strings.Contains(out, "hooks:") || !strings.Contains(out, "session_start") {
		t.Fatalf("summary should include hooks, got:\n%s", out)
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

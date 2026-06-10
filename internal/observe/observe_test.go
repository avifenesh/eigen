package observe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
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
	sink(agent.Event{Kind: agent.EventToolStart, Step: 0, ToolName: "bash"})
	sink(agent.Event{Kind: agent.EventToolResult, Step: 0, ToolName: "bash", IsError: true, Result: "boom"})
	lg.Close()

	if forwarded != 2 {
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
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	if recs[0].Kind != "tool_start" || recs[0].Tool != "bash" || recs[0].Session != "sess-1" {
		t.Fatalf("rec0 wrong: %+v", recs[0])
	}
	if recs[1].Kind != "tool_result" || !recs[1].IsError || recs[1].ResultLen != 4 {
		t.Fatalf("rec1 wrong: %+v", recs[1])
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
